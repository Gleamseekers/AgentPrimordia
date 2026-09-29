package agent

// v3.6 实时 × 跨组件联动集成测试
//
// 验证 realtime 的集成接口（多模态/守卫）能与 Runtime 组合联动：
//   - 视觉帧经多模态提供者分析
//   - ASR 转写经音频护栏校验

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
)

type integMultimodal struct{ calls int }

func (m *integMultimodal) AnalyzeFrame(_ context.Context, f realtime.VideoFrame) (string, error) {
	m.calls++
	return fmt.Sprintf("frame@%dx%d", f.Width, f.Height), nil
}

type integAudioGuard struct{ blocked bool }

func (g *integAudioGuard) CheckTranscript(_ context.Context, t string) (string, bool, error) {
	if g.blocked && t == "secret" {
		return "", true, nil
	}
	return t, false, nil
}

// TestRealtimeMultimodalIntegration 验证视觉帧经多模态分析
func TestRealtimeMultimodalIntegration(t *testing.T) {
	mm := &integMultimodal{}
	mi := realtime.NewMultimodalIntegration(mm)

	desc, err := mi.DescribeFrame(context.Background(), realtime.VideoFrame{Width: 640, Height: 480, Data: []byte("x")})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if desc != "frame@640x480" {
		t.Errorf("desc = %q", desc)
	}
	if mm.calls != 1 {
		t.Errorf("calls = %d", mm.calls)
	}
}

// TestRealtimeGuardrailIntegration 验证转写经音频护栏校验
func TestRealtimeGuardrailIntegration(t *testing.T) {
	guard := &integAudioGuard{blocked: true}
	gi := realtime.NewGuardrailIntegration(guard)
	ctx := context.Background()

	// 正常转写通过
	out, err := gi.SanitizeTranscript(ctx, "hello")
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if out != "hello" {
		t.Errorf("out = %q", out)
	}

	// 敏感转写拦截
	_, err = gi.SanitizeTranscript(ctx, "secret")
	if err == nil {
		t.Fatal("blocked transcript should error")
	}
}

// TestRealtimeFullPipeline 验证 Runtime + 多模态 + 守卫 全链路联动
func TestRealtimeFullPipeline(t *testing.T) {
	mm := &integMultimodal{}
	guard := &integAudioGuard{}
	mi := realtime.NewMultimodalIntegration(mm)
	gi := realtime.NewGuardrailIntegration(guard)

	rt := realtime.NewRuntime(realtime.RuntimeConfig{})
	rt.OpenSession("s1")

	// 推入视觉帧并经多模态分析
	_ = rt.PushVision(context.Background(), "s1", []byte("frame"), 320, 240)
	desc, _ := mi.DescribeFrame(context.Background(), realtime.VideoFrame{Width: 320, Height: 240})
	if desc == "" {
		t.Error("multimodal desc empty")
	}

	// 模拟 ASR 转写并经护栏校验后送入 Runtime
	transcript := "打开灯"
	clean, err := gi.SanitizeTranscript(context.Background(), transcript)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	text, audio, err := rt.ProcessTurn(context.Background(), "s1", clean)
	if err != nil {
		t.Fatalf("process turn: %v", err)
	}
	if text == "" || len(audio) == 0 {
		t.Error("response empty")
	}
}

// TestRealtimeEdgeFallback 验证边缘不可用时回退
func TestRealtimeEdgeFallback(t *testing.T) {
	edge := &integEdge{available: false}
	ei := realtime.NewEdgeIntegration(edge)
	_, err := ei.TryInfer(context.Background(), []byte("in"))
	if err == nil {
		t.Fatal("unavailable edge should error")
	}
}

type integEdge struct{ available bool }

func (e *integEdge) Infer(_ context.Context, in []byte) ([]byte, error) { return in, nil }
func (e *integEdge) Available() bool                                    { return e.available }
