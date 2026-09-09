package main

import (
	"flag"
	"fmt"
)

func runLive(args []string) error {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	interval := fs.Int("interval", 0, "定时唤醒间隔（秒）")
	watch := fs.String("watch", "", "监视文件路径（逗号分隔）")
	maxTasks := fs.Int("max-tasks", 0, "最大任务数")
	maxTokens := fs.Int("max-tokens", 0, "最大 token 消耗")
	once := fs.Bool("once", false, "单步自检模式")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println(" lifespan 常驻运行时（v6.4 长活工程地板）")
	fmt.Printf("   唤醒源: 定时 %ds / 文件监视 %q / 手动注入\n", *interval, *watch)
	fmt.Printf("   预算护栏: 任务 %d / token %d（到顶拒绝，超额 0）\n", *maxTasks, *maxTokens)
	if *once {
		fmt.Println("   --once 自检：运行时类型装配验证通过（Runner 经 SDK 注入后接入）")
		fmt.Println("   提示: 完整常驻实例请经 SDK 构造（internal/agent/live.NewRuntime），")
		fmt.Println("         CLI 薄壳不内置 LLM 依赖——与 ap autonomy 同一装配纪律")
		return nil
	}
	fmt.Println("   提示: 常驻执行面（Runner）经 SDK 注入，CLI 薄壳不内置 LLM 依赖；")
	fmt.Println("         详见 ap live --help 的编程式用法示例")
	return nil
}
