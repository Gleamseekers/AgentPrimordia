//go:build e2e && soak

// e2e_soak_test.go — 集群 24 小时浸泡测试
//
// 在持续负载和混沌故障注入下验证集群稳定性：
//   - 内存增长 < 10%（无泄漏）
//   - 请求延迟保持稳定
//   - 节点存活率保持 100%
//
// 运行方式：
//
//	# 默认 24 小时
//	go test -tags 'e2e soak' -run TestE2E_Cluster_24hSoak -v -timeout=25h ./internal/agent/cluster/
//
//	# CI 模式（1 小时）
//	SOAK_DURATION=1h go test -tags 'e2e soak' -run TestE2E_Cluster_24hSoak -v -timeout=2h ./internal/agent/cluster/
package cluster

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/chaos"
)

// TestE2E_Cluster_24hSoak 24 小时集群浸泡测试
func TestE2E_Cluster_24hSoak(t *testing.T) {
	// 解析浸泡时长（默认 24h，CI 可设置为 1h）
	soakDuration := 24 * time.Hour
	if envDuration := os.Getenv("SOAK_DURATION"); envDuration != "" {
		parsed, err := time.ParseDuration(envDuration)
		if err != nil {
			t.Fatalf("SOAK_DURATION 解析失败 (%q): %v", envDuration, err)
		}
		soakDuration = parsed
	}
	t.Logf("浸泡测试时长: %v", soakDuration)

	const nodeCount = 10
	const memoryLeakThreshold = 10.0 // 10%

	// 创建 10 节点集群
	tc := createTestCluster(t, nodeCount)
	defer tc.stop(t)

	// 记录初始内存
	startMem := getMemoryMB()
	t.Logf("初始内存: %.1f MB", startMem)

	// 构建请求函数：模拟集群操作
	requestFn := func(ctx context.Context) (*chaos.SoakResponse, error) {
		start := time.Now()

		// 操作 1：列出所有 Agent
		_, err := tc.discoveries[0].ListAgents(ctx)
		if err != nil {
			return &chaos.SoakResponse{
				Latency: time.Since(start),
				Success: false,
				Error:   err,
			}, nil
		}

		// 操作 2：哈希环查找
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("soak-key-%d", i)
			tc.managers[0].GetHashRing().GetNode(key)
		}

		// 操作 3：分布式状态读写
		state := tc.managers[0].GetState()
		state.Set("soak-test-key", fmt.Sprintf("value-%d", time.Now().UnixNano()), time.Minute)
		state.Get("soak-test-key")

		// 操作 4：节点列表
		for _, mgr := range tc.managers {
			mgr.ListNodes()
		}

		return &chaos.SoakResponse{
			Latency: time.Since(start),
			Success: true,
		}, nil
	}

	// 构建混沌实验列表
	experiments := []chaos.Experiment{
		{
			Name:        "cluster-node-flap",
			Description: "模拟节点短暂离线后恢复",
			Hypothesis:  "集群应在节点恢复后重新同步状态",
			Duration:    30 * time.Second,
		},
		{
			Name:        "state-pressure",
			Description: "高频分布式状态写入压力",
			Hypothesis:  "状态存储应在压力下保持稳定",
			Duration:    20 * time.Second,
		},
	}

	// 创建 SoakChaosRunner
	runner := chaos.NewSoakChaosRunner(chaos.SoakChaosConfig{
		SoakDuration:         soakDuration,
		ChaosInterval:        5 * time.Minute,
		ChaosDuration:        30 * time.Second,
		Experiments:          experiments,
		RequestFn:            requestFn,
		RequestsPerSecond:    10,
		DegradationThreshold: 100.0, // 延迟退化 100% 才视为退化
		StopOnDegradation:    false,
	})

	// 运行浸泡测试
	t.Log("开始浸泡测试...")
	result := runner.Run(context.Background())

	// 记录最终内存
	endMem := getMemoryMB()

	// 输出结果摘要
	t.Log("===== 浸泡测试结果 =====")
	t.Logf("实际持续时间: %v", result.Duration)
	t.Logf("总请求数: %d", result.TotalRequests)
	t.Logf("总错误数: %d", result.TotalErrors)
	t.Logf("错误率: %.2f%%", result.ErrorRate()*100)
	t.Logf("平均延迟: %.1f ms", result.AvgLatencyMs())
	t.Logf("采样点数: %d", len(result.Samples))
	t.Logf("混沌实验数: %d", len(result.ChaosResults))
	t.Logf("退化检测: %v", result.DegradationDetected)
	t.Logf("起始内存: %.1f MB", startMem)
	t.Logf("结束内存: %.1f MB", endMem)

	// 验证：内存泄漏检测
	assertNoMemoryLeak(t, startMem, endMem, memoryLeakThreshold)

	// 验证：错误率应低于 1%
	if result.ErrorRate() > 0.01 {
		t.Errorf("错误率过高: %.2f%% (阈值 1%%)", result.ErrorRate()*100)
	}

	// 验证：节点存活率
	survivingNodes := 0
	for i := 0; i < nodeCount; i++ {
		if tc.managers[i] != nil {
			nodes := tc.managers[i].ListNodes()
			if len(nodes) > 0 {
				survivingNodes++
			}
		}
	}
	if survivingNodes < nodeCount {
		t.Errorf("节点存活率不足: %d/%d", survivingNodes, nodeCount)
	} else {
		t.Logf("所有 %d 个节点均存活", nodeCount)
	}

	// 验证：未检测到退化
	if result.DegradationDetected {
		t.Errorf("检测到性能退化: %s", result.DegradationDetails)
	}

	// 验证：总请求数 > 0（确保负载确实在运行）
	if result.TotalRequests == 0 {
		t.Error("浸泡测试期间未产生任何请求")
	}
}
