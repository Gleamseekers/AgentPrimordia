package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// interopReportDoc 入库用的协议符合性报告文档（bench/results/a2a-interop/）。
type interopReportDoc struct {
	// GeneratedAt 报告生成时间（UTC，RFC3339）。
	GeneratedAt string `json:"generated_at"`
	// FrameworkVersion 生成时框架版本（pkg/agent.go const Version 真值）。
	FrameworkVersion string `json:"framework_version"`
	// Generator 报告生成方式（可复现命令）。
	Generator string `json:"generator"`
	// GenerationMode 生成模式：离线静态分析（无需运行中的端点）。
	GenerationMode string `json:"generation_mode"`
	// AgentCard 受检 Agent Card（与 demo server 在线暴露的 card 同一构造函数）。
	AgentCard ap.OpenAgentCard `json:"agent_card"`
	// InteropConfig 受检互操作配置。
	InteropConfig ap.InteropConfig `json:"interop_config"`
	// Report 协议符合性报告本体。
	Report ap.InteropReport `json:"report"`
}

// runReport 离线生成协议符合性报告（JSON 写 stdout）。
//
// 口径：GenerateInteropReport 是对 card + config 的静态符合性分析，
// 不需要运行中的端点；card 与 demo server 共用 buildCard，因此本报告
// 即「demo server 在线暴露面」的符合性证据。用法：
//
//	go run ./agentprimordia/ecosystem/examples/a2a-handoff report \
//	  > agentprimordia/bench/results/a2a-interop/a2a-interop-report.json
func runReport() error {
	addr := envOr("AP_A2A_HANDOFF_ADDR", "127.0.0.1:9797")
	card := buildCard(addr)
	cfg := ap.DefaultInteropConfig()
	report := ap.GenerateInteropReport(card, cfg)

	doc := interopReportDoc{
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		FrameworkVersion: ap.Version,
		Generator: "go run ./agentprimordia/ecosystem/examples/a2a-handoff report" +
			"（GenerateInteropReport 离线静态分析，无需运行端点）",
		GenerationMode: "offline-static-analysis",
		AgentCard:      card,
		InteropConfig:  cfg,
		Report:         report,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("序列化报告失败: %w", err)
	}

	// 人读摘要走 stderr，避免污染 stdout 的 JSON（重定向入库时）
	fmt.Fprintf(os.Stderr, "符合性得分 %.0f%%（%d/%d 项通过）\n",
		report.Score*100, len(report.Checks)-len(report.FailedChecks()), len(report.Checks))
	return nil
}
