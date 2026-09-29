// Package main 演示 AgentPrimordia 在真实业务场景下的能力 —
// GitHub Issue 自动 Triage Bot。
//
// 场景: Agent 自动读取 open issues，分类（bug/feature/question/duplicate），
//
//	添加 label，输出最终报告。
//
// 架构: ReAct Agent + 3 个 GitHub 工具 + 内存仓库（mock server）
//
// 运行:
//
//	cd agentprimordia
//	export OPENAI_API_KEY=sk-xxx                    # 或 QWEN_API_KEY / DEEPSEEK_API_KEY
//	go run ./ecosystem/examples/github-issue-triage/
//
// 接入真实 GitHub API（需同时设置 GITHUB_TOKEN 与 LLM API Key）：
//
//	export GITHUB_TOKEN=ghp_xxx                    # 需要 issues 读 + label 写权限
//	export GH_REPO=owner/repo                      # 可选，默认 owner/repo
//	go run ./ecosystem/examples/github-issue-triage/
//
// 无 API Key 也能跑（自动用 mock LLM，演示工具调用流程 + 预期结果）
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/testutil"
)

const systemPrompt = `你是 AgentPrimordia 项目的 GitHub Issue Triage 助手。

# 任务
1. 调用 list_issues 获取所有 open issues
2. 对每个 issue 调用 read_issue 阅读详情
3. 分类决策（bug / feature / question / duplicate）
4. 给出推荐 labels（包含分类 label + 必要的 priority/platform 等）
5. 调用 add_label 应用 label
6. 最后输出一份 Markdown 报告

# 判断规则
- bug: 报告错误、崩溃、异常行为（如 panic、stack trace、error message）
- feature: 新功能请求、改进建议
- question: 用法咨询、配置问题
- duplicate: 明确提到 "same as #N" 或与已有 issue 重复
- confidence: 0.0-1.0
- reasoning: 简短解释（1 句话）

# 推荐 label 规则
- bug → ["bug"] + 可选 ["priority:high"]（如果 panic/崩溃/数据丢失）
- feature → ["enhancement"]
- question → ["question"]
- duplicate → ["duplicate"]
- 平台相关 bug → ["bug", "platform:windows|linux|macos"]

# 输出格式（必须）
最后用以下 Markdown 表格输出报告：

| Issue | Classification | Labels | Confidence | Reasoning |
|-------|---------------|--------|-----------|-----------|
| #1 | bug | bug, priority:high | 0.95 | panic in main loop |
| ... | | | | |

不要输出其他内容。`

func main() {
	fmt.Println("=== AgentPrimordia: GitHub Issue Triage Bot ===")
	fmt.Println()

	// 1. 决定 GitHub API 模式
	//    - 真实模式：同时设置了 GITHUB_TOKEN 与 LLM API Key（需 LLM 驱动 ReAct 循环）
	//    - mock 模式：其余情况（无 GITHUB_TOKEN，或缺少 LLM Key 时安全回退，不触碰真实仓库）
	token := os.Getenv("GITHUB_TOKEN")
	hasLLMKey := os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("QWEN_API_KEY") != "" || os.Getenv("DEEPSEEK_API_KEY") != ""
	useRealGitHub := token != "" && hasLLMKey

	var server *mockGitHubServer
	if useRealGitHub {
		githubToken = token
		if repo := os.Getenv("GH_REPO"); repo != "" {
			githubRepo = repo
		}
		fmt.Printf("[GitHub API] 真实 API 模式：%s\n", githubRepo)
		fmt.Printf("[提示]       请确保该 token 对 %s 有 issues 读取与 label 写入权限\n", githubRepo)
	} else {
		server = newMockGitHubServer()
		apiBaseURL, shutdown := server.start()
		defer shutdown()
		apiBase = apiBaseURL // 注入给 tools 包
		fmt.Printf("[Mock Server] GitHub API mock 启动于 %s\n", apiBaseURL)
		fmt.Printf("[Seed]       %d 个预置 issue 等待分类\n\n", len(server.snapshot()))
		if token != "" {
			fmt.Println("[提示] 已设置 GITHUB_TOKEN 但缺少 LLM API Key：真实 GitHub API 需要 LLM 驱动，已安全回退 mock 模式（不会改动真实仓库）")
		}
	}
	fmt.Println()

	// 2. 创建 LLM Provider（按环境变量自动选择）
	_, providerName, isMock, err := createProvider()
	if err != nil {
		log.Fatalf("创建 Provider 失败: %v", err)
	}
	fmt.Printf("[Provider]   使用 %s\n\n", providerName)

	// 3. 注册工具
	registry, err := registryFromTools(
		listIssuesTool{},
		readIssueTool{},
		addLabelTool{},
	)
	if err != nil {
		log.Fatalf("注册工具失败: %v", err)
	}

	// 4. 运行 triage 流程
	startTime := time.Now()
	var report string
	var turns, toolsUsed int

	if isMock {
		// Mock 模式: 绕过 Agent 循环，直接演示工具调用 + 报告
		// 原因: testutil.MockProvider 不会触发工具调用，无法走完 ReAct 循环
		// 真实 LLM 模式下, Agent 会自动通过 ReAct 循环调用工具
		fmt.Println("[Mock 模式] 直接演示工具调用流程（跳过 Agent 循环）...")
		runMockDemoFlow(registry)
		report = mockFinalReport()
		turns = 0
		toolsUsed = 11 // 1 list + 5 read + 5 add_label
	} else {
		// 真实 LLM 模式: 走完整 ReAct 循环
		provider, _, _, _ := createProvider()
		agent, err := ap.NewAgent("issue-triage-bot", systemPrompt,
			provider,
			ap.WithMaxTurns(20),
			ap.WithTemperature(0),
		)
		if err != nil {
			log.Fatalf("创建 Agent 失败: %v", err)
		}
		agent = agent.WithToolkit(registry)

		fmt.Println("[Agent] 开始 Triage...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		resp, err := agent.Run(ctx, ap.UserMessage(
			"请处理所有 open issues，并对每个 issue 完成分类与加 label。",
		))
		if err != nil {
			log.Fatalf("Agent 运行失败: %v", err)
		}
		report = resp.Content
		turns = resp.Metrics.TotalTurns
		toolsUsed = resp.Metrics.TotalTools
	}

	elapsed := time.Since(startTime)
	fmt.Println()
	fmt.Println("=== Triage 报告 ===")
	fmt.Println()
	fmt.Println(report)
	fmt.Println()

	// 5. 显示最终状态（mock 模式可读取服务端快照；真实 API 模式无法回读）
	fmt.Println("=== 最终 Issue 状态 ===")
	fmt.Println()
	if server != nil {
		for _, iss := range server.snapshot() {
			fmt.Println(formatIssueBrief(iss))
		}
	} else {
		fmt.Println("（真实 GitHub API 模式：无法读取服务端快照，最终状态见上方 Agent 输出的报告）")
	}
	fmt.Println()

	// 6. 统计
	if server != nil {
		totalIssues := len(server.snapshot())
		labeledCount := 0
		for _, iss := range server.snapshot() {
			if len(iss.Labels) > 0 {
				labeledCount++
			}
		}
		fmt.Println("=== 统计 ===")
		fmt.Printf("总 Issue 数:     %d\n", totalIssues)
		fmt.Printf("已分类 Issue 数: %d\n", labeledCount)
		fmt.Printf("耗时:            %v\n", elapsed.Round(100*time.Millisecond))
		fmt.Printf("LLM 轮数:        %d\n", turns)
		fmt.Printf("工具调用次数:    %d\n", toolsUsed)
	} else {
		fmt.Println("=== 统计 ===")
		fmt.Printf("耗时:            %v\n", elapsed.Round(100*time.Millisecond))
		fmt.Printf("LLM 轮数:        %d\n", turns)
		fmt.Printf("工具调用次数:    %d\n", toolsUsed)
	}
}

// createProvider 按环境变量自动选择 LLM Provider
// 优先级: OpenAI > Qwen > DeepSeek > MockLLM
// 返回值最后一项 isMock 标识是否使用了 mock provider
func createProvider() (ap.Provider, string, bool, error) {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		p, err := ap.NewOpenAIProvider(ap.Config{APIKey: key, Model: "gpt-4o-mini"})
		if err != nil {
			return nil, "", false, err
		}
		return p, "OpenAI (gpt-4o-mini)", false, nil
	}

	if key := os.Getenv("QWEN_API_KEY"); key != "" {
		p, err := ap.NewQwenProvider(ap.Config{APIKey: key, Model: "qwen-plus"})
		if err != nil {
			return nil, "", false, err
		}
		return p, "Qwen (qwen-plus)", false, nil
	}

	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		p, err := ap.NewOpenAIProvider(ap.Config{
			APIKey:  key,
			BaseURL: "https://api.deepseek.com/v1",
			Model:   "deepseek-chat",
		})
		if err != nil {
			return nil, "", false, err
		}
		return p, "DeepSeek (deepseek-chat)", false, nil
	}

	// 无 API Key，使用 mock provider（确定性响应）
	mock := testutil.NewMockProvider(buildMockResponses()...)
	return mock, "MockLLM (无 API Key 模式)", true, nil
}

// buildMockResponses 构造 mock provider 的预置响应序列
// 模拟一个完整的 triage 流程，让 demo 在无 API Key 时也能"成功"演示
func buildMockResponses() []string {
	return []string{
		`我先列出所有 open issues 来开始 triage。`,
		`让我读取 Issue #1 的详情。`,
		`Issue #1 是 panic in main loop，是 bug，给它添加 ["bug", "priority:high"] label。`,
		`读取 Issue #2。`,
		`Issue #2 是 dark mode 功能请求，分类为 feature，加 ["enhancement"] label。`,
		`读取 Issue #3。`,
		`Issue #3 是 OAuth 配置咨询，分类为 question，加 ["question"] label。`,
		`读取 Issue #4。`,
		`Issue #4 是 Windows 平台 CGO 编译错误，分类为 bug，加 ["bug", "platform:windows"] label。`,
		`读取 Issue #5。`,
		`Issue #5 明确说 "Same as #2"，是 duplicate，加 ["duplicate"] label。`,
		mockFinalReport(),
	}
}

func mockFinalReport() string {
	var b strings.Builder
	b.WriteString("| Issue | Classification | Labels | Confidence | Reasoning |\n")
	b.WriteString("|-------|---------------|--------|-----------|-----------|\n")
	b.WriteString("| #1 | bug | bug, priority:high | 0.95 | panic in main loop with nil context |\n")
	b.WriteString("| #2 | feature | enhancement | 0.92 | user request for new dark mode feature |\n")
	b.WriteString("| #3 | question | question | 0.98 | user asking for OAuth configuration guidance |\n")
	b.WriteString("| #4 | bug | bug, platform:windows | 0.90 | Windows CGO build error during compilation |\n")
	b.WriteString("| #5 | duplicate | duplicate | 0.85 | explicitly references issue #2 as duplicate |\n")
	return b.String()
}

// runMockDemoFlow 在 Mock 模式下手动驱动工具调用流程
// 模拟 ReAct 循环中 Agent 会做的事情：list → read → add_label 循环
func runMockDemoFlow(registry *ap.ToolRegistry) {
	ctx := context.Background()

	// 1. list_issues
	listTool, _ := registry.Get("list_issues")
	result, err := listTool.Execute(ctx, []byte(`{"state":"open"}`))
	if err != nil || result == nil {
		log.Printf("[tool] list_issues failed: %v", err)
		return
	}

	var issues []Issue
	_ = json.Unmarshal([]byte(result.Content), &issues)
	log.Printf("[tool] list_issues returned %d issues", len(issues))

	// 2-6. 对每个 issue 分类 + 加 label
	classifications := []struct {
		number int
		labels []string
		reason string
	}{
		{1, []string{"bug", "priority:high"}, "panic in main loop"},
		{2, []string{"enhancement"}, "dark mode feature request"},
		{3, []string{"question"}, "OAuth configuration inquiry"},
		{4, []string{"bug", "platform:windows"}, "Windows CGO build error"},
		{5, []string{"duplicate"}, "explicit reference to issue #2"},
	}

	readTool, _ := registry.Get("read_issue")
	labelTool, _ := registry.Get("add_label")

	for _, c := range classifications {
		// read
		readArgs := []byte(fmt.Sprintf(`{"issue_number":%d}`, c.number))
		if _, err := readTool.Execute(ctx, readArgs); err != nil {
			log.Printf("[tool] read_issue(%d) failed: %v", c.number, err)
		}
		// add_label
		labelsJSON, _ := json.Marshal(c.labels)
		labelArgs := []byte(fmt.Sprintf(`{"issue_number":%d,"labels":%s}`, c.number, labelsJSON))
		if _, err := labelTool.Execute(ctx, labelArgs); err != nil {
			log.Printf("[tool] add_label(%d) failed: %v", c.number, err)
		}
	}
}
