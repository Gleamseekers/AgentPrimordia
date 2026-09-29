// main.go — AP 用 AP 开发 AP 自举演示（v3.6-4）
//
// 用 AgentPrimordia 框架（真实 ReActAgent + 共享记忆）反复解决
// 真实 harness 基准集任务，输出成功率曲线（round → pass_rate）。
// 由于跨任务记忆（v3.6-3）与模型经验积累，成功率曲线可见上升。
//
// 用法：
//
//	go run ./bench/self-bootstrap --rounds 5
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/self_bootstrap"
)

func main() {
	var (
		rounds = flag.Int("rounds", 5, "运行轮数")
		limit  = flag.Int("limit", 12, "使用的用例数（0=全部）")
	)
	flag.Parse()

	cases := eval.MustBenchmarkCases()
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}

	report, err := self_bootstrap.RunBootstrap(context.Background(), self_bootstrap.BootstrapConfig{
		Cases:  cases,
		Rounds: *rounds,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "自举失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("==> AP 用 AP 开发 AP（自举）成功率曲线\n")
	fmt.Printf("    用例数: %d  轮数: %d\n\n", len(cases), *rounds)
	fmt.Printf("    round | pass_rate | memory_hits\n")
	fmt.Printf("    ------+-----------+------------\n")
	for _, rr := range report.Rounds {
		fmt.Printf("    %5d |   %.3f    | %d\n", rr.Round, rr.PassRate, rr.MemoryHits)
	}
	fmt.Printf("\n    起始成功率: %.3f  结束成功率: %.3f  曲线上升: %v\n",
		report.Started, report.Ended, report.Rising)
	if report.Rising {
		fmt.Printf("    ✅ 自举成功：AP 用 AP 开发 AP，成功率曲线可见上升\n")
	} else {
		fmt.Printf("    ⚠️ 成功率曲线未上升，检查自举机制\n")
	}
}
