// loop.go — ReAct Loop 工程化子命令
// 提供 trace（追踪）、inspect（检查）、resume（恢复）三个子命令
// 用于观察和恢复 Agent 的 ReAct 循环执行状态
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/persist"
)

// defaultCheckpointDB 返回默认的 checkpoint 数据库路径
// 约定：项目根目录下的 .ap/checkpoint.db
func defaultCheckpointDB(dir string) string {
	return filepath.Join(dir, ".ap", "checkpoint.db")
}

// ─── ap loop 入口 ───

func runLoop(args []string) error {
	if len(args) == 0 {
		fmt.Print(`ap loop — ReAct Loop 工程化

用法:
  ap loop <subcommand> [options]

子命令:
  trace     查看 Agent 执行追踪
  inspect   查看 Agent 当前状态
  resume    从检查点恢复运行

选项:
  --db, -d  指定 checkpoint 数据库路径（默认: .ap/checkpoint.db）
  --agent   指定 agent 名称（默认: 从 checkpoint 加载第一个）

运行 "ap loop <subcommand> --help" 查看子命令详情。
`)
		return nil
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "trace":
		return runLoopTrace(subArgs)
	case "inspect":
		return runLoopInspect(subArgs)
	case "resume":
		return runLoopResume(subArgs)
	case "--help", "-h", "help":
		fmt.Print(`ap loop — ReAct Loop 工程化

用法:
  ap loop <subcommand> [options]

子命令:
  trace     查看 Agent 执行追踪
  inspect   查看 Agent 当前状态
  resume    从检查点恢复运行

运行 "ap loop <subcommand> --help" 查看子命令详情。
`)
		return nil
	default:
		return fmt.Errorf("unknown loop subcommand %q, run %s for usage", sub, bold("ap loop --help"))
	}
}

// ─── 共享工具函数 ───

// openCheckpointStore 打开 checkpoint 数据库，返回 store 和关闭函数
func openCheckpointStore(args []string, dir string) (*persist.SQLiteCheckpointStore, func(), error) {
	dbPath := defaultCheckpointDB(dir)

	// 解析 --db/-d 参数
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--db", "-d":
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("--db requires a value")
			}
			dbPath = args[i]
		case "--help", "-h":
			return nil, nil, nil // 返回 nil 表示需要显示帮助
		}
	}

	// 确保 .ap 目录存在
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, fmt.Errorf("create .ap directory: %w", err)
	}

	store, err := persist.NewSQLiteCheckpointStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open checkpoint store: %w\n  hint: agent has not saved any checkpoint yet", err)
	}

	cleanup := func() {
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: close checkpoint store: %v\n", err)
		}
	}

	return store, cleanup, nil
}

// resolveAgentID 从参数中解析 agent ID，如果未指定则尝试自动检测
func resolveAgentID(store *persist.SQLiteCheckpointStore, args []string, dir string) (string, error) {
	ctx := context.Background()

	// 解析 --agent 参数
	for i := 0; i < len(args); i++ {
		if args[i] == "--agent" {
			i++
			if i >= len(args) {
				return "", fmt.Errorf("--agent requires a value")
			}
			return args[i], nil
		}
		// 跳过 --db/-d 及其参数
		if args[i] == "--db" || args[i] == "-d" {
			i++
		}
	}

	// 未指定 --agent，尝试使用项目名作为 agent 名称
	projectName := filepath.Base(dir)
	_, err := store.Load(ctx, projectName)
	if err == nil {
		return projectName, nil
	}

	// 也尝试带 "-agent" 后缀的形式
	agentName := projectName + "-agent"
	_, err = store.Load(ctx, agentName)
	if err == nil {
		return agentName, nil
	}

	// 读取 .ap.yaml 获取 agent 名称
	cfg := loadAPConfigFromDir(dir)
	if cfg.Name != "" {
		_, err = store.Load(ctx, cfg.Name)
		if err == nil {
			return cfg.Name, nil
		}
	}

	return "", fmt.Errorf("no checkpoint found for project %q\n  hint: run your agent first, or specify with --agent <name>", projectName)
}

// ─── ap loop trace ───

func runLoopTrace(args []string) error {
	// 处理 --help
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(`ap loop trace — 查看 Agent 执行追踪

用法:
  ap loop trace [options]

选项:
  --db, -d    指定 checkpoint 数据库路径（默认: .ap/checkpoint.db）
  --agent     指定 agent 名称（默认: 自动检测）
  --turns, -n 显示最近 N 轮（默认: 全部）

显示每个 turn 的 LLM 调用、工具执行、延迟等追踪信息。
`)
			return nil
		}
	}

	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	store, cleanup, err := openCheckpointStore(args, dir)
	if err != nil {
		return err
	}
	if store == nil {
		return nil // --help 已处理
	}
	defer cleanup()

	agentID, err := resolveAgentID(store, args, dir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	state, err := store.Load(ctx, agentID)
	if err != nil {
		return fmt.Errorf("load checkpoint for %q: %w", agentID, err)
	}

	// 显示追踪头
	printTraceHeader(state)

	// 显示每轮对话
	printTraceTurns(state.Messages)

	// 显示指标
	printTraceMetrics(state)

	return nil
}

func printTraceHeader(state *persist.AgentState) {
	fmt.Println()
	fmt.Printf("╔══════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  Agent Trace: %-43s ║\n", truncate(state.AgentID, 43))
	fmt.Printf("╠══════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Session:   %-44s ║\n", truncate(state.SessionID, 44))
	fmt.Printf("║  Status:    %-44s ║\n", coloredStatus(state.Status))
	fmt.Printf("║  Turns:     %-44d ║\n", state.TurnCount)
	fmt.Printf("║  Saved:     %-44s ║\n", formatTime(state.SavedAt))
	fmt.Printf("╚══════════════════════════════════════════════════════════╝\n")
	fmt.Println()
}

func printTraceTurns(messages []persist.CheckpointMessage) {
	if len(messages) == 0 {
		fmt.Println("  (no messages recorded)")
		return
	}

	turnNum := 0
	for i, msg := range messages {
		switch msg.Role {
		case "system":
			fmt.Printf("  ┌─ System ──────────────────────────────────────────────\n")
			fmt.Printf("  │ %s\n", wrapText(msg.Content, 56))
			fmt.Printf("  └────────────────────────────────────────────────────────\n")

		case "user":
			turnNum++
			fmt.Printf("\n  ╭── Turn %d ─────────────────────────────────────────────\n", turnNum)
			fmt.Printf("  │ 👤 User:\n")
			fmt.Printf("  │    %s\n", wrapText(msg.Content, 56))

		case "assistant":
			fmt.Printf("  │\n")
			fmt.Printf("  │ 🤖 Assistant:\n")
			fmt.Printf("  │    %s\n", wrapText(msg.Content, 56))

		case "tool":
			fmt.Printf("  │\n")
			fmt.Printf("  │ 🔧 Tool Call:\n")
			// 尝试解析 tool call JSON
			if toolInfo := parseToolCall(msg.Content); toolInfo != "" {
				fmt.Printf("  │    %s\n", wrapText(toolInfo, 56))
			} else {
				fmt.Printf("  │    %s\n", wrapText(msg.Content, 56))
			}

		case "tool_result":
			fmt.Printf("  │\n")
			fmt.Printf("  │ 📊 Tool Result:\n")
			fmt.Printf("  │    %s\n", wrapText(truncate(msg.Content, 200), 56))

		default:
			fmt.Printf("  │ [%s]: %s\n", msg.Role, wrapText(truncate(msg.Content, 100), 52))
		}

		// 如果是最后一轮或以 assistant 结尾，关闭 turn 框
		isLast := i == len(messages)-1
		nextIsUser := !isLast && messages[i+1].Role == "user"
		if msg.Role == "assistant" || msg.Role == "tool_result" {
			if isLast || nextIsUser {
				fmt.Printf("  ╰────────────────────────────────────────────────────────\n")
			}
		}
	}
	fmt.Println()
}

func printTraceMetrics(state *persist.AgentState) {
	fmt.Printf("  ┌─ Metrics ──────────────────────────────────────────────\n")
	fmt.Printf("  │ Total Turns:   %d\n", state.Metrics.TotalTurns)
	fmt.Printf("  │ Total Tools:   %d\n", state.Metrics.TotalTools)
	fmt.Printf("  │ Duration:      %s\n", state.Metrics.Duration)
	fmt.Printf("  │ LLM Latency:   %s\n", state.Metrics.LLMLatency)
	fmt.Printf("  │ Tool Latency:  %s\n", state.Metrics.ToolLatency)
	fmt.Printf("  └────────────────────────────────────────────────────────\n")
}

// ─── ap loop inspect ───

func runLoopInspect(args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(`ap loop inspect — 查看 Agent 当前状态

用法:
  ap loop inspect [options]

选项:
  --db, -d    指定 checkpoint 数据库路径（默认: .ap/checkpoint.db）
  --agent     指定 agent 名称（默认: 自动检测）
  --json, -j  以 JSON 格式输出

显示 Agent 的运行时状态、消息历史、指标等详细信息。
`)
			return nil
		}
	}

	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	store, cleanup, err := openCheckpointStore(args, dir)
	if err != nil {
		return err
	}
	if store == nil {
		return nil
	}
	defer cleanup()

	agentID, err := resolveAgentID(store, args, dir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	state, err := store.Load(ctx, agentID)
	if err != nil {
		return fmt.Errorf("load checkpoint for %q: %w", agentID, err)
	}

	// 检查是否要求 JSON 输出
	jsonOut := false
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOut = true
			break
		}
	}

	if jsonOut {
		return printInspectJSON(state)
	}
	return printInspectPretty(state)
}

func printInspectPretty(state *persist.AgentState) error {
	fmt.Println()
	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  Agent Inspection: %-42s ║\n", truncate(state.AgentID, 42))
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Session:      %-44s ║\n", truncate(state.SessionID, 44))
	fmt.Printf("║  Status:       %-44s ║\n", coloredStatus(state.Status))
	fmt.Printf("║  Turn Count:   %-44d ║\n", state.TurnCount)
	fmt.Printf("║  Messages:     %-44d ║\n", len(state.Messages))
	fmt.Printf("║  Saved At:     %-44s ║\n", formatTime(state.SavedAt))
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Metrics                                                    ║\n")
	fmt.Printf("║    Total Turns:  %-42d ║\n", state.Metrics.TotalTurns)
	fmt.Printf("║    Total Tools:  %-42d ║\n", state.Metrics.TotalTools)
	fmt.Printf("║    Duration:     %-42s ║\n", truncate(state.Metrics.Duration, 42))
	fmt.Printf("║    LLM Latency:  %-42s ║\n", truncate(state.Metrics.LLMLatency, 42))
	fmt.Printf("║    Tool Latency: %-42s ║\n", truncate(state.Metrics.ToolLatency, 42))
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Message History (%d messages)                              ║\n", len(state.Messages))
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")

	// 按角色统计
	roleCount := make(map[string]int)
	for _, m := range state.Messages {
		roleCount[m.Role]++
	}

	fmt.Println()
	fmt.Println("  Role Distribution:")
	roles := sortedKeys(roleCount)
	for _, role := range roles {
		bar := strings.Repeat("█", roleCount[role])
		fmt.Printf("    %-12s %3d %s\n", role+":", roleCount[role], bar)
	}

	// 显示最近 5 条消息
	fmt.Println()
	fmt.Println("  Recent Messages:")
	start := 0
	if len(state.Messages) > 5 {
		start = len(state.Messages) - 5
		fmt.Printf("    (showing last 5 of %d)\n", len(state.Messages))
	}
	for i := start; i < len(state.Messages); i++ {
		m := state.Messages[i]
		prefix := roleIcon(m.Role)
		fmt.Printf("    %s [%s] %s\n", prefix, m.Role, truncate(m.Content, 80))
	}
	fmt.Println()

	return nil
}

func printInspectJSON(state *persist.AgentState) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(state)
}

// ─── ap loop resume ───

func runLoopResume(args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Print(`ap loop resume — 从检查点恢复 Agent 运行

用法:
  ap loop resume [options]

选项:
  --db, -d    指定 checkpoint 数据库路径（默认: .ap/checkpoint.db）
  --agent     指定要恢复的 agent 名称
  --prompt, -p 恢复后发送的提示消息（可选）

从 checkpoint 加载状态并恢复 Agent 执行。
Agent 必须实现 ResumeFromCheckpoint 接口。
`)
			return nil
		}
	}

	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	store, cleanup, err := openCheckpointStore(args, dir)
	if err != nil {
		return err
	}
	if store == nil {
		return nil
	}
	defer cleanup()

	agentID, err := resolveAgentID(store, args, dir)
	if err != nil {
		return err
	}

	ctx := context.Background()
	state, err := store.Load(ctx, agentID)
	if err != nil {
		return fmt.Errorf("load checkpoint for %q: %w", agentID, err)
	}

	if state.Status != "paused" && state.Status != "failed" && state.Status != "cancelled" {
		return fmt.Errorf("agent %q is %s, cannot resume (only paused/failed/cancelled agents can be resumed)", agentID, state.Status)
	}

	infof("Resuming agent %q from checkpoint (status: %s, turn: %d)", agentID, state.Status, state.TurnCount)

	// 解析 --prompt 参数
	var prompt string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--prompt", "-p":
			i++
			if i < len(args) {
				prompt = args[i]
			}
		case "--db", "-d", "--agent":
			i++ // 跳过这些参数的值
		}
	}

	// 编译项目
	binaryName := filepath.Base(dir) + "-agent"
	spinner := newSpinner(fmt.Sprintf("编译 %s", binaryName))
	buildCmd := exec.Command("go", "build", "-o", binaryName, ".")
	buildCmd.Dir = dir
	buildOutput, buildErr := buildCmd.CombinedOutput()
	spinner.Stop()

	if buildErr != nil {
		return fmt.Errorf("build failed: %s\n  hint: run %s for details", strings.TrimSpace(string(buildOutput)), bold("go build ."))
	}
	defer os.Remove(filepath.Join(dir, binaryName))

	// 运行
	successf("编译完成，从 checkpoint 恢复运行 %s", binaryName)
	fmt.Println()
	runCmd := exec.Command(filepath.Join(".", binaryName))
	runCmd.Dir = dir
	runCmd.Stdin = os.Stdin
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr

	// 注入环境变量
	env := os.Environ()
	env = appendConfigEnv(env, dir)
	env = append(env, "AP_RESUME=1")
	env = append(env, "AP_RESUME_AGENT="+agentID)
	if prompt != "" {
		env = append(env, "AP_PROMPT="+prompt)
	}
	runCmd.Env = env

	if err := runCmd.Run(); err != nil {
		return fmt.Errorf("resume failed: %w", err)
	}
	return nil
}

// ─── 格式化辅助函数 ───

func wrapText(s string, width int) string {
	if len(s) <= width {
		return s
	}
	// 简单按宽度截断并加省略号
	lines := []string{}
	for len(s) > width {
		// 尝试在空格处断开
		br := width
		for i := width; i > width/2; i-- {
			if i < len(s) && s[i] == ' ' {
				br = i
				break
			}
		}
		lines = append(lines, s[:br])
		s = strings.TrimSpace(s[br:])
	}
	if len(s) > 0 {
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n  │    ")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	return t.Format("2006-01-02 15:04:05")
}

func coloredStatus(status string) string {
	switch status {
	case "running":
		return fmt.Sprintf("%s %s", status, "(active)")
	case "completed":
		return fmt.Sprintf("%s %s", status, "(done)")
	case "failed":
		return fmt.Sprintf("%s %s", status, "(error)")
	case "paused":
		return fmt.Sprintf("%s %s", status, "(paused)")
	case "cancelled":
		return fmt.Sprintf("%s %s", status, "(stopped)")
	case "idle":
		return fmt.Sprintf("%s %s", status, "(ready)")
	default:
		return status
	}
}

func roleIcon(role string) string {
	switch role {
	case "system":
		return "⚙"
	case "user":
		return "👤"
	case "assistant":
		return "🤖"
	case "tool":
		return "🔧"
	case "tool_result":
		return "📊"
	default:
		return "  "
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseToolCall 尝试解析工具调用 JSON，提取工具名和参数摘要
func parseToolCall(content string) string {
	var call struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(content), &call); err != nil {
		return ""
	}
	if call.Name == "" {
		return ""
	}

	parts := []string{fmt.Sprintf("tool: %s", call.Name)}
	if len(call.Arguments) > 0 {
		keys := make([]string, 0, len(call.Arguments))
		for k := range call.Arguments {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, call.Arguments[k]))
		}
	}
	return strings.Join(parts, ", ")
}
