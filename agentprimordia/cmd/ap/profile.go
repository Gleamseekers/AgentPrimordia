package main

import (
	"fmt"
	"os"

	"agentprimordia/internal/memory"
)

func runProfile(args []string) error {
	subcmd := "show"
	if len(args) > 0 {
		switch args[0] {
		case "show", "history", "export":
			subcmd = args[0]
		case "--help", "-h":
			printProfileHelp()
			return nil
		default:
			return fmt.Errorf("unknown profile subcommand %q", args[0])
		}
	}

	switch subcmd {
	case "show":
		return showProfile()
	case "history":
		return showGrowthHistory()
	case "export":
		return exportProfile()
	}
	return nil
}

func showProfile() error {
	dir, err := findProjectDir()
	if err != nil {
		return fmt.Errorf("未找到项目目录: %w", err)
	}

	cfg := loadAPConfigFromDir(dir)
	dbPath := "./data/memory.db"
	if cfg.Memory != nil && cfg.Memory.Path != "" {
		dbPath = cfg.Memory.Path
	}

	// 读取 SelfModel（从 SQLite 记忆库）
	sm := loadSelfModelFromDB(dbPath)
	snap := sm.Snapshot()

	// 输出报告
	fmt.Println()
	fmt.Println("Agent Growth Profile")
	fmt.Println("====================")
	fmt.Println()

	// 总体表现
	fmt.Printf("Overall: %d%% success (%d tasks)", int(snap.SuccessRate*100), snap.TotalTasks)
	if snap.TotalTasks > 0 {
		fmt.Printf(", avg %.1f turns", snap.AvgTurns)
	}
	fmt.Println()
	fmt.Println()

	// Top 能力
	if len(snap.Capabilities) > 0 {
		fmt.Println("Top Capabilities:")
		limit := 5
		if len(snap.Capabilities) < limit {
			limit = len(snap.Capabilities)
		}
		for i := 0; i < limit; i++ {
			cs := snap.Capabilities[i]
			total := cs.Successes + cs.Failures
			trendIcon := trendIndicator(cs.Trend)
			fmt.Printf("  %d. %-20s %3.0f%% (%d/%d)  %s\n",
				i+1, cs.Domain, cs.SuccessRate*100, cs.Successes, total, trendIcon)
		}
		fmt.Println()
	}

	// 弱项
	weakCount := 0
	for i := len(snap.Capabilities) - 1; i >= 0; i-- {
		cs := snap.Capabilities[i]
		if cs.SuccessRate < 0.6 && cs.Successes+cs.Failures >= 3 {
			weakCount++
		}
	}
	if weakCount > 0 {
		fmt.Println("Weak Areas:")
		shown := 0
		for i := len(snap.Capabilities) - 1; i >= 0 && shown < 3; i-- {
			cs := snap.Capabilities[i]
			if cs.SuccessRate < 0.6 && cs.Successes+cs.Failures >= 3 {
				total := cs.Successes + cs.Failures
				trendIcon := trendIndicator(cs.Trend)
				fmt.Printf("  %d. %-20s %3.0f%% (%d/%d)  %s\n",
					shown+1, cs.Domain, cs.SuccessRate*100, cs.Successes, total, trendIcon)
				shown++
			}
		}
		fmt.Println()
	}

	// 失败模式
	if len(snap.TopFailures) > 0 {
		fmt.Println("Top Failure Patterns:")
		for i, fp := range snap.TopFailures {
			if i >= 3 {
				break
			}
			fmt.Printf("  %d. %s (%d times)", i+1, fp.Signature, fp.Count)
			if fp.Mitigation != "" {
				fmt.Printf(" — %s", fp.Mitigation)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// 零状态提示
	if snap.TotalTasks == 0 {
		fmt.Println("Agent 尚未积累学习数据。")
		fmt.Println("开始使用 agent 完成任务后，成长数据将自动记录。")
		fmt.Println()
		fmt.Println("提示:")
		fmt.Println("  - 运行 ap run 开始与 agent 对话")
		fmt.Println("  - 设置 AP_LLM_API_KEY 启用真实 LLM")
		fmt.Println()
	}

	return nil
}

func showGrowthHistory() error {
	dir, err := findProjectDir()
	if err != nil {
		return fmt.Errorf("未找到项目目录: %w", err)
	}

	cfg := loadAPConfigFromDir(dir)
	dbPath := "./data/memory.db"
	if cfg.Memory != nil && cfg.Memory.Path != "" {
		dbPath = cfg.Memory.Path
	}

	growthDB, err := memory.NewGrowthEventLog(dbPath)
	if err != nil {
		// 成长事件表可能不存在，显示空状态
		fmt.Println("Growth History")
		fmt.Println("==============")
		fmt.Println()
		fmt.Println("暂无成长事件记录。")
		return nil
	}
	defer growthDB.Close()

	events, err := growthDB.Recent(20)
	if err != nil {
		// 表不存在时优雅降级
		fmt.Println("Growth History")
		fmt.Println("==============")
		fmt.Println()
		fmt.Println("暂无成长事件记录。")
		return nil
	}

	fmt.Println("Growth History")
	fmt.Println("==============")
	fmt.Println()

	if len(events) == 0 {
		fmt.Println("暂无成长事件记录。")
		return nil
	}

	for _, e := range events {
		date := e.CreatedAt.Format("01-02")
		fmt.Printf("  [%s] %s — %s\n", date, e.Category, e.Title)
	}
	fmt.Println()
	return nil
}

func exportProfile() error {
	dir, err := findProjectDir()
	if err != nil {
		return fmt.Errorf("未找到项目目录: %w", err)
	}

	cfg := loadAPConfigFromDir(dir)
	sm := loadSelfModelFromDB("./data/memory.db")
	if cfg.Memory != nil && cfg.Memory.Path != "" {
		sm = loadSelfModelFromDB(cfg.Memory.Path)
	}

	data, err := sm.Marshal()
	if err != nil {
		return fmt.Errorf("导出失败: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// loadSelfModelFromDB 从 SQLite 数据库加载 SelfModel。
// 目前返回空的 SelfModel（未来接入持久化数据）。
func loadSelfModelFromDB(dbPath string) *memory.SelfModel {
	sm := memory.NewSelfModel()

	// 检查数据库文件是否存在
	if _, err := os.Stat(dbPath); err != nil {
		return sm
	}

	// TODO: 从 SQLite 反序列化 SelfModel 数据
	// 当前 SelfModel 是内存态的，持久化接入后此处读取
	return sm
}

func trendIndicator(trend string) string {
	switch trend {
	case "improving":
		return "improving"
	case "declining":
		return "declining"
	default:
		return "stable"
	}
}

func printProfileHelp() {
	fmt.Print(`ap profile — display agent growth profile

用法:
  ap profile [subcommand]

子命令:
  show      显示 agent 成长报告 (默认)
  history   显示成长事件历史
  export    导出 SelfModel 原始数据 (JSON)

示例:
  ap profile
  ap profile history
  ap profile export > profile.json
`)
}
