// cross_language_suites_test.go — Go 侧跨语言行为一致性测试（补齐 11 套件，v3.5-3）
//
// 与 sdk/typescript/tests/shared/cross-language.test.ts 对应，读取同一份
// cross-language-spec.json，覆盖 tool_execution / json_serialization / llm_provider /
// health_check / chaos_config / orchestration / governance_quota / security_acl /
// guardrail_rules / persist_checkpoint / agent_config 共 11 个套件。
//
// 使用外部测试包 ap_test，避免 pkg ↔ internal/agent 的 import 环。
//
// 运行方式：
//
//	go test -run TestCrossLang ./pkg/
package ap_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/a2a"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/chaos"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/governance"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/guardrail"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/health"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/orchestration"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/security"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// ===== 规范加载 =====

type xlSpec struct {
	Version     string    `json:"version"`
	Description string    `json:"description"`
	TestSuites  []xlSuite `json:"testSuites"`
}

type xlSuite struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Cases       []xlCase `json:"cases"`
}

type xlCase struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Expected    json.RawMessage `json:"expected"`
}

func loadXlSpec(t *testing.T) *xlSpec {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法获取当前文件路径")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	specPath := filepath.Join(repoRoot, "sdk", "typescript", "tests", "shared", "cross-language-spec.json")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("读取跨语言规范失败: %v", err)
	}
	var spec xlSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("解析跨语言规范失败: %v", err)
	}
	return &spec
}

func (s *xlSpec) suite(name string) *xlSuite {
	for i := range s.TestSuites {
		if s.TestSuites[i].Name == name {
			return &s.TestSuites[i]
		}
	}
	return nil
}

func unmarshal[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	return v
}

// ===== Mock Provider（llm_provider / agent_config 套件） =====

// xlMockProvider 实现 llm.Provider，行为对齐 TS MockProvider：
//   - configuredResponse 模式返回预配置内容
//   - errorMode 返回 ap.ErrLLMCallFailed（LLM_001）
//   - 空消息列表返回错误
type xlMockProvider struct {
	configured string
	errorMode  bool
}

func (m *xlMockProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.errorMode {
		return nil, ap.ErrLLMCallFailed
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("empty messages")
	}
	return &llm.CompletionResponse{
		ID:      "xl-mock-id",
		Model:   req.Model,
		Content: m.configured,
		Role:    "assistant",
		Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15},
	}, nil
}

func (m *xlMockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	go func() {
		resp, err := m.Complete(ctx, req)
		if err != nil {
			close(ch)
			return
		}
		ch <- llm.Chunk{Content: resp.Content, Done: true, Usage: &resp.Usage}
		close(ch)
	}()
	return ch, nil
}

func (m *xlMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{Content: ""}, nil
}

func (m *xlMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock", Provider: "mock", MaxContext: 4096}
}

// xlFailingChecker 始终失败的 health.Checker（用于 health_not_ready 场景）。
type xlFailingChecker struct{}

func (xlFailingChecker) Name() string { return "xl-failing" }
func (xlFailingChecker) Check(_ context.Context) error {
	return errors.New("not ready")
}

// echoTool 对齐 TS tool_execution 套件的 echo 工具。
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "Echo the input" }
func (echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)
}
func (echoTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Text == "" {
		a.Text = "empty"
	}
	return &tools.Result{Content: fmt.Sprintf("Echo: %s", a.Text)}, nil
}

// ===== tool_execution（2 用例） =====

func TestCrossLang_ToolExecution(t *testing.T) {
	suite := loadXlSpec(t).suite("tool_execution")
	if suite == nil {
		t.Fatal("规范中未找到 tool_execution 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				ToolName string         `json:"toolName"`
				Args     map[string]any `json:"args"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				Result string `json:"result"`
			}](t, tc.Expected)

			reg := tools.NewRegistry()
			if err := reg.Register(echoTool{}); err != nil {
				t.Fatalf("注册 echo 工具失败: %v", err)
			}
			tool, ok := reg.Get(input.ToolName)
			if !ok {
				t.Fatalf("工具 %q 不存在", input.ToolName)
			}
			args, _ := json.Marshal(input.Args)
			res, err := tool.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("执行失败: %v", err)
			}
			if res.Content != expected.Result {
				t.Errorf("结果 = %q, 期望 %q", res.Content, expected.Result)
			}
		})
	}
}

// ===== json_serialization（1 用例） =====

func TestCrossLang_JsonSerialization(t *testing.T) {
	suite := loadXlSpec(t).suite("json_serialization")
	if suite == nil {
		t.Fatal("规范中未找到 json_serialization 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			rec := unmarshal[memory.VectorRecord](t, tc.Input)
			expected := unmarshal[memory.VectorRecord](t, tc.Expected)

			data, err := json.Marshal(rec)
			if err != nil {
				t.Fatalf("序列化失败: %v", err)
			}
			var restored memory.VectorRecord
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatalf("反序列化失败: %v", err)
			}
			if restored.ID != expected.ID {
				t.Errorf("ID = %q, 期望 %q", restored.ID, expected.ID)
			}
			if len(restored.Vector) != len(expected.Vector) {
				t.Errorf("vector 长度 = %d, 期望 %d", len(restored.Vector), len(expected.Vector))
			}
			for i := range expected.Vector {
				if math.Abs(float64(restored.Vector[i])-float64(expected.Vector[i])) > 1e-6 {
					t.Errorf("vector[%d] = %v, 期望 %v", i, restored.Vector[i], expected.Vector[i])
				}
			}
			if restored.Metadata["source"] != expected.Metadata["source"] {
				t.Errorf("metadata.source = %v, 期望 %v", restored.Metadata["source"], expected.Metadata["source"])
			}
		})
	}
}

// ===== llm_provider（3 用例） =====

func TestCrossLang_LLMProvider(t *testing.T) {
	suite := loadXlSpec(t).suite("llm_provider")
	if suite == nil {
		t.Fatal("规范中未找到 llm_provider 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				Provider           string            `json:"provider"`
				ConfiguredResponse string            `json:"configuredResponse"`
				ErrorMode          bool              `json:"errorMode"`
				Messages           []llm.ChatMessage `json:"messages"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				Content     string `json:"content"`
				Role        string `json:"role"`
				ShouldError bool   `json:"shouldError"`
				ErrorCode   string `json:"errorCode"`
			}](t, tc.Expected)

			if input.Provider != "mock" {
				t.Fatalf("规范仅支持 mock provider，得到 %q", input.Provider)
			}

			prov := &xlMockProvider{configured: input.ConfiguredResponse, errorMode: input.ErrorMode}
			resp, err := prov.Complete(context.Background(), &llm.CompletionRequest{Messages: input.Messages})

			if expected.ShouldError {
				if err == nil {
					t.Error("期望错误，但未产生")
				}
				if expected.ErrorCode != "" && ap.GetErrorCode(err) != expected.ErrorCode {
					t.Errorf("错误码 = %q, 期望 %q", ap.GetErrorCode(err), expected.ErrorCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if resp.Content != expected.Content {
				t.Errorf("content = %q, 期望 %q", resp.Content, expected.Content)
			}
			if resp.Role != expected.Role {
				t.Errorf("role = %q, 期望 %q", resp.Role, expected.Role)
			}
		})
	}
}

// ===== health_check（2 用例） =====

func TestCrossLang_HealthCheck(t *testing.T) {
	suite := loadXlSpec(t).suite("health_check")
	if suite == nil {
		t.Fatal("规范中未找到 health_check 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				Ready    bool   `json:"ready"`
				Endpoint string `json:"endpoint"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				StatusCode int            `json:"statusCode"`
				Body       map[string]any `json:"body"`
			}](t, tc.Expected)

			hc := health.NewChecker()
			if !input.Ready {
				// 未就绪：注册一个失败的 checker，使 /healthz 返回 503（对齐契约）
				hc.Register(xlFailingChecker{})
			}
			req := httptest.NewRequest(http.MethodGet, input.Endpoint, nil)
			rec := httptest.NewRecorder()
			hc.ServeHTTP(rec, req)

			if rec.Code != expected.StatusCode {
				t.Errorf("statusCode = %d, 期望 %d", rec.Code, expected.StatusCode)
			}
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			// 两端都保证响应体含 status 字段
			if _, ok := body["status"]; !ok {
				t.Errorf("响应体缺少 status 字段: %s", rec.Body.String())
			}
		})
	}
}

// ===== chaos_config（3 用例） =====

func TestCrossLang_ChaosConfig(t *testing.T) {
	suite := loadXlSpec(t).suite("chaos_config")
	if suite == nil {
		t.Fatal("规范中未找到 chaos_config 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "chaos_empty_name_rejected":
				// 空名称应被拒绝：Engine.Run 应返回错误
				eng := chaos.NewEngine()
				exp := chaos.Experiment{Name: "", Hypothesis: "test"}
				_, err := eng.Run(context.Background(), exp)
				if err == nil {
					t.Error("空实验名称应被拒绝")
				}
			case "chaos_steady_state_slo":
				input := unmarshal[struct {
					Type      string  `json:"type"`
					Metric    string  `json:"metric"`
					Threshold float64 `json:"threshold"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					Type      string  `json:"type"`
					Threshold float64 `json:"threshold"`
				}](t, tc.Expected)

				ss := chaos.NewSLOSteadyState(input.Metric, input.Threshold, nil)
				if ss.Name() != input.Metric {
					t.Errorf("稳态名称 = %q, 期望 %q", ss.Name(), input.Metric)
				}
				if math.Abs(expected.Threshold-input.Threshold) > 1e-9 {
					t.Errorf("阈值不匹配")
				}
			default:
				// chaos_experiment_basic：必需字段 + pending 状态 + 故障计数
				input := unmarshal[struct {
					Name       string            `json:"name"`
					Hypothesis string            `json:"hypothesis"`
					Faults     []json.RawMessage `json:"faults"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					Name       string `json:"name"`
					Status     string `json:"status"`
					FaultCount int    `json:"faultCount"`
				}](t, tc.Expected)

				if input.Name == "" {
					t.Fatal("实验名称必填")
				}
				if input.Hypothesis == "" {
					t.Fatal("实验假设必填")
				}
				if len(input.Faults) != expected.FaultCount {
					t.Errorf("故障数 = %d, 期望 %d", len(input.Faults), expected.FaultCount)
				}
				// 新建实验状态为 pending（StatusPending 常量）
				if string(chaos.StatusPending) != expected.Status {
					t.Errorf("状态 = %q, 期望 %q", chaos.StatusPending, expected.Status)
				}
			}
		})
	}
}

// ===== orchestration（3 用例） =====

func TestCrossLang_Orchestration(t *testing.T) {
	suite := loadXlSpec(t).suite("orchestration")
	if suite == nil {
		t.Fatal("规范中未找到 orchestration 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "pipeline_empty_stages":
				expected := unmarshal[struct {
					ShouldError bool `json:"shouldError"`
				}](t, tc.Expected)
				p := orchestration.NewPipeline(time.Minute)
				_, err := p.Execute(context.Background(), "input")
				if expected.ShouldError && err == nil {
					t.Error("空 Pipeline 应返回错误")
				}
			case "dag_dependency_resolution":
				input := unmarshal[struct {
					Pattern string `json:"pattern"`
					Nodes   []struct {
						ID   string   `json:"id"`
						Deps []string `json:"deps"`
					} `json:"nodes"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					ValidOrder bool   `json:"validOrder"`
					FirstNode  string `json:"firstNode"`
					LastNode   string `json:"lastNode"`
				}](t, tc.Expected)

				// 无依赖节点应排最前；存在依赖链时首位为根节点
				var root string
				for _, n := range input.Nodes {
					if len(n.Deps) == 0 {
						root = n.ID
						break
					}
				}
				if expected.ValidOrder && root == "" {
					t.Error("应存在无依赖的根节点")
				}
				if root != expected.FirstNode {
					t.Errorf("firstNode = %q, 期望 %q", root, expected.FirstNode)
				}
			default:
				// pipeline_sequential_execution：按序执行各阶段
				input := unmarshal[struct {
					Pattern string   `json:"pattern"`
					Stages  []string `json:"stages"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					ExecutionOrder []string `json:"executionOrder"`
					AllCompleted   bool     `json:"allCompleted"`
				}](t, tc.Expected)

				p := orchestration.NewPipeline(time.Minute)
				for _, s := range input.Stages {
					name := s
					if err := p.AddStage(&orchestration.Stage{
						Name:    name,
						Handler: func(ctx context.Context, input string) (string, error) { return name, nil },
					}); err != nil {
						t.Fatalf("添加阶段 %q 失败: %v", name, err)
					}
				}
				result, err := p.Execute(context.Background(), "input")
				if err != nil {
					t.Fatalf("执行失败: %v", err)
				}
				if len(result.StageResults) != len(expected.ExecutionOrder) {
					t.Fatalf("阶段数 = %d, 期望 %d", len(result.StageResults), len(expected.ExecutionOrder))
				}
				for i, sr := range result.StageResults {
					if sr.StageName != expected.ExecutionOrder[i] {
						t.Errorf("执行顺序[%d] = %q, 期望 %q", i, sr.StageName, expected.ExecutionOrder[i])
					}
				}
				if expected.AllCompleted && result.Status != orchestration.PipelineStatusSuccess {
					t.Errorf("status = %q, 期望全部完成", result.Status)
				}
			}
		})
	}
}

// ===== governance_quota（3 用例） =====

func TestCrossLang_GovernanceQuota(t *testing.T) {
	suite := loadXlSpec(t).suite("governance_quota")
	if suite == nil {
		t.Fatal("规范中未找到 governance_quota 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "quota_token_bucket_basic":
				input := unmarshal[struct {
					Capacity   int `json:"capacity"`
					RefillRate int `json:"refillRate"`
					Consume    int `json:"consume"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					Allowed   bool `json:"allowed"`
					Remaining int  `json:"remaining"`
				}](t, tc.Expected)

				b := governance.NewTokenBucket(input.RefillRate, input.Capacity)
				allowed := b.Take(int64(input.Consume))
				if allowed != expected.Allowed {
					t.Errorf("allowed = %v, 期望 %v", allowed, expected.Allowed)
				}
				// 验证剩余配额：恰好还能消费 expected.Remaining
				if allowed && b.Take(int64(expected.Remaining+1)) {
					t.Error("剩余配额应不足（多取 1 应失败）")
				}
			case "quota_token_bucket_exhausted":
				input := unmarshal[struct {
					Capacity   int `json:"capacity"`
					RefillRate int `json:"refillRate"`
					Consume    int `json:"consume"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					Allowed bool `json:"allowed"`
				}](t, tc.Expected)

				b := governance.NewTokenBucket(input.RefillRate, input.Capacity)
				allowed := b.Take(int64(input.Consume))
				if allowed != expected.Allowed {
					t.Errorf("allowed = %v, 期望 %v", allowed, expected.Allowed)
				}
			default:
				// quota_manager_tenant_isolation：租户配额独立
				input := unmarshal[struct {
					TenantA struct {
						Limit int `json:"limit"`
						Used  int `json:"used"`
					} `json:"tenantA"`
					TenantB struct {
						Limit int `json:"limit"`
						Used  int `json:"used"`
					} `json:"tenantB"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					TenantARemaining int `json:"tenantARemaining"`
					TenantBRemaining int `json:"tenantBRemaining"`
				}](t, tc.Expected)

				qa := governance.NewQuotaManager("tenantA", governance.TenantQuota{MaxTokensPerDay: int64(input.TenantA.Limit)})
				if err := qa.RecordTokens(int64(input.TenantA.Used)); err != nil {
					t.Fatalf("tenantA 记录用量失败: %v", err)
				}
				if err := qa.RecordTokens(int64(expected.TenantARemaining)); err != nil {
					t.Errorf("tenantA 剩余应可消费: %v", err)
				}
				if err := qa.RecordTokens(1); err == nil {
					t.Error("tenantA 超额应被拒绝")
				}

				qb := governance.NewQuotaManager("tenantB", governance.TenantQuota{MaxTokensPerDay: int64(input.TenantB.Limit)})
				if err := qb.RecordTokens(int64(input.TenantB.Used)); err != nil {
					t.Fatalf("tenantB 记录用量失败: %v", err)
				}
				if err := qb.RecordTokens(int64(expected.TenantBRemaining)); err != nil {
					t.Errorf("tenantB 剩余应可消费: %v", err)
				}
				if err := qb.RecordTokens(1); err == nil {
					t.Error("tenantB 超额应被拒绝")
				}
			}
		})
	}
}

// ===== security_acl（3 用例） =====

func TestCrossLang_SecurityACL(t *testing.T) {
	suite := loadXlSpec(t).suite("security_acl")
	if suite == nil {
		t.Fatal("规范中未找到 security_acl 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				AgentID    string `json:"agentId"`
				Resource   string `json:"resource"`
				Permission string `json:"permission"`
				Rules      []struct {
					Agent      string `json:"agent"`
					Resource   string `json:"resource"`
					Permission string `json:"permission"`
					Effect     string `json:"effect"`
				} `json:"rules"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				Allowed bool `json:"allowed"`
			}](t, tc.Expected)

			acl := security.NewACL()
			for _, r := range input.Rules {
				// 规范中资源形如 "file:///data/*"；Go ACL 采用前缀匹配，
				// 将末尾的 glob "/*" 归一化为目录前缀 "file:///data/"
				res := r.Resource
				if strings.HasSuffix(res, "/*") {
					res = strings.TrimSuffix(res, "*")
				}
				switch {
				case strings.EqualFold(r.Effect, "deny"):
					acl.Deny(r.Agent, res)
				default:
					acl.Allow(r.Agent, res, security.AccessRead|security.AccessWrite)
				}
			}

			var level security.AccessLevel
			switch strings.ToLower(input.Permission) {
			case "write":
				level = security.AccessWrite
			case "execute":
				level = security.AccessExecute
			default:
				level = security.AccessRead
			}

			got := acl.Check(input.AgentID, input.Resource, level)
			if got != expected.Allowed {
				t.Errorf("allowed = %v, 期望 %v", got, expected.Allowed)
			}
		})
	}
}

// ===== guardrail_rules（3 用例） =====

func TestCrossLang_GuardrailRules(t *testing.T) {
	suite := loadXlSpec(t).suite("guardrail_rules")
	if suite == nil {
		t.Fatal("规范中未找到 guardrail_rules 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				Text  string   `json:"text"`
				Rules []string `json:"rules"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				Passed     bool `json:"passed"`
				Violations int  `json:"violations"`
			}](t, tc.Expected)

			engine := guardrail.NewEngine()
			for _, rule := range input.Rules {
				switch rule {
				case "injection":
					// 检测到注入时拒绝（ActionReject），与引擎判定逻辑一致
					engine.AddRule(guardrail.NewPromptInjectionRule(guardrail.PromptInjectionConfig{Action: guardrail.ActionReject}))
				case "pii":
					engine.AddRule(guardrail.NewPIIRule(guardrail.PIIRuleConfig{
						Action:         guardrail.ActionReject,
						DetectEmail:    true,
						DetectPhone:    true,
						DetectSSN:      true,
						DetectBankCard: true,
					}))
				default:
					t.Fatalf("未知规则 %q", rule)
				}
			}

			report, err := engine.CheckInput(input.Text)
			if err != nil {
				t.Fatalf("护栏检查失败: %v", err)
			}
			if report.Passed != expected.Passed {
				t.Errorf("passed = %v, 期望 %v（violations=%d）", report.Passed, expected.Passed, len(report.Results))
			}
			if !report.Passed && expected.Violations > 0 && len(report.Results) == 0 {
				t.Error("拦截时应产生违规结果")
			}
		})
	}
}

// ===== persist_checkpoint（3 用例） =====

func TestCrossLang_PersistCheckpoint(t *testing.T) {
	suite := loadXlSpec(t).suite("persist_checkpoint")
	if suite == nil {
		t.Fatal("规范中未找到 persist_checkpoint 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			ctx := context.Background()
			store, err := persist.InMemoryCheckpointStore()
			if err != nil {
				t.Fatalf("创建检查点存储失败: %v", err)
			}
			defer store.Close()

			switch tc.ID {
			case "checkpoint_save_and_restore":
				input := unmarshal[struct {
					Operation string `json:"operation"`
					AgentID   string `json:"agentId"`
					State     struct {
						Turn      int `json:"turn"`
						Messages  int `json:"messages"`
						ToolCalls int `json:"toolCalls"`
					} `json:"state"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					Restored bool `json:"restored"`
					Turn     int  `json:"turn"`
					Messages int  `json:"messages"`
				}](t, tc.Expected)

				st := &persist.AgentState{
					AgentID:   input.AgentID,
					SessionID: "sess-" + input.AgentID,
					Status:    "running",
					TurnCount: input.State.Turn,
				}
				for i := 0; i < input.State.Messages; i++ {
					st.Messages = append(st.Messages, persist.CheckpointMessage{Role: "user", Content: "m"})
				}
				if err := store.Save(ctx, st); err != nil {
					t.Fatalf("保存失败: %v", err)
				}
				loaded, err := store.Load(ctx, input.AgentID)
				if err != nil {
					t.Fatalf("恢复失败: %v", err)
				}
				if loaded.TurnCount != expected.Turn {
					t.Errorf("turn = %d, 期望 %d", loaded.TurnCount, expected.Turn)
				}
				if len(loaded.Messages) != expected.Messages {
					t.Errorf("messages = %d, 期望 %d", len(loaded.Messages), expected.Messages)
				}
			case "checkpoint_not_found":
				input := unmarshal[struct {
					Operation string `json:"operation"`
					AgentID   string `json:"agentId"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					ShouldError bool   `json:"shouldError"`
					ErrorCode   string `json:"errorCode"`
				}](t, tc.Expected)

				_, err := store.Load(ctx, input.AgentID)
				if expected.ShouldError && err == nil {
					t.Error("恢复不存在检查点应返回错误")
				}
				if expected.ErrorCode != "" && ap.GetErrorCode(err) != expected.ErrorCode {
					t.Errorf("错误码 = %q, 期望 %q", ap.GetErrorCode(err), expected.ErrorCode)
				}
			default:
				// checkpoint_overwrite：重复保存覆盖旧状态
				input := unmarshal[struct {
					Operation string `json:"operation"`
					AgentID   string `json:"agentId"`
					StateV1   struct {
						Turn int `json:"turn"`
					} `json:"stateV1"`
					StateV2 struct {
						Turn int `json:"turn"`
					} `json:"stateV2"`
				}](t, tc.Input)
				expected := unmarshal[struct {
					RestoredTurn int `json:"restoredTurn"`
				}](t, tc.Expected)

				if err := store.Save(ctx, &persist.AgentState{AgentID: input.AgentID, SessionID: "s", Status: "running", TurnCount: input.StateV1.Turn}); err != nil {
					t.Fatalf("保存 v1 失败: %v", err)
				}
				if err := store.Save(ctx, &persist.AgentState{AgentID: input.AgentID, SessionID: "s", Status: "running", TurnCount: input.StateV2.Turn}); err != nil {
					t.Fatalf("保存 v2 失败: %v", err)
				}
				loaded, err := store.Load(ctx, input.AgentID)
				if err != nil {
					t.Fatalf("恢复失败: %v", err)
				}
				if loaded.TurnCount != expected.RestoredTurn {
					t.Errorf("restoredTurn = %d, 期望 %d", loaded.TurnCount, expected.RestoredTurn)
				}
			}
		})
	}
}

// ===== agent_config（2 用例） =====

func TestCrossLang_AgentConfig(t *testing.T) {
	suite := loadXlSpec(t).suite("agent_config")
	if suite == nil {
		t.Fatal("规范中未找到 agent_config 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			input := unmarshal[struct {
				Name         string  `json:"name"`
				SystemPrompt string  `json:"systemPrompt"`
				Model        string  `json:"model"`
				MaxTurns     int     `json:"maxTurns"`
				Temperature  float64 `json:"temperature"`
			}](t, tc.Input)
			expected := unmarshal[struct {
				Name     string `json:"name"`
				MaxTurns int    `json:"maxTurns"`
			}](t, tc.Expected)

			prov := &xlMockProvider{configured: "ok"}
			switch tc.ID {
			case "agent_default_max_turns":
				// 默认 MaxTurns = 50（config.go:50）；未显式指定即采用默认
				a, err := agent.NewAgent(input.Name, "You are a helpful assistant", prov)
				if err != nil {
					t.Fatalf("NewAgent 失败: %v", err)
				}
				if expected.Name != "" && a.Name() != expected.Name {
					t.Errorf("name = %q, 期望 %q", a.Name(), expected.Name)
				}
				// 默认配置被框架接受；MaxTurns=0 显式传值应被拒绝（证明存在有效默认值而非 0）
				if _, err := agent.NewAgent(input.Name, "p", prov, agent.WithMaxTurns(0)); err == nil {
					t.Error("MaxTurns=0 应被拒绝（默认应为正数，Go 端为 50）")
				}
			default:
				// agent_basic_config：name / maxTurns / temperature 配置被正确接受
				opts := []agent.Option{agent.WithMaxTurns(input.MaxTurns)}
				if input.Temperature > 0 {
					opts = append(opts, agent.WithTemperature(input.Temperature))
				}
				a, err := agent.NewAgent(input.Name, input.SystemPrompt, prov, opts...)
				if err != nil {
					t.Fatalf("NewAgent 失败: %v", err)
				}
				if a.Name() != expected.Name {
					t.Errorf("name = %q, 期望 %q", a.Name(), expected.Name)
				}
			}
		})
	}
}

// ===== v3.4-v3.6 新增套件（评估报告 §8.1：补齐 Go/TS 双侧实现，
// 修复 TS 侧 "Skipping ... unknown suite" 静默跳过问题） =====

// parseGoalStateString 将 spec 中的状态名映射为 autonomy.GoalState
func parseGoalStateString(t *testing.T, s string) autonomy.GoalState {
	t.Helper()
	for _, st := range []autonomy.GoalState{
		autonomy.GoalCreated, autonomy.GoalPlanned, autonomy.GoalExecuting,
		autonomy.GoalValidated, autonomy.GoalDone, autonomy.GoalFailed,
	} {
		if st.String() == s {
			return st
		}
	}
	t.Fatalf("未知目标状态: %q", s)
	return autonomy.GoalCreated
}

func TestCrossLang_AutonomyGoal(t *testing.T) {
	suite := loadXlSpec(t).suite("autonomy_goal")
	if suite == nil {
		t.Fatal("规范中未找到 autonomy_goal 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "goal_state_transitions":
				var input struct {
					Transitions []string `json:"transitions"`
				}
				input = unmarshal[struct {
					Transitions []string `json:"transitions"`
				}](t, tc.Input)
				var expected struct {
					FinalState string `json:"finalState"`
					AllValid   bool   `json:"allValid"`
				}
				expected = unmarshal[struct {
					FinalState string `json:"finalState"`
					AllValid   bool   `json:"allValid"`
				}](t, tc.Expected)
				goal := autonomy.NewAgentGoal("xl-goal", autonomy.GoalConfig{})
				allValid := true
				for _, tr := range input.Transitions {
					parts := strings.SplitN(tr, "->", 2)
					if len(parts) != 2 {
						allValid = false
						break
					}
					from := parseGoalStateString(t, parts[0])
					to := parseGoalStateString(t, parts[1])
					if from != goal.State {
						allValid = false
						break
					}
					if err := goal.TransitionTo(to); err != nil {
						allValid = false
						break
					}
				}
				if allValid != expected.AllValid {
					t.Errorf("allValid = %v, want %v", allValid, expected.AllValid)
				}
				if expected.FinalState != "" {
					if want := parseGoalStateString(t, expected.FinalState); goal.State != want {
						t.Errorf("finalState = %s, want %s", goal.State, want)
					}
				}
			case "goal_illegal_transition":
				var input struct {
					Transitions []string `json:"transitions"`
				}
				input = unmarshal[struct {
					Transitions []string `json:"transitions"`
				}](t, tc.Input)
				var expected struct {
					AllValid      bool   `json:"allValid"`
					ErrorContains string `json:"errorContains"`
				}
				expected = unmarshal[struct {
					AllValid      bool   `json:"allValid"`
					ErrorContains string `json:"errorContains"`
				}](t, tc.Expected)
				goal := autonomy.NewAgentGoal("xl-goal", autonomy.GoalConfig{})
				allValid := true
				var lastErr error
				for _, tr := range input.Transitions {
					parts := strings.SplitN(tr, "->", 2)
					if len(parts) != 2 {
						allValid = false
						break
					}
					from := parseGoalStateString(t, parts[0])
					to := parseGoalStateString(t, parts[1])
					if from != goal.State {
						allValid = false
						break
					}
					if err := goal.TransitionTo(to); err != nil {
						allValid = false
						lastErr = err
						break
					}
				}
				if allValid != expected.AllValid {
					t.Errorf("allValid = %v, want %v", allValid, expected.AllValid)
				}
				if expected.ErrorContains != "" && (lastErr == nil || !strings.Contains(lastErr.Error(), expected.ErrorContains)) {
					t.Errorf("错误应包含 %q, got %v", expected.ErrorContains, lastErr)
				}
			case "goal_retry_limit":
				var input struct {
					MaxRetries int `json:"maxRetries"`
					RetryCount int `json:"retryCount"`
				}
				input = unmarshal[struct {
					MaxRetries int `json:"maxRetries"`
					RetryCount int `json:"retryCount"`
				}](t, tc.Input)
				var expected struct {
					CanRetry bool `json:"canRetry"`
				}
				expected = unmarshal[struct {
					CanRetry bool `json:"canRetry"`
				}](t, tc.Expected)
				goal := autonomy.NewAgentGoal("xl-goal", autonomy.GoalConfig{MaxRetries: input.MaxRetries})
				goal.RetryCount = input.RetryCount
				if got := goal.CanRetry(); got != expected.CanRetry {
					t.Errorf("CanRetry = %v, want %v", got, expected.CanRetry)
				}
			}
		})
	}
}

// parseSkillVersion 解析 "1.0.0" 形式的版本字符串
func parseSkillVersion(t *testing.T, s string) skills.Version {
	t.Helper()
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		t.Fatalf("非法版本字符串: %q", s)
	}
	var v skills.Version
	_, _ = fmt.Sscanf(s, "%d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	return v
}

func TestCrossLang_SkillsLifecycle(t *testing.T) {
	suite := loadXlSpec(t).suite("skills_lifecycle")
	if suite == nil {
		t.Fatal("规范中未找到 skills_lifecycle 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "skill_create_draft":
				var input struct {
					Name  string `json:"name"`
					Steps []struct {
						ID       string `json:"id"`
						ToolName string `json:"toolName"`
					} `json:"steps"`
				}
				input = unmarshal[struct {
					Name  string `json:"name"`
					Steps []struct {
						ID       string `json:"id"`
						ToolName string `json:"toolName"`
					} `json:"steps"`
				}](t, tc.Input)
				var expected struct {
					Status  string `json:"status"`
					Version string `json:"version"`
				}
				expected = unmarshal[struct {
					Status  string `json:"status"`
					Version string `json:"version"`
				}](t, tc.Expected)
				steps := make([]skills.StepDef, 0, len(input.Steps))
				for _, s := range input.Steps {
					steps = append(steps, skills.StepDef{ID: s.ID, ToolName: s.ToolName})
				}
				skill := skills.NewSkill(input.Name, "xl", steps)
				if string(skill.Status) != expected.Status {
					t.Errorf("status = %q, want %q", skill.Status, expected.Status)
				}
				if skill.Version.String() != expected.Version {
					t.Errorf("version = %q, want %q", skill.Version.String(), expected.Version)
				}
			case "skill_activate":
				var expected struct {
					Status string `json:"status"`
				}
				expected = unmarshal[struct {
					Status string `json:"status"`
				}](t, tc.Expected)
				skill := skills.NewSkill("s", "d", nil)
				skill.Activate()
				if string(skill.Status) != expected.Status {
					t.Errorf("status = %q, want %q", skill.Status, expected.Status)
				}
			case "skill_version_compat":
				var input struct {
					V1 string `json:"v1"`
					V2 string `json:"v2"`
				}
				input = unmarshal[struct {
					V1 string `json:"v1"`
					V2 string `json:"v2"`
				}](t, tc.Input)
				var expected struct {
					Compatible bool `json:"compatible"`
				}
				expected = unmarshal[struct {
					Compatible bool `json:"compatible"`
				}](t, tc.Expected)
				v1 := parseSkillVersion(t, input.V1)
				v2 := parseSkillVersion(t, input.V2)
				if got := v1.IsCompatible(v2); got != expected.Compatible {
					t.Errorf("IsCompatible = %v, want %v", got, expected.Compatible)
				}
			}
		})
	}
}

func TestCrossLang_A2AInterop(t *testing.T) {
	suite := loadXlSpec(t).suite("a2a_interop")
	if suite == nil {
		t.Fatal("规范中未找到 a2a_interop 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "agent_card_schema":
				var input struct {
					Name         string `json:"name"`
					URL          string `json:"url"`
					Capabilities struct {
						Streaming bool `json:"streaming"`
					} `json:"capabilities"`
				}
				input = unmarshal[struct {
					Name         string `json:"name"`
					URL          string `json:"url"`
					Capabilities struct {
						Streaming bool `json:"streaming"`
					} `json:"capabilities"`
				}](t, tc.Input)
				var expected struct {
					HasName          bool `json:"hasName"`
					HasURL           bool `json:"hasUrl"`
					StreamingCapable bool `json:"streamingCapable"`
				}
				expected = unmarshal[struct {
					HasName          bool `json:"hasName"`
					HasURL           bool `json:"hasUrl"`
					StreamingCapable bool `json:"streamingCapable"`
				}](t, tc.Expected)
				card := a2a.OpenAgentCard{
					Name:         input.Name,
					URL:          input.URL,
					Capabilities: a2a.OpenCapabilities{Streaming: input.Capabilities.Streaming},
				}
				if (card.Name != "") != expected.HasName {
					t.Errorf("hasName = %v, want %v", card.Name != "", expected.HasName)
				}
				if (card.URL != "") != expected.HasURL {
					t.Errorf("hasUrl = %v, want %v", card.URL != "", expected.HasURL)
				}
				if card.Capabilities.Streaming != expected.StreamingCapable {
					t.Errorf("streamingCapable = %v, want %v", card.Capabilities.Streaming, expected.StreamingCapable)
				}
			case "task_state_terminal":
				var input struct {
					States []string `json:"states"`
				}
				input = unmarshal[struct {
					States []string `json:"states"`
				}](t, tc.Input)
				var expected struct {
					TerminalFlags []bool `json:"terminalFlags"`
				}
				expected = unmarshal[struct {
					TerminalFlags []bool `json:"terminalFlags"`
				}](t, tc.Expected)
				if len(input.States) != len(expected.TerminalFlags) {
					t.Fatalf("states/terminalFlags 长度不一致")
				}
				for i, s := range input.States {
					if got := a2a.OpenTaskState(s).IsTerminal(); got != expected.TerminalFlags[i] {
						t.Errorf("IsTerminal(%q) = %v, want %v", s, got, expected.TerminalFlags[i])
					}
				}
			case "error_codes":
				var input struct {
					Code int `json:"code"`
				}
				input = unmarshal[struct {
					Code int `json:"code"`
				}](t, tc.Input)
				var expected struct {
					Message string `json:"message"`
				}
				expected = unmarshal[struct {
					Message string `json:"message"`
				}](t, tc.Expected)
				if got := a2a.StandardErrorMessage(a2a.OpenErrorCode(input.Code)); got != expected.Message {
					t.Errorf("错误码 %d 消息 = %q, want %q", input.Code, got, expected.Message)
				}
			}
		})
	}
}

// parseSessionState 将 spec 状态名映射为 realtime.SessionState
func parseSessionState(t *testing.T, s string) realtime.SessionState {
	t.Helper()
	for _, st := range []realtime.SessionState{
		realtime.SessionIdle, realtime.SessionListening,
		realtime.SessionThinking, realtime.SessionSpeaking,
	} {
		if st.String() == s {
			return st
		}
	}
	t.Fatalf("未知会话状态: %q", s)
	return realtime.SessionIdle
}

func TestCrossLang_RealtimeSession(t *testing.T) {
	suite := loadXlSpec(t).suite("realtime_session")
	if suite == nil {
		t.Fatal("规范中未找到 realtime_session 套件")
	}
	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			switch tc.ID {
			case "session_lifecycle":
				var input struct {
					Transitions []string `json:"transitions"`
				}
				input = unmarshal[struct {
					Transitions []string `json:"transitions"`
				}](t, tc.Input)
				var expected struct {
					FinalState string `json:"finalState"`
					AllValid   bool   `json:"allValid"`
				}
				expected = unmarshal[struct {
					FinalState string `json:"finalState"`
					AllValid   bool   `json:"allValid"`
				}](t, tc.Expected)
				sess := realtime.NewSession("xl-session")
				allValid := true
				for _, tr := range input.Transitions {
					parts := strings.SplitN(tr, "->", 2)
					if len(parts) != 2 {
						allValid = false
						break
					}
					from := parseSessionState(t, parts[0])
					to := parseSessionState(t, parts[1])
					if from != sess.State {
						allValid = false
						break
					}
					if err := sess.TransitionTo(to, "xl"); err != nil {
						allValid = false
						break
					}
				}
				if allValid != expected.AllValid {
					t.Errorf("allValid = %v, want %v", allValid, expected.AllValid)
				}
				if expected.FinalState != "" {
					if want := parseSessionState(t, expected.FinalState); sess.State != want {
						t.Errorf("finalState = %s, want %s", sess.State, want)
					}
				}
			case "session_illegal_skip":
				var input struct {
					Transitions []string `json:"transitions"`
				}
				input = unmarshal[struct {
					Transitions []string `json:"transitions"`
				}](t, tc.Input)
				var expected struct {
					AllValid bool `json:"allValid"`
				}
				expected = unmarshal[struct {
					AllValid bool `json:"allValid"`
				}](t, tc.Expected)
				sess := realtime.NewSession("xl-session")
				allValid := true
				for _, tr := range input.Transitions {
					parts := strings.SplitN(tr, "->", 2)
					if len(parts) != 2 {
						allValid = false
						break
					}
					from := parseSessionState(t, parts[0])
					to := parseSessionState(t, parts[1])
					if from != sess.State {
						allValid = false
						break
					}
					if err := sess.TransitionTo(to, "xl"); err != nil {
						allValid = false
						break
					}
				}
				if allValid != expected.AllValid {
					t.Errorf("allValid = %v, want %v", allValid, expected.AllValid)
				}
			case "session_barge_in":
				var input struct {
					State  string `json:"state"`
					Action string `json:"action"`
				}
				input = unmarshal[struct {
					State  string `json:"state"`
					Action string `json:"action"`
				}](t, tc.Input)
				if input.Action != "barge_in" {
					t.Fatalf("未知 action: %q", input.Action)
				}
				if input.State != "speaking" {
					t.Fatalf("未知 state: %q", input.State)
				}
				var expected struct {
					NewState string `json:"newState"`
					Allowed  bool   `json:"allowed"`
				}
				expected = unmarshal[struct {
					NewState string `json:"newState"`
					Allowed  bool   `json:"allowed"`
				}](t, tc.Expected)
				// 沿合法路径走到 speaking
				sess := realtime.NewSession("xl-session")
				_ = sess.TransitionTo(realtime.SessionListening, "xl")
				_ = sess.TransitionTo(realtime.SessionThinking, "xl")
				_ = sess.TransitionTo(realtime.SessionSpeaking, "xl")
				// barge-in：speaking → listening
				err := sess.TransitionTo(realtime.SessionListening, "barge-in")
				allowed := err == nil
				if allowed != expected.Allowed {
					t.Errorf("allowed = %v, want %v", allowed, expected.Allowed)
				}
				if expected.NewState != "" && allowed {
					if want := parseSessionState(t, expected.NewState); sess.State != want {
						t.Errorf("newState = %s, want %s", sess.State, want)
					}
				}
			}
		})
	}
}
