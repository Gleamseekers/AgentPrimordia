// main.go — AgentPrimordia v7.1 命题 4：规划增强 A/B bench
//
// A 臂：无规划器（纯 ReAct）
// B 臂：WithPlanner（LLM 任务分解 + 子任务调度）
//
// 分析：McNemar 检验（配对二分类），门限 B > A 显著（p < 0.05）
// 支持幂等续跑：跳过已完成的结果条目
//
// 用法：
//
//	go run ./bench/planning-v71 --model sensenova-6.8-flash-lite \
//	  --base-url https://token.sensenova.cn/v1 \
//	  --api-key xxx --out bench/results/planning-v71
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// longHorizonItem 长跨度任务题面
type longHorizonItem struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Expected string `json:"expected"`
	Fixtures []struct {
		Path   string `json:"path"`
		Inline string `json:"inline"`
	} `json:"fixtures,omitempty"`
	SuccessAssert []struct {
		Kind     string `json:"kind"`
		Path     string `json:"path,omitempty"`
		Contains string `json:"contains,omitempty"`
	} `json:"success_assert,omitempty"`
}

// unitResult 单题单臂结果
type unitResult struct {
	Item        string `json:"item"`
	Arm         string `json:"arm"` // A=no planner, B=with planner
	Success     bool   `json:"success"`
	Turns       int    `json:"turns"`
	DurationSec int    `json:"duration_sec"`
	Error       string `json:"error,omitempty"`
}

func main() {
	var (
		model     = flag.String("model", "sensenova-6.8-flash-lite", "model name")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "https://token.sensenova.cn/v1", "base URL")
		outDir    = flag.String("out", "bench/results/planning-v71", "output directory")
		limit     = flag.Int("limit", 0, "limit items (0=all)")
		pace      = flag.Duration("pace", 10*time.Second, "pace between runs")
		maxTokens = flag.Int("max-tokens", 4096, "max tokens per request")
		evalPath  = flag.String("eval", "docs/evals/long-horizon-v1.json", "eval dataset path")
	)
	flag.Parse()

	if *apiKey == "" {
		*apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if *apiKey == "" {
		fmt.Println("错误: 需要 --api-key 或 OPENAI_API_KEY")
		os.Exit(1)
	}

	// 加载题面
	items, err := loadItems(*evalPath)
	if err != nil {
		fmt.Printf("加载题面失败: %v\n", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(items) {
		items = items[:*limit]
	}
	fmt.Printf("长跨度题面: %d 条\n", len(items))

	// 创建 Provider
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

	// 幂等续跑：加载已有结果
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
				fmt.Printf("[%d/%d] %s arm=%s 已完成，跳过\n", i+1, len(items), item.ID, arm)
				continue
			}

			fmt.Printf("[%d/%d] %s arm=%s ...", i+1, len(items), item.ID, arm)
			r := runUnit(ctx, prov, item, arm, *maxTokens)
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
			fmt.Printf(" %s (%ds, %d turns)\n", status, r.DurationSec, r.Turns)

			time.Sleep(*pace)
		}
	}

	// McNemar 分析
	mcnemarAnalysis(results, items)
}

func runUnit(ctx context.Context, prov llm.Provider, item longHorizonItem, arm string, maxTokens int) unitResult {
	start := time.Now()
	r := unitResult{Item: item.ID, Arm: arm}

	sandbox, err := os.MkdirTemp("", "planning-"+item.ID+"-")
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer os.RemoveAll(sandbox)

	// fixtures 注入
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

	opts := []agent.Option{
		agent.WithMaxTurns(30),
		agent.WithToolkit(reg),
		agent.WithCheckpointStore(ckpt),
		agent.WithSessionID(fmt.Sprintf("%s-%s", item.ID, arm)),
	}

	// B 臂注入 LLM 规划器
	if arm == "B" {
		planner := planning.NewLLMPlanner(prov)
		opts = append(opts, agent.WithPlanner(planner))
	}

	ag, err := agent.NewAgent("planning-"+item.ID, prompt, prov, opts...)
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
使用提供的工具完成复杂任务。任务可能需要多个步骤。
所有产物必须写在该目录内。直接开始工作，不要询问用户。
如果需要创建多个文件或执行多个步骤，请逐一完成。`, sandbox)
}

func checkAssertions(sandbox string, asserts []struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	Contains string `json:"contains,omitempty"`
}, response string) bool {
	if len(asserts) == 0 {
		return strings.TrimSpace(response) != ""
	}
	for _, a := range asserts {
		switch a.Kind {
		case "file_exists":
			if _, err := os.Stat(filepath.Join(sandbox, a.Path)); err != nil {
				return false
			}
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

func loadItems(path string) ([]longHorizonItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取题面文件失败: %w", err)
	}
	var items []longHorizonItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("解析题面 JSON 失败: %w", err)
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

// mcnemarAnalysis McNemar 检验——配对二分类显著性分析
// 比较 A 臂和 B 臂在同一题面上的成败差异
func mcnemarAnalysis(results map[string]unitResult, items []longHorizonItem) {
	// 构建配对列联表
	//        B成功   B失败
	// A成功    a       b
	// A失败    c       d
	var a, b, c, d int

	paired := 0
	for _, item := range items {
		aKey := fmt.Sprintf("%s/A", item.ID)
		bKey := fmt.Sprintf("%s/B", item.ID)
		aRes, aOK := results[aKey]
		bRes, bOK := results[bKey]
		if !aOK || !bOK {
			continue
		}
		paired++

		switch {
		case aRes.Success && bRes.Success:
			a++
		case aRes.Success && !bRes.Success:
			b++
		case !aRes.Success && bRes.Success:
			c++
		case !aRes.Success && !bRes.Success:
			d++
		}
	}

	aSuccess := a + b
	bSuccess := a + c
	total := a + b + c + d

	fmt.Printf("\n===== 规划增强 A/B 汇总 =====\n")
	fmt.Printf("配对题数: %d\n", paired)
	fmt.Printf("A 臂（无规划）成功: %d/%d (%.1f%%)\n", aSuccess, total, 100*float64(aSuccess)/float64(max(total, 1)))
	fmt.Printf("B 臂（增强规划）成功: %d/%d (%.1f%%)\n", bSuccess, total, 100*float64(bSuccess)/float64(max(total, 1)))

	// 列联表
	fmt.Printf("\n配对列联表:\n")
	fmt.Printf("          B成功  B失败\n")
	fmt.Printf("A成功     %5d  %5d\n", a, b)
	fmt.Printf("A失败     %5d  %5d\n", c, d)

	// McNemar 检验
	// 卡方 = (b - c)^2 / (b + c)，自由度=1
	if b+c > 0 {
		chi2 := float64((b - c) * (b - c)) / float64(b+c)
		// 近似 p 值（自由度=1 的卡方分布）
		pValue := chi2PValue(chi2)
		fmt.Printf("\nMcNemar 检验:\n")
		fmt.Printf("  χ² = %.4f (b=%d, c=%d)\n", chi2, b, c)
		fmt.Printf("  p ≈ %.4f\n", pValue)
		if pValue < 0.05 {
			fmt.Printf("  ✓ B 臂显著优于 A 臂（p < 0.05）\n")
		} else {
			fmt.Printf("  ✗ 差异不显著（p >= 0.05）\n")
		}
	} else {
		fmt.Printf("\nMcNemar 检验: b+c=0，无法计算（两臂结果完全一致或全失败）\n")
	}

	gain := float64(bSuccess-aSuccess) / float64(max(total, 1)) * 100
	fmt.Printf("\n增益: %.1fpp (B - A)\n", gain)
}

// chi2PValue 卡方分布 p 值近似计算（自由度=1）
func chi2PValue(x float64) float64 {
	if x <= 0 {
		return 1.0
	}
	// 使用正态近似：Z = sqrt(chi2)，p = 2*(1 - Phi(Z))
	z := math.Sqrt(x)
	// Abramowitz & Stegun 近似
	p := 0.5 * math.Erfc(z/math.Sqrt2)
	return p
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
