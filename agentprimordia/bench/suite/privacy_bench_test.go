package suite

// Phase 4.1: 隐私路由开销基准
//
// 测量 PII 检测 + 路由决策对延迟的影响：
//   - 无 PII 文本的路由延迟（direct 策略）
//   - 含 PII 文本的路由延迟（local_inference / redact 策略）
//   - 不同候选节点数下的路由开销

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/cluster"
)

// benchPIIDetector 基准测试用 PII 检测器（模拟正则检测开销）
type benchPIIDetector struct{}

func (d *benchPIIDetector) Detect(text string) []cluster.PIIFinding {
	// 模拟真实检测器的行为：扫描邮箱和手机号模式
	var findings []cluster.PIIFinding
	// 简化检测：查找 "@" 和连续数字
	for i := 0; i < len(text); i++ {
		if text[i] == '@' && i > 0 {
			findings = append(findings, cluster.PIIFinding{
				Type:  "email",
				Value: "user@example.com",
				Start: i - 4,
				End:   i + 12,
			})
			break
		}
	}
	return findings
}

// newBenchPrivacyRouter 创建基准测试用隐私路由器
func newBenchPrivacyRouter(nodeCount int, withWebGPU bool) *cluster.PrivacyRouter {
	router := cluster.NewPrivacyRouter(
		cluster.WithPIIDetector(&benchPIIDetector{}),
	)

	for i := 0; i < nodeCount; i++ {
		router.RegisterCapability(fmt.Sprintf("node-%d", i), &cluster.NodeCapability{
			HasWebGPU:     withWebGPU && i == 0, // 只有第一个节点有 WebGPU
			HasLocalLLM:   withWebGPU && i == 0,
			PrivacyLevel:  2,
			MaxConcurrent: 10,
			CurrentLoad:   i % 5,
		})
	}

	return router
}

// candidateNodes 生成候选节点列表
func candidateNodes(n int) []string {
	nodes := make([]string, n)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("node-%d", i)
	}
	return nodes
}

// BenchmarkPrivacyRouter_Route_NoPII 基准：无 PII 文本路由
func BenchmarkPrivacyRouter_Route_NoPII(b *testing.B) {
	router := newBenchPrivacyRouter(5, true)
	ctx := context.Background()
	text := "请帮我分析这个项目的架构设计，不涉及任何个人信息"
	nodes := candidateNodes(5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = router.Route(ctx, text, nodes)
	}
}

// BenchmarkPrivacyRouter_Route_WithPII 基准：含 PII 文本路由
func BenchmarkPrivacyRouter_Route_WithPII(b *testing.B) {
	router := newBenchPrivacyRouter(5, true)
	ctx := context.Background()
	text := "请联系 user@example.com 获取项目详情"
	nodes := candidateNodes(5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = router.Route(ctx, text, nodes)
	}
}

// BenchmarkPrivacyRouter_Route_NodeScale 基准：不同候选节点数下的路由
func BenchmarkPrivacyRouter_Route_NodeScale(b *testing.B) {
	ctx := context.Background()
	text := "请联系 user@example.com 获取项目详情"

	nodeCounts := []int{3, 5, 10, 20}
	for _, n := range nodeCounts {
		b.Run(fmt.Sprintf("Nodes_%d", n), func(b *testing.B) {
			router := newBenchPrivacyRouter(n, true)
			nodes := candidateNodes(n)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = router.Route(ctx, text, nodes)
			}
		})
	}
}

// BenchmarkPrivacyRouter_RegisterCapability 基准：节点能力注册开销
func BenchmarkPrivacyRouter_RegisterCapability(b *testing.B) {
	router := cluster.NewPrivacyRouter(
		cluster.WithPIIDetector(&benchPIIDetector{}),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.RegisterCapability(fmt.Sprintf("node-%d", i%100), &cluster.NodeCapability{
			HasWebGPU:     true,
			PrivacyLevel:  2,
			MaxConcurrent: 10,
		})
	}
}
