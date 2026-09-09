// main.go — AgentPrimordia v7.1 命题 5：工具智能统一 A/B bench
//
// A 臂：基线 Agent（filesystem + shell，无工具智能）
// B 臂：Agent + IntelligenceHook（使用后画像 + 失败后缺口检测 + 自动工具创建）
//
// 验收门：McNemar p<0.05 且配对差值 ≥ +20pp；工具复用率 B 臂 ≥ 2× A 臂
//
// 用法：
//
//	go run ./bench/intelligence --model sensenova-6.8-flash-lite \
//	  --base-url https://token.sensenova.cn/v1 \
//	  --api-key xxx --out bench/results/intelligence
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/agent/hooks"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
	"agentprimordia/internal/tools/intelligence"
	"agentprimordia/internal/tools/intelligence/create"
	"agentprimordia/internal/tools/intelligence/optimize"
)

// taskItem 题面
type taskItem struct {
	ID          string `json:"id"`
	Task        string `json:"task"`
	Expected    string `json:"expected"`
	Kind        string `json:"kind"` // tool_create / tool_optimize / baseline
	Fixtures    []struct {
		Path   string `json:"path"`
		Inline string `json:"inline"`
	} `json:"fixtures"`
	SuccessAssert []struct {
		Kind     string `json:"kind"`
		Path     string `json:"path,omitempty"`
		Contains string `json:"contains,omitempty"`
	} `json:"success_assert"`
}

// unitResult 单元结果
type unitResult struct {
	Item        string   `json:"item"`
	Arm         string   `json:"arm"`
	Success     bool     `json:"success"`
	Turns       int      `json:"turns"`
	GapsFound   int      `json:"gaps_found"`
	ToolsUsed   int      `json:"tools_used"`
	ToolNames   []string `json:"tool_names,omitempty"`
	DurationSec int      `json:"duration_sec"`
	Error       string   `json:"error,omitempty"`
}

// registeringCreator 包装基础生成器，将创建的工具注册到 agent 的工具注册表
type registeringCreator struct {
	base *create.LifecycleCreator
	reg  *tools.Registry
	dir  string
}

func (c *registeringCreator) Create(ctx context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	art, err := c.base.Create(ctx, gap)
	if err != nil || art == nil {
		return art, err
	}
	// 将工件写入沙箱并注册为可执行工具
	scriptPath := filepath.Join(c.dir, ".intel-tools", art.Name)
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return nil, fmt.Errorf("创建工具目录失败: %w", err)
	}
	if err := os.WriteFile(scriptPath, art.Artifact, 0755); err != nil {
		return nil, fmt.Errorf("写入工具脚本失败: %w", err)
	}

	t := &artifactTool{
		name:    art.Name,
		desc:    art.Description,
		path:    scriptPath,
		workdir: c.dir,
	}
	_ = c.reg.Register(t)
	return art, nil
}

// artifactTool 将 ToolArtifact 适配为 tools.Tool 接口
type artifactTool struct {
	name    string
	desc    string
	path    string
	workdir string
}

func (t *artifactTool) Name() string        { return t.name }
func (t *artifactTool) Description() string  { return t.desc }
func (t *artifactTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"args":{"type":"string","description":"传递给工具的参数"}},"required":["args"]}`)
}

func (t *artifactTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult("参数解析失败: " + err.Error()), nil
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", t.path)
	cmd.Args = append(cmd.Args, strings.Fields(params.Args)...)
	cmd.Dir = t.workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tools.NewResult(fmt.Sprintf("执行出错: %v\n输出: %s", err, string(out))), nil
	}
	return tools.NewResult(strings.TrimSpace(string(out))), nil
}

func main() {
	var (
		model     = flag.String("model", "sensenova-6.8-flash-lite", "model name")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "", "base URL")
		outDir    = flag.String("out", "bench/results/intelligence", "output directory")
		limit     = flag.Int("limit", 0, "limit items (0=all)")
		pace      = flag.Duration("pace", 10*time.Second, "pace between runs")
		maxTokens = flag.Int("max-tokens", 4096, "max tokens per request")
	)
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if *apiKey == "" {
		fmt.Println("错误: 需要 --api-key 或 OPENAI_API_KEY")
		os.Exit(1)
	}

	items, err := loadTasks()
	if err != nil {
		fmt.Printf("加载题面失败: %v\n", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(items) {
		items = items[:*limit]
	}
	fmt.Printf("工具智能题面: %d 条\n", len(items))

	prov, err := llm.NewOpenAIProvider(llm.Config{
		APIKey:    *apiKey,
		Model:     *model,
		BaseURL:   *baseURL,
		MaxTokens: *maxTokens,
	})
	if err != nil {
		fmt.Printf("创建 Provider 失败: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Printf("创建输出目录失败: %v\n", err)
		os.Exit(1)
	}

	resultsFile := filepath.Join(*outDir, "results.jsonl")
	results, err := loadResults(resultsFile)
	if err != nil {
		fmt.Printf("加载已有结果失败: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	for i, item := range items {
		for _, arm := range []string{"A", "B"} {
			key := fmt.Sprintf("%s/%s", item.ID, arm)
			if _, done := results[key]; done {
				continue
			}

			fmt.Printf("[%d/%d] %s arm=%s ...", i+1, len(items), item.ID, arm)
			r := runUnit(ctx, prov, item, arm)
			results[key] = r

			f, _ := os.OpenFile(resultsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err := json.NewEncoder(f).Encode(r); err != nil {
				fmt.Printf("写入结果失败: %v\n", err)
			}
			f.Close()

			status := "OK"
			if !r.Success {
				status = "FAIL"
			}
			fmt.Printf(" %s (%ds, %d turns, gaps=%d, tools=%d)\n", status, r.DurationSec, r.Turns, r.GapsFound, r.ToolsUsed)

			time.Sleep(*pace)
		}
	}

	summarize(results)
}

// runUnit 运行单臂（真实 Agent 执行）
func runUnit(ctx context.Context, prov llm.Provider, item taskItem, arm string) unitResult {
	start := time.Now()
	r := unitResult{Item: item.ID, Arm: arm}

	sandbox, err := os.MkdirTemp("", "intel-"+item.ID+"-")
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer os.RemoveAll(sandbox)

	// 注入 fixtures
	for _, fx := range item.Fixtures {
		p := filepath.Join(sandbox, filepath.FromSlash(fx.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			r.Error = err.Error()
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
		if err := os.WriteFile(p, []byte(fx.Inline), 0644); err != nil {
			r.Error = err.Error()
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
	}

	ckpt, err := persist.NewSQLiteCheckpointStore(filepath.Join(sandbox, "checkpoint.db"))
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer ckpt.Close()

	reg := sandboxToolkit(sandbox)
	prompt := systemPrompt(sandbox)

	var hm *hooks.HookManager
	if arm == "B" {
		hm = hooks.NewHookManager()
		profiler := optimize.NewInMemoryProfiler()
		detector := create.NewTraceGapDetector()
		creator := &registeringCreator{base: create.NewLifecycleCreator(), reg: reg, dir: sandbox}
		iHook := intelligence.NewIntelligenceHook(profiler, detector, creator)

		// 桥接 IntelligenceHook → HookManager
		hm.Register(hooks.HookAfterTool, func(hctx context.Context, hcx *hooks.HookContext) error {
			toolName := ""
			args := ""
			if hcx.ToolCall != nil {
				toolName = hcx.ToolCall.Name
				args = hcx.ToolCall.Args
			}
			result := ""
			var toolErr error
			if hcx.ToolResult != nil {
				result = hcx.ToolResult.Content
				if hcx.ToolResult.IsError {
					toolErr = fmt.Errorf("tool error: %s", result)
				}
			}
			iHook.AfterToolCall(hctx, toolName, args, result, toolErr, hcx.Duration)
			return nil
		})
		hm.Register(hooks.HookAfterTurn, func(hctx context.Context, _ *hooks.HookContext) error {
			iHook.OnTurnEnd(hctx)
			return nil
		})
	}

	opts := []agent.Option{
		agent.WithMaxTurns(20),
		agent.WithToolkit(reg),
		agent.WithCheckpointStore(ckpt),
		agent.WithSessionID(fmt.Sprintf("%s-%s", item.ID, arm)),
	}
	if hm != nil {
		opts = append(opts, agent.WithHooks(hm))
	}

	ag, err := agent.NewAgent("intel-"+item.ID, prompt, prov, opts...)
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	resp, err := ag.Run(ctx, agent.UserMessage(item.Task))
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	r.Turns = resp.Metrics.TotalTurns
	r.DurationSec = int(time.Since(start).Seconds())
	r.Success = checkAssertions(sandbox, item.SuccessAssert, resp.Content)

	return r
}

func sandboxToolkit(dir string) *tools.Registry {
	reg := tools.NewRegistry()
	fsTool, err := builtin.NewFileSystem(dir)
	if err == nil {
		_ = reg.Register(fsTool)
	}
	shell := builtin.NewShell().WithAllowedWorkdirs([]string{dir})
	_ = reg.Register(shell)
	return reg
}

func systemPrompt(sandbox string) string {
	return fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
你可以使用 shell 工具执行任意命令来完成数据处理、文件转换等任务。
直接开始工作，不要询问用户。`, sandbox)
}

func checkAssertions(sandbox string, asserts []struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	Contains string `json:"contains,omitempty"`
}, response string) bool {
	if len(asserts) == 0 {
		return strings.Contains(strings.ToLower(response), "done")
	}
	for _, a := range asserts {
		switch a.Kind {
		case "file_contains":
			data, err := os.ReadFile(filepath.Join(sandbox, a.Path))
			if err != nil {
				return false
			}
			if !strings.Contains(string(data), a.Contains) {
				return false
			}
		case "response_contains":
			if !strings.Contains(strings.ToLower(response), strings.ToLower(a.Contains)) {
				return false
			}
		}
	}
	return true
}

func loadTasks() ([]taskItem, error) {
	data, err := os.ReadFile("bench/intelligence/tasks.json")
	if err != nil {
		return nil, fmt.Errorf("读取 tasks.json: %w", err)
	}
	var items []taskItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析 tasks.json: %w", err)
	}
	return items, nil
}

func loadResults(path string) (map[string]unitResult, error) {
	results := make(map[string]unitResult)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return results, nil
		}
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var r unitResult
		if err := dec.Decode(&r); err != nil {
			break
		}
		results[fmt.Sprintf("%s/%s", r.Item, r.Arm)] = r
	}
	return results, nil
}

func summarize(results map[string]unitResult) {
	paired := make(map[string][2]bool)
	toolUsage := make(map[string]int)

	for _, r := range results {
		pair := paired[r.Item]
		if r.Arm == "A" {
			pair[0] = r.Success
		} else {
			pair[1] = r.Success
		}
		paired[r.Item] = pair

		for _, t := range r.ToolNames {
			toolUsage[t]++
		}
	}

	var n00, n01, n10, n11 int
	for _, pair := range paired {
		switch {
		case !pair[0] && !pair[1]:
			n00++
		case !pair[0] && pair[1]:
			n01++
		case pair[0] && !pair[1]:
			n10++
		case pair[0] && pair[1]:
			n11++
		}
	}

	total := n00 + n01 + n10 + n11
	aSuccess := n10 + n11
	bSuccess := n01 + n11

	fmt.Printf("\n===== 工具智能 A/B 汇总 =====\n")
	fmt.Printf("总题数: %d\n", total)
	fmt.Printf("A 臂（基线）成功率: %d/%d (%.1f%%)\n", aSuccess, total, 100*float64(aSuccess)/float64(max(total, 1)))
	fmt.Printf("B 臂（统一智能）成功率: %d/%d (%.1f%%)\n", bSuccess, total, 100*float64(bSuccess)/float64(max(total, 1)))

	fmt.Printf("\n配对分析:\n")
	fmt.Printf("  双臂都成功: %d\n", n11)
	fmt.Printf("  双臂都失败: %d\n", n00)
	fmt.Printf("  仅 A 成功:  %d\n", n10)
	fmt.Printf("  仅 B 成功:  %d\n", n01)

	discordant := n01 + n10
	if discordant > 0 {
		chi2 := math.Pow(float64(n01-n10), 2) / float64(discordant)
		pValue := 1 - chiSquaredCDF(chi2, 1)
		fmt.Printf("\nMcNemar 卡方: %.3f (df=1, p=%.4f)\n", chi2, pValue)
		if pValue < 0.05 {
			fmt.Printf("结论: B 臂显著优于 A 臂 (p<0.05)\n")
		} else {
			fmt.Printf("结论: 双臂无显著差异 (p>=0.05)\n")
		}
	} else {
		fmt.Printf("\nMcNemar: 无不一致配对，无法计算\n")
	}

	reused := 0
	for _, count := range toolUsage {
		if count >= 2 {
			reused++
		}
	}
	totalTools := len(toolUsage)
	if totalTools > 0 {
		fmt.Printf("\n工具复用率: %d/%d (%.1f%%)\n", reused, totalTools, 100*float64(reused)/float64(totalTools))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func chiSquaredCDF(x float64, df int) float64 {
	if x <= 0 {
		return 0
	}
	z := math.Sqrt(x)
	return 2*normalCDF(z) - 1
}

func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}
