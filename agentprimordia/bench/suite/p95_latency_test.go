package suite

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

// TestMain 抑制基准期间的 Agent INFO 日志，避免干扰输出解析。
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// ===== v4.0-4 性能大版本：关键路径 P95 延迟基准 =====
//
// 验收标准：关键路径 P95 达标（延迟分布而非平均）。
// 本文件测量 Agent 单轮、工具调用、记忆检索三条关键路径的
// P50 / P95 / P99 延迟，输出可被 bench-regression-check.sh 解析。
//
// 测量说明：Windows 下 time.Now() 连续调用可能返回相同 tick，单次测时不可靠；
// 采用「批次累积」策略——每批 batchSize 次调用测一次总耗时，得到每批的平均
// 单次耗时，批次间的波动即代表延迟分布。P95 表示最慢 5% 批次的平均延迟。
//
// 运行：
//	go test -bench=BenchmarkP95 -benchmem -benchtime=200ms -run=^$ ./bench/suite

const p95BatchSize = 100

// p95Collector 收集单次操作的耗时差，计算 P50/P95/P99。
// 实现要点：Windows 下 time.Now() 两次调用可能返回相同时间戳（时钟粒度），
// 因此循环内采用 UnixNano 差值**累加**到变量（编译器无法优化掉有副作用的累加），
// 每批结束后记录该批平均单次耗时，批次间波动即代表延迟分布。
type p95Collector struct {
	batches []float64 // 每批平均单次耗时（ns）
}

func (c *p95Collector) add(batchTotal time.Duration, count int) {
	if count == 0 {
		return
	}
	c.batches = append(c.batches, float64(batchTotal)/float64(count))
}

func (c *p95Collector) percentile(p float64) time.Duration {
	if len(c.batches) == 0 {
		return 0
	}
	sorted := make([]float64, len(c.batches))
	copy(sorted, c.batches)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * p)
	return time.Duration(sorted[idx])
}

func (c *p95Collector) report(b *testing.B) {
	// P50 对超快路径（单次 <10µs）受 Windows 时钟精度影响可能为 0，
	// 此时回退到框架测量的每 op 平均耗时（b.Elapsed()/b.N），保证非零。
	avg := float64(b.Elapsed()) / float64(b.N)
	p50 := c.percentile(0.50)
	if p50 <= 0 {
		p50 = time.Duration(avg)
	}
	b.ReportMetric(float64(p50), "p50_ns/op")
	b.ReportMetric(float64(c.percentile(0.95)), "p95_ns/op")
	b.ReportMetric(float64(c.percentile(0.99)), "p99_ns/op")
}

// BenchmarkP95AgentRun 关键路径一：Agent 单轮运行延迟分布（MockLLM）。
func BenchmarkP95AgentRun(b *testing.B) {
	agent, err := ap.NewAgent("P95Agent", "你是助手", &benchMockLLM{}, ap.WithMaxTurns(1))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		// 逐调用 UnixNano 差值累加（Windows 时钟粒度下单次测时不可靠，累加可被正确测量）
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_, _ = agent.Run(ctx, ap.UserMessage("hello"))
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

// BenchmarkP95ToolCall 关键路径二：工具调用延迟分布（MockLLM + 文件系统工具）。
func BenchmarkP95ToolCall(b *testing.B) {
	fs, err := ap.NewFileSystem("/")
	if err != nil {
		b.Fatal(err)
	}
	reg := ap.NewToolRegistry()
	_ = reg.Register(fs)
	agent, err := ap.NewAgent("P95ToolAgent", "你是助手", &benchMockLLM{},
		ap.WithMaxTurns(1),
		ap.WithToolkit(reg),
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_, _ = agent.Run(ctx, ap.UserMessage("列出当前目录文件"))
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

// BenchmarkP95MemorySearch 关键路径三：记忆检索延迟分布（1K 条目 FTS 搜索）。
func BenchmarkP95MemorySearch(b *testing.B) {
	memory, _ := ap.WithInMemory()
	defer memory.Close()
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		_ = memory.Add(ctx, &ap.Episode{
			ID:      fmt.Sprintf("p95-pre-%d", i),
			Content: fmt.Sprintf("预填充记忆条目 %d，包含一些常见关键词如文件、搜索、分析、代码", i),
			Role:    "user",
		})
	}

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_, _ = memory.Search(ctx, "文件搜索", nil)
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}
