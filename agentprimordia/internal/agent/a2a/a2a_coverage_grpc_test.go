// a2a_coverage_grpc_test.go — A2A 协议栈覆盖率补强（gRPC 面）
//
// 目标（只新增测试，不改生产代码）：
//   - grpc_auth.go：context 主体注入/提取、unary/stream 认证拦截器、
//     APIKey/Bearer 凭证提取函数（含 metadata 缺失分支）；
//   - grpc_circuit_breaker.go：unary/stream 拦截器透传与熔断快速失败；
//   - grpc_convert.go：AgentCard/Task/Message/Part/Artifact/TaskEvent 的
//     proto 双向转换往返（含 nil 分支）；
//   - interceptors.go：指标收集器快照与配置默认值；
//   - trace_propagation.go：ctx 便捷封装；
//   - tool_lease.go / lessee.go：租约释放、TTL 选项、注册表视图。
package a2a

import (
	"context"
	"errors"
	"testing"
	"time"

	a2av1 "github.com/Gleamseekers/AgentPrimordia/internal/agent/a2a/proto/a2a/v1"
	"github.com/Gleamseekers/AgentPrimordia/internal/resilience"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ===== grpc_auth.go =====

// TestCoverPrincipalContextRoundTrip WithPrincipal / PrincipalFromContext 往返。
func TestCoverPrincipalContextRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, ok := PrincipalFromContext(ctx); ok {
		t.Error("空 ctx 不应有主体")
	}
	p := &Principal{ID: "u1", Scopes: []string{"*"}}
	ctx2 := WithPrincipal(ctx, p)
	got, ok := PrincipalFromContext(ctx2)
	if !ok || got.ID != "u1" {
		t.Errorf("主体往返失败: %v %v", got, ok)
	}
}

// TestCoverUnaryAuthInterceptor 三条路径：nil 直通 / 认证失败 / 成功注入主体。
func TestCoverUnaryAuthInterceptor(t *testing.T) {
	t.Parallel()
	info := &grpc.UnaryServerInfo{FullMethod: "/a2a.v1.A2AService/CreateTask"}
	handler := func(ctx context.Context, _ any) (any, error) {
		p, ok := PrincipalFromContext(ctx)
		if !ok {
			return nil, errors.New("no principal")
		}
		return p.ID, nil
	}

	// nil auth → 直通（handler 看不到主体）
	passthrough := UnaryAuthInterceptor(nil)
	if _, err := passthrough(context.Background(), nil, info, handler); err == nil {
		t.Error("nil auth 直通时 handler 应报无主体")
	}

	// 认证失败 → Unauthenticated
	failing := UnaryAuthInterceptor(func(context.Context) (*Principal, error) {
		return nil, errors.New("bad token")
	})
	_, err := failing(context.Background(), nil, info, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("错误码 = %v, want Unauthenticated", status.Code(err))
	}

	// 认证成功 → 主体注入
	ok := UnaryAuthInterceptor(func(context.Context) (*Principal, error) {
		return &Principal{ID: "svc-a"}, nil
	})
	resp, err := ok(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatal(err)
	}
	if resp != "svc-a" {
		t.Errorf("handler 应看到主体 svc-a, got %v", resp)
	}
}

// nopServerStream 实现最小 grpc.ServerStream。
type nopServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (n *nopServerStream) Context() context.Context { return n.ctx }
func (n *nopServerStream) Send(any) error           { return nil }

// TestCoverStreamAuthInterceptor stream 拦截器三条路径与 wrappedStream 上下文。
func TestCoverStreamAuthInterceptor(t *testing.T) {
	t.Parallel()
	info := &grpc.StreamServerInfo{FullMethod: "/a2a.v1.A2AService/SubscribeTaskEvents"}
	handler := func(_ any, ss grpc.ServerStream) error {
		p, ok := PrincipalFromContext(ss.Context())
		if !ok {
			return errors.New("no principal in stream")
		}
		return status.Error(codes.OK, p.ID)
	}

	// nil auth → 直通（原始 ctx 无主体）
	passthrough := StreamAuthInterceptor(nil)
	if err := passthrough(nil, &nopServerStream{ctx: context.Background()}, info, handler); err == nil {
		t.Error("nil auth 直通时 handler 应报无主体")
	}

	// 认证失败 → Unauthenticated
	failing := StreamAuthInterceptor(func(context.Context) (*Principal, error) {
		return nil, errors.New("expired")
	})
	err := failing(nil, &nopServerStream{ctx: context.Background()}, info, handler)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("错误码 = %v, want Unauthenticated", status.Code(err))
	}

	// 认证成功 → wrappedStream 携带主体
	ok := StreamAuthInterceptor(func(context.Context) (*Principal, error) {
		return &Principal{ID: "svc-b"}, nil
	})
	err = ok(nil, &nopServerStream{ctx: context.Background()}, info, handler)
	if status.Code(err) != codes.OK {
		t.Errorf("handler 应通过: %v", err)
	}
}

// TestCoverAPIKeyAuthFunc metadata 缺失 / header 缺失 / 钥无效 / 合法 / 默认 header。
func TestCoverAPIKeyAuthFunc(t *testing.T) {
	t.Parallel()
	keys := map[string]string{"k-1": "agent-1"}
	fn := APIKeyAuthFunc(keys, "x-api-key")

	// 无 metadata → ErrAuthHeaderMissing
	if _, err := fn(context.Background()); !errors.Is(err, ErrAuthHeaderMissing) {
		t.Errorf("无 metadata 应返回 ErrAuthHeaderMissing, got %v", err)
	}
	// 有 metadata 但缺 header
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("other", "v"))
	if _, err := fn(ctx); !errors.Is(err, ErrAuthHeaderMissing) {
		t.Errorf("缺 header 应返回 ErrAuthHeaderMissing, got %v", err)
	}
	// 无效钥
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "bad"))
	if _, err := fn(ctx); err == nil {
		t.Error("无效钥应报错")
	}
	// 合法钥
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "k-1"))
	p, err := fn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "agent-1" || !p.HasScope("*") {
		t.Errorf("主体错误: %+v", p)
	}
	// 默认 header 名（空 headerName → x-api-key）
	defFn := APIKeyAuthFunc(keys, "")
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-api-key", "k-1"))
	if _, err := defFn(ctx); err != nil {
		t.Errorf("默认 header 名应生效: %v", err)
	}
}

// TestCoverBearerAuthFunc bearer 提取：metadata 缺失 / header 缺失 / 非 Bearer / 合法。
func TestCoverBearerAuthFunc(t *testing.T) {
	t.Parallel()
	fn := BearerAuthFunc(func(token string) (*Principal, error) {
		if token != "good" {
			return nil, errors.New("invalid token")
		}
		return &Principal{ID: "u9"}, nil
	})

	if _, err := fn(context.Background()); !errors.Is(err, ErrAuthHeaderMissing) {
		t.Errorf("无 metadata 应返回 ErrAuthHeaderMissing, got %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x", "y"))
	if _, err := fn(ctx); !errors.Is(err, ErrAuthHeaderMissing) {
		t.Errorf("缺 authorization 应返回 ErrAuthHeaderMissing, got %v", err)
	}
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic abc"))
	if _, err := fn(ctx); !errors.Is(err, ErrAuthBearerRequired) {
		t.Errorf("非 Bearer 应返回 ErrAuthBearerRequired, got %v", err)
	}
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))
	p, err := fn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "u9" {
		t.Errorf("主体错误: %+v", p)
	}
	// 验证器错误向上传递
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))
	if _, err := fn(ctx); err == nil {
		t.Error("验证器错误应向上传递")
	}
}

// ===== grpc_circuit_breaker.go =====

// TestCoverCircuitBreakerInterceptor_Unary 一元拦截器：成功透传 / 失败计数 /
// 熔断快速失败 / Reset 恢复。
func TestCoverCircuitBreakerInterceptor_Unary(t *testing.T) {
	t.Parallel()
	cbi := NewCircuitBreakerInterceptor(resilience.Config{
		FailureThreshold: 2, Timeout: 100 * time.Millisecond,
	}, nil) // nil logger → slog.Default()
	if cbi.logger == nil {
		t.Error("nil logger 应回退默认")
	}
	interceptor := cbi.UnaryClientInterceptor()
	invoker := func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		return errors.New("rpc failed")
	}

	// 前两次失败被记录（未熔断）
	for i := 0; i < 2; i++ {
		err := interceptor(context.Background(), "/m", nil, nil, nil, invoker)
		if err == nil {
			t.Fatal("失败应向上传递")
		}
	}
	// 第三次：熔断打开 → 快速失败（invoker 不再被调用）
	var called bool
	fastInvoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		called = true
		return nil
	}
	err := interceptor(context.Background(), "/m", nil, nil, nil, fastInvoker)
	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Errorf("熔断应返回 ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Error("熔断打开时不应调用 invoker")
	}
	if cbi.State() != resilience.StateOpen {
		t.Errorf("State = %v, want Open", cbi.State())
	}
	// Reset 后恢复
	cbi.Reset()
	if cbi.State() != resilience.StateClosed {
		t.Errorf("Reset 后 State = %v, want Closed", cbi.State())
	}
	if err := interceptor(context.Background(), "/m", nil, nil, nil, fastInvoker); err != nil {
		t.Errorf("Reset 后应放行: %v", err)
	}
}

// TestCoverCircuitBreakerInterceptor_Stream 流拦截器：streamer 错误/成功两路。
func TestCoverCircuitBreakerInterceptor_Stream(t *testing.T) {
	t.Parallel()
	cb := resilience.NewCircuitBreaker(resilience.Config{FailureThreshold: 5, Timeout: time.Second})
	cbi := NewCircuitBreakerInterceptorWithCB(cb, slogDiscard())
	interceptor := cbi.StreamClientInterceptor()

	// streamer 返回错误 → 错误透传（断路器记录失败）
	errStreamer := func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, errors.New("dial failed")
	}
	if _, err := interceptor(context.Background(), nil, nil, "/m", errStreamer); err == nil {
		t.Error("streamer 错误应向上传递")
	}

	// 成功 → 返回 stream
	want := &nopClientStream{}
	okStreamer := func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
		return want, nil
	}
	got, err := interceptor(context.Background(), nil, nil, "/m", okStreamer)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Error("应返回 streamer 产生的 stream")
	}
}

// nopClientStream 实现最小 grpc.ClientStream。
type nopClientStream struct{ grpc.ClientStream }

// TestCoverWithGRPCCircuitBreaker 选项注入断路器字段。
func TestCoverWithGRPCCircuitBreaker(t *testing.T) {
	t.Parallel()
	cb := resilience.NewCircuitBreaker(resilience.Config{})
	c := &A2AGRPCClient{}
	WithGRPCCircuitBreaker(cb)(c)
	if c.circuitBreaker != cb {
		t.Error("WithGRPCCircuitBreaker 应注入断路器")
	}
}

// ===== grpc_convert.go =====

// TestCoverProtoConvert_RoundTrip AgentCard 全字段 proto 往返（含 nil 分支）。
func TestCoverProtoConvert_RoundTrip(t *testing.T) {
	t.Parallel()
	if toProtoAgentCard(nil) != nil {
		t.Error("nil AgentCard 应转 nil")
	}
	if fromProtoAgentCard(nil) != nil {
		t.Error("nil proto AgentCard 应转 nil")
	}
	card := &AgentCard{
		Protocol: "a2a", AgentID: "a1", Name: "A1", Description: "d",
		Capabilities: AgentCapabilities{InputModes: []string{"text"}, OutputModes: []string{"text"}, Streaming: true},
		Endpoints:    AgentEndpoints{BaseURL: "http://a1", TaskSend: "/send", AgentCardURL: "/card"},
		SecuritySchemes: []SecurityScheme{
			{Scheme: AuthBearer, In: "header", Name: "Authorization", Scopes: []string{"*"}},
		},
		Skills: []AgentSkill{{ID: "s1", Name: "echo", InputModes: []string{"text"}}},
		Metadata: map[string]string{"k": "v"},
	}
	pc := toProtoAgentCard(card)
	if pc.SecuritySchemes[0].Scheme != "bearer" || pc.Skills[0].Name != "echo" {
		t.Fatalf("proto 转换错误: %+v", pc)
	}
	back := fromProtoAgentCard(pc)
	if back.AgentID != "a1" || back.Name != "A1" || !back.Capabilities.Streaming {
		t.Fatalf("往返丢失字段: %+v", back)
	}
	if len(back.SecuritySchemes) != 1 || back.SecuritySchemes[0].Scheme != AuthBearer {
		t.Errorf("安全方案往返错误: %+v", back.SecuritySchemes)
	}
	if len(back.Skills) != 1 || back.Skills[0].ID != "s1" {
		t.Errorf("技能往返错误: %+v", back.Skills)
	}
	// nil 子结构分支
	if toProtoAgentCapabilities(nil) != nil || fromProtoAgentCapabilities(nil) == nil {
		t.Error("Capabilities nil 分支异常")
	}
	if toProtoAgentEndpoints(nil) != nil || fromProtoAgentEndpoints(nil) == nil {
		t.Error("Endpoints nil 分支异常")
	}
	if got := fromProtoSecurityScheme(nil); got.Scheme != "" || got.Name != "" {
		t.Error("nil SecurityScheme 应回退零值")
	}
	if got := fromProtoAgentSkill(nil); got.ID != "" || got.Name != "" {
		t.Error("nil AgentSkill 应回退零值")
	}
}

// TestCoverProtoConvert_TaskAndEvents Task/Message/Part/Artifact/TaskEvent 往返。
func TestCoverProtoConvert_TaskAndEvents(t *testing.T) {
	t.Parallel()
	// Task 含 message/status/artifacts
	task := &Task{
		ID: "t1", SessionID: "s1", State: TaskWorking,
		Message: &A2AMessage{
			Role: "user", MessageID: "m1", ParentID: "p0",
			Parts: []Part{
				NewTextPart("hi"),
				NewFilePartFromURI("https://x/f", "text/plain"),
				FilePart{TypeField: "file", File: &FileWithBytes{Name: "a", MimeType: "application/octet-stream", Bytes: "eHg="}},
				NewDataPart([]byte(`{"k":1}`)),
				&TextPart{TypeField: "text", Text: "ptr-text"},
			},
		},
		Status:    &TaskStatus{State: TaskWorking, ErrorMessage: "", StreamMessage: &A2AMessage{Role: "agent"}},
		Artifacts: []Artifact{{ArtifactID: "art-1", MimeType: "text/plain", Bytes: []byte("x"), URI: "https://x/a"}},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	pt := toProtoTask(task)
	if len(pt.Message.Parts) != 5 {
		t.Fatalf("parts = %d, want 5", len(pt.Message.Parts))
	}
	back := fromProtoTask(pt)
	if back.ID != "t1" || back.State != TaskWorking {
		t.Fatalf("Task 往返错误: %+v", back)
	}
	if len(back.Artifacts) != 1 || back.Artifacts[0].ArtifactID != "art-1" {
		t.Errorf("Artifacts 往返错误: %+v", back.Artifacts)
	}
	// part 类型保持（text×2 / file×2 / data×1）
	var textN, fileN, dataN int
	for _, p := range back.Message.Parts {
		switch p.(type) {
		case TextPart:
			textN++
		case FilePart:
			fileN++
		case DataPart:
			dataN++
		}
	}
	if textN != 2 || fileN != 2 || dataN != 1 {
		t.Errorf("part 类型往返错误: text=%d file=%d data=%d", textN, fileN, dataN)
	}

	// nil 分支
	if toProtoTask(nil) != nil || fromProtoTask(nil) != nil {
		t.Error("Task nil 分支异常")
	}
	if toProtoMessage(nil) != nil || fromProtoMessage(nil) != nil {
		t.Error("Message nil 分支异常")
	}
	if toProtoTaskStatus(nil) != nil || fromProtoTaskStatus(nil) != nil {
		t.Error("TaskStatus nil 分支异常")
	}
	if toProtoPart(nil) != nil || fromProtoPart(nil) != nil {
		t.Error("Part nil 分支异常")
	}

	// part 内容缺失 → 回退 TextPart{Type: part.Type}
	emptyPart := &a2av1.Part{Type: "mystery"}
	fallback := fromProtoPart(emptyPart)
	if tp, ok := fallback.(TextPart); !ok || tp.TypeField != "mystery" {
		t.Errorf("无内容 part 应回退 TextPart: %T %+v", fallback, fallback)
	}
	// 已知类型但内容为 nil → 同样回退
	nilText := &a2av1.Part{Type: "text", Content: &a2av1.Part_Text{}}
	if _, ok := fromProtoPart(nilText).(TextPart); !ok {
		t.Errorf("Text 内容为 nil 应回退 TextPart: %T", fromProtoPart(nilText))
	}

	// TaskEvent 往返（含 state/artifact）
	state := TaskCompleted
	ev := &TaskEvent{
		Type: EventArtifact, TaskID: "t1", Timestamp: time.Now(),
		Message:  &A2AMessage{Role: "agent"},
		Artifact: &Artifact{ArtifactID: "art-1"},
		State:    &state,
		Error:    "",
	}
	pe := toProtoTaskEvent(ev)
	if pe.State != "completed" || pe.Artifact == nil || pe.Artifact.ArtifactId != "art-1" {
		t.Fatalf("TaskEvent proto 转换错误: %+v", pe)
	}
	backEv := fromProtoTaskEvent(pe)
	if backEv.State == nil || *backEv.State != TaskCompleted {
		t.Errorf("State 往返错误: %+v", backEv.State)
	}
	if backEv.Artifact == nil || backEv.Artifact.ArtifactID != "art-1" {
		t.Errorf("Artifact 往返错误: %+v", backEv.Artifact)
	}
	// nil / 空 state 分支
	if toProtoTaskEvent(nil) != nil || fromProtoTaskEvent(nil) != nil {
		t.Error("TaskEvent nil 分支异常")
	}
	if fromProtoTaskEvent(&a2av1.TaskEvent{Type: "state_change"}).State != nil {
		t.Error("空 state 不应生成 State 指针")
	}
}

// ===== interceptors.go =====

// TestCoverInterceptorMetrics 指标快照零值与配置默认值分支。
func TestCoverInterceptorMetrics(t *testing.T) {
	t.Parallel()
	m := NewA2AInterceptorMetrics()
	snap := m.Snapshot()
	if snap.TotalCalls != 0 || snap.AvgLatencyMillis != 0 {
		t.Errorf("零值快照异常: %+v", snap)
	}

	cfg := &A2AInterceptorConfig{}
	if cfg.logger() == nil {
		t.Error("logger() 不应返回 nil")
	}
	if cfg.metrics() == nil {
		t.Error("metrics() 不应返回 nil")
	}
	if cfg.slowThreshold() != time.Second {
		t.Errorf("默认慢请求阈值 = %v, want 1s", cfg.slowThreshold())
	}
	custom := &A2AInterceptorConfig{
		Logger: slogDiscard(), Metrics: m, SlowRequestThreshold: 5 * time.Millisecond,
	}
	if custom.logger() == nil || custom.metrics() != m || custom.slowThreshold() != 5*time.Millisecond {
		t.Error("自定义配置未生效")
	}
}

// ===== trace_propagation.go =====

// TestCoverTraceContextInCtx ctx 便捷封装：生成与续传。
func TestCoverTraceContextInCtx(t *testing.T) {
	t.Parallel()
	ctx, tc := GenerateTraceContextInCtx(context.Background())
	if tc.IsZero() {
		t.Fatal("生成的 TraceContext 不应为零值")
	}
	got, ok := TraceContextFromContext(ctx)
	if !ok || got.TraceID != tc.TraceID {
		t.Errorf("ctx 中的 trace 不一致: %v vs %v", got, tc)
	}

	ctx2, child := ContinueTraceInCtx(context.Background(), tc)
	if child.TraceID != tc.TraceID {
		t.Errorf("子 trace 应保持 trace_id: %s vs %s", child.TraceID, tc.TraceID)
	}
	if child.SpanID == tc.SpanID {
		t.Error("子 trace 应更新 span_id")
	}
	got2, _ := TraceContextFromContext(ctx2)
	if got2.SpanID != child.SpanID {
		t.Error("ctx 中的子 trace 不一致")
	}

	// 零值父 → 生成新 trace
	_, fresh := ContinueTraceInCtx(context.Background(), TraceContext{})
	if fresh.IsZero() {
		t.Error("零值父应生成新 trace")
	}
}

// ===== tool_lease.go / lessee.go =====

// TestCoverToolLease_ReleaseAndOptions 租约释放、lessor/lessee TTL 选项、
// 注册表视图。
func TestCoverToolLease_ReleaseAndOptions(t *testing.T) {
	t.Parallel()
	// Release 标记释放
	lease := &ToolLease{Status: LeaseStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
	lease.Release()
	if lease.Status != LeaseStatusReleased || lease.IsActive() {
		t.Errorf("Release 后状态错误: %v", lease.Status)
	}

	// lessor TTL 选项（复用 newLessorWithTools 的注册表）
	_, reg := newLessorWithTools()
	l2 := NewLessorHandler(reg, "agent", WithLeaseTTL(2*time.Minute), WithLeaseMaxDuration(10*time.Minute))
	got, err := l2.CreateLease("get_weather", "caller", "ep", 0 /* ≤0 取默认 TTL */)
	if err != nil {
		t.Fatal(err)
	}
	// LeasedAt/ExpiresAt 为两次 time.Now() 调用，允许毫秒级漂移
	if d := got.ExpiresAt.Sub(got.LeasedAt); d < 2*time.Minute || d > 2*time.Minute+time.Second {
		t.Errorf("默认 TTL 应为 ~2m, got %v", d)
	}
	// 超过 maxDuration 被钳制
	clamped, err := l2.CreateLease("get_weather", "caller", "ep", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if d := clamped.ExpiresAt.Sub(clamped.LeasedAt); d < 10*time.Minute || d > 10*time.Minute+time.Second {
		t.Errorf("TTL 应被钳制到 ~10m, got %v", d)
	}
	// GetRegistry 视图
	if l2.GetRegistry() != reg {
		t.Error("GetRegistry 应返回同一注册表")
	}

	// lessee 客户端
	lessee := NewLesseeClient(reg, WithLeaseClientMaxTTL(30*time.Minute))
	lse, err := lessee.LeaseTool(context.Background(), "lessor", "get_weather", "ep")
	if err != nil {
		t.Fatal(err)
	}
	if d := lse.ExpiresAt.Sub(lse.LeasedAt); d < 30*time.Minute || d > 30*time.Minute+time.Second {
		t.Errorf("客户端最大租期应为 ~30m, got %v", d)
	}
	if lse.MaxCalls != 100 {
		t.Errorf("默认 MaxCalls = %d, want 100", lse.MaxCalls)
	}
	if lessee.ActiveLeaseCount() != 1 {
		t.Errorf("ActiveLeaseCount = %d, want 1", lessee.ActiveLeaseCount())
	}
	if _, ok := lessee.GetLease(lse.LeaseID); !ok {
		t.Error("GetLease 应找到租约")
	}
	if !lessee.ReleaseLease(lse.LeaseID) {
		t.Error("ReleaseLease 应成功")
	}
	if lessee.ReleaseLease(lse.LeaseID) {
		t.Error("重复释放应失败")
	}
	if lessee.ActiveLeaseCount() != 0 {
		t.Errorf("释放后 ActiveLeaseCount = %d", lessee.ActiveLeaseCount())
	}
}
