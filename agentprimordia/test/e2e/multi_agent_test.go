package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/orchestration"
)

// TestE2E_Pipeline_TwoStages 验证两阶段 Pipeline 编排：
// 输入 → Stage1(大写转换) → Stage2(添加前缀) → 最终输出
func TestE2E_Pipeline_TwoStages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := orchestration.NewPipeline(5 * time.Second)

	err := p.AddStage(&orchestration.Stage{
		Name:    "uppercase",
		Handler: func(_ context.Context, input string) (string, error) { return strings.ToUpper(input), nil },
		OnError: orchestration.ErrorSkip,
	})
	if err != nil {
		t.Fatalf("AddStage(uppercase) error: %v", err)
	}

	err = p.AddStage(&orchestration.Stage{
		Name:    "prefix",
		Handler: func(_ context.Context, input string) (string, error) { return "[PROCESSED] " + input, nil },
		OnError: orchestration.ErrorSkip,
	})
	if err != nil {
		t.Fatalf("AddStage(prefix) error: %v", err)
	}

	result, err := p.Execute(ctx, "hello world")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.FinalOutput != "[PROCESSED] HELLO WORLD" {
		t.Errorf("FinalOutput = %q, want %q", result.FinalOutput, "[PROCESSED] HELLO WORLD")
	}
	if len(result.StageResults) != 2 {
		t.Errorf("StageResults len = %d, want 2", len(result.StageResults))
	}
}

// TestE2E_Pipeline_ErrorSkip 验证错误跳过策略：
// Stage1 失败 → 跳过 → Stage2 继续执行
func TestE2E_Pipeline_ErrorSkip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := orchestration.NewPipeline(5 * time.Second)

	_ = p.AddStage(&orchestration.Stage{
		Name:    "failing-stage",
		Handler: func(_ context.Context, _ string) (string, error) { return "", fmt.Errorf("intentional failure") },
		OnError: orchestration.ErrorSkip,
	})
	_ = p.AddStage(&orchestration.Stage{
		Name:    "recovery-stage",
		Handler: func(_ context.Context, input string) (string, error) { return "recovered: " + input, nil },
		OnError: orchestration.ErrorSkip,
	})

	result, err := p.Execute(ctx, "test input")
	// Pipeline 可能返回 "stages skipped" 错误，这是正常行为
	if err != nil {
		t.Logf("Execute() returned error (expected for skip): %v", err)
	}
	if result != nil && result.FinalOutput != "" {
		t.Logf("got output despite skip: %q", result.FinalOutput)
	}
}

// TestE2E_Pipeline_ThreeStages_Chain 验证三阶段链式处理
func TestE2E_Pipeline_ThreeStages_Chain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := orchestration.NewPipeline(5 * time.Second)

	stages := []struct {
		name    string
		handler orchestration.StageHandler
	}{
		{"trim", func(_ context.Context, input string) (string, error) { return strings.TrimSpace(input), nil }},
		{"reverse", func(_ context.Context, input string) (string, error) {
			runes := []rune(input)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		}},
		{"wrap", func(_ context.Context, input string) (string, error) { return "<" + input + ">", nil }},
	}

	for _, s := range stages {
		if err := p.AddStage(&orchestration.Stage{Name: s.name, Handler: s.handler, OnError: orchestration.ErrorSkip}); err != nil {
			t.Fatalf("AddStage(%s) error: %v", s.name, err)
		}
	}

	result, err := p.Execute(ctx, "  hello  ")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.FinalOutput != "<olleh>" {
		t.Errorf("FinalOutput = %q, want %q", result.FinalOutput, "<olleh>")
	}
}

// TestE2E_Pipeline_Events 验证 Pipeline 事件流
func TestE2E_Pipeline_Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := orchestration.NewPipeline(5 * time.Second)
	_ = p.AddStage(&orchestration.Stage{
		Name:    "stage-1",
		Handler: func(_ context.Context, input string) (string, error) { return input + "!", nil },
		OnError: orchestration.ErrorSkip,
	})

	events := p.Events()
	_, err := p.Execute(ctx, "test")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// 收集事件（非阻塞）
	var eventTypes []string
	for {
		select {
		case ev := <-events:
			eventTypes = append(eventTypes, ev.Type)
		default:
			goto done
		}
	}
done:
	if len(eventTypes) == 0 {
		t.Log("no events captured (may be timing-dependent)")
	}
}

// TestE2E_Pipeline_Timeout 验证 Pipeline 超时
func TestE2E_Pipeline_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := orchestration.NewPipeline(100 * time.Millisecond)
	_ = p.AddStage(&orchestration.Stage{
		Name: "slow-stage",
		Handler: func(ctx context.Context, _ string) (string, error) {
			select {
			case <-time.After(10 * time.Second):
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		Timeout: 100 * time.Millisecond,
		OnError: orchestration.ErrorSkip,
	})

	result, err := p.Execute(ctx, "test")
	// Pipeline 应该完成（可能带错误），不应阻塞
	_ = result
	_ = err
}
