package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

// outputGuardMockProvider 简单的 mock Provider，固定返回指定内容
type outputGuardMockProvider struct {
	content string
}

func (p *outputGuardMockProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{
		Content: p.content,
		Usage:   llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}
func (p *outputGuardMockProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, nil
}
func (p *outputGuardMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, nil
}
func (p *outputGuardMockProvider) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}
func (p *outputGuardMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock", Provider: "mock"}
}

// TestRunLoop_OutputGuard_Sanitize 验证 OutputGuard 在 LLM 响应返回后自动脱敏
func TestRunLoop_OutputGuard_Sanitize(t *testing.T) {
	mockProvider := &outputGuardMockProvider{
		content: "用户邮箱是 zhangsan@example.com，电话 13812345678",
	}

	// OutputGuard：检测到包含 PII 时执行简单脱敏（替换为 [MASKED]）
	guardCalled := false
	guard := OutputGuard(func(content string) (string, bool, error) {
		guardCalled = true
		sanitized := content
		sanitized = strings.ReplaceAll(sanitized, "zhangsan@example.com", "[MASKED_EMAIL]")
		sanitized = strings.ReplaceAll(sanitized, "13812345678", "[MASKED_PHONE]")
		if sanitized != content {
			return sanitized, false, nil
		}
		return "", false, nil
	})

	agent := newReActAgent(ReActConfig{
		Name:     "test-guard",
		Model:    mockProvider,
		MaxTurns: 1,
	}).WithOutputGuard(guard)

	resp, err := agent.Run(context.Background(), Message{Role: RoleUser, Content: "测试"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !guardCalled {
		t.Error("OutputGuard 未被调用")
	}
	if strings.Contains(resp.Content, "zhangsan@example.com") {
		t.Errorf("PII 未脱敏: %q", resp.Content)
	}
	if strings.Contains(resp.Content, "13812345678") {
		t.Errorf("PII 未脱敏: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "[MASKED_EMAIL]") {
		t.Errorf("缺少 [MASKED_EMAIL]: %q", resp.Content)
	}
}

// TestRunLoop_OutputGuard_Block 验证 OutputGuard 在 reject 动作时阻断返回
func TestRunLoop_OutputGuard_Block(t *testing.T) {
	mockProvider := &outputGuardMockProvider{content: "正常响应内容"}

	guardCalled := false
	guard := OutputGuard(func(_ string) (string, bool, error) {
		guardCalled = true
		return "", true, nil // 拒绝
	})

	agent := newReActAgent(ReActConfig{
		Name:     "test-block",
		Model:    mockProvider,
		MaxTurns: 1,
	}).WithOutputGuard(guard)

	_, err := agent.Run(context.Background(), Message{Role: RoleUser, Content: "测试"})
	if err == nil {
		t.Error("期望返回 blocked 错误")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("错误信息不正确: %v", err)
	}
	// 修复评估报告 §四.1-③：拦截错误必须匹配 ErrOutputBlocked sentinel
	//（修复前为裸 fmt.Errorf，errors.Is 永不命中）。
	if !errors.Is(err, ErrOutputBlocked) {
		t.Errorf("errors.Is(err, ErrOutputBlocked) 应为 true, got err=%v", err)
	}
	if !guardCalled {
		t.Error("OutputGuard 未被调用")
	}
}

// TestRunLoop_OutputGuard_Pass 验证 OutputGuard 在 pass 动作时不影响内容
func TestRunLoop_OutputGuard_Pass(t *testing.T) {
	expected := "干净的回复内容"
	mockProvider := &outputGuardMockProvider{content: expected}

	guard := OutputGuard(func(_ string) (string, bool, error) {
		return "", false, nil // pass
	})

	agent := newReActAgent(ReActConfig{
		Name:     "test-pass",
		Model:    mockProvider,
		MaxTurns: 1,
	}).WithOutputGuard(guard)

	resp, err := agent.Run(context.Background(), Message{Role: RoleUser, Content: "测试"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp.Content != expected {
		t.Errorf("content = %q, want %q", resp.Content, expected)
	}
}

// TestRunLoop_OutputGuard_NotSet 验证未设置 OutputGuard 时正常运行
func TestRunLoop_OutputGuard_NotSet(t *testing.T) {
	expected := "未配置 guardrail 的响应"
	mockProvider := &outputGuardMockProvider{content: expected}

	agent := newReActAgent(ReActConfig{
		Name:     "test-no-guard",
		Model:    mockProvider,
		MaxTurns: 1,
	}).AsCapability()

	resp, err := agent.Run(context.Background(), Message{Role: RoleUser, Content: "测试"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp.Content != expected {
		t.Errorf("content = %q, want %q", resp.Content, expected)
	}
}
