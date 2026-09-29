package suite

// Phase 4.1: 知识蒸馏开销基准
//
// 测量 LLM 蒸馏管道对 Agent 响应延迟的影响：
//   - KnowledgeDistiller.Distill 单次蒸馏延迟
//   - DistillPipeline.ProcessInteraction 完整管道延迟
//   - FeedbackLearner 偏好学习开销

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/learning"
)

// benchInteraction 构造基准测试用交互数据
func benchInteraction(i int) learning.Interaction {
	return learning.Interaction{
		ID:          fmt.Sprintf("bench-inter-%d", i),
		UserInput:   fmt.Sprintf("请帮我分析第 %d 号日志文件中的错误模式", i),
		AgentOutput: fmt.Sprintf("已分析日志文件 %d，发现 3 个错误模式：空指针、超时、资源泄漏", i),
		Feedback:    "correct",
		Success:     true,
		Timestamp:   time.Now(),
		Metadata:    map[string]string{"task_type": "log_analysis"},
	}
}

// BenchmarkKnowledgeDistiller_Distill 基准：单次知识蒸馏
func BenchmarkKnowledgeDistiller_Distill(b *testing.B) {
	distiller := learning.NewKnowledgeDistiller()
	ctx := context.Background()

	inter := benchInteraction(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = distiller.Distill(ctx, inter)
	}
}

// BenchmarkDistillPipeline_ProcessInteraction 基准：完整蒸馏管道
func BenchmarkDistillPipeline_ProcessInteraction(b *testing.B) {
	pipeline := learning.NewDistillPipeline()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.ProcessInteraction(ctx, benchInteraction(i))
	}
}

// BenchmarkDistillPipeline_BuildSystemPrompt 基准：系统提示构建
func BenchmarkDistillPipeline_BuildSystemPrompt(b *testing.B) {
	pipeline := learning.NewDistillPipeline()
	ctx := context.Background()

	// 预填充一些交互以产生知识
	for i := 0; i < 50; i++ {
		_, _ = pipeline.ProcessInteraction(ctx, benchInteraction(i))
	}

	basePrompt := "你是一个专业的日志分析助手。"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pipeline.BuildSystemPrompt(basePrompt)
	}
}

// BenchmarkFeedbackLearner_Record 基准：反馈学习开销
func BenchmarkFeedbackLearner_Record(b *testing.B) {
	learner := learning.NewFeedbackLearner()

	feedbacks := []string{"correct", "incorrect", "partially correct", "excellent"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = learner.RecordFeedback(learning.FeedbackEntry{
			ID:          fmt.Sprintf("fb-%d", i),
			UserInput:   "测试输入",
			AgentOutput: "测试输出",
			Feedback:    feedbacks[i%len(feedbacks)],
		})
	}
}

// BenchmarkCapabilityEvolver_Evaluate 基准：能力进化评估
func BenchmarkCapabilityEvolver_Evaluate(b *testing.B) {
	evolver := learning.NewCapabilityEvolver()
	evolver.Register(learning.Capability{
		Name:        "log_analysis",
		Description: "日志分析能力",
		Score:       0.5,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evolver.Evaluate("log_analysis", i%3 != 0)
	}
}
