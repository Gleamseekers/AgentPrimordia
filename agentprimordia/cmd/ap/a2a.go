package main

import (
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/a2a"
)

const a2aUsage = `Usage: ap a2a <subcommand> [arguments]

Subcommands:
  interop-check    验证当前部署的开放协议兼容性报告

Examples:
  ap a2a interop-check
`

func runA2A(args []string) error {
	if len(args) == 0 {
		fmt.Print(a2aUsage)
		return nil
	}

	sub := args[0]
	switch sub {
	case "interop-check":
		return runA2AInteropCheck(args[1:])
	case "--help", "-h", "help":
		fmt.Print(a2aUsage)
		return nil
	default:
		return fmt.Errorf("unknown a2a subcommand %q, run \"ap a2a --help\"", sub)
	}
}

func runA2AInteropCheck(args []string) error {
	_ = args
	// 构造当前部署的 Agent Card 与互操作配置（默认值，真实部署应从配置加载）
	card := a2a.OpenAgentCard{
		Name:               "ap-agent",
		Description:        "AgentPrimordia agent",
		URL:                "http://localhost:8080",
		Version:            "3.5.0",
		Capabilities:       a2a.OpenCapabilities{Streaming: true},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	cfg := a2a.DefaultInteropConfig()

	report := a2a.GenerateInteropReport(card, cfg)

	fmt.Println("🌐 开放 A2A 协议兼容性报告")
	fmt.Printf("   模式: %s | 符合性得分: %.0f%%\n\n", report.Mode, report.Score*100)
	for _, c := range report.Checks {
		mark := "✅"
		if !c.Passed {
			mark = "❌"
		}
		fmt.Printf("   %s %-28s %s\n", mark, c.Name, c.Detail)
	}

	if failed := report.FailedChecks(); len(failed) > 0 {
		fmt.Printf("\n⚠️  %d 项未通过，建议补齐以达到完全兼容\n", len(failed))
	} else {
		fmt.Println("\n✅ 全部检查通过，符合开放 A2A 协议规范")
	}
	return nil
}
