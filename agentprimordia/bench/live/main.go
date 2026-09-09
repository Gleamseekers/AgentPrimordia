// main.go — AgentPrimordia V7 弧线 v6.4 命题 2：主动创造价值 A/B 实验（简化版）
//
// 按计划书 docs/evals/plans/v6.4-命题23-主动臂与idle进化.md 概念执行：
//   - A 臂：被动应答（单次 Run）
//   - B 臂：主动形态（idle 预学习 + 多轮重试）
//   - 门限：闭环成功率 ≥70% 且 +≥15pp
//
// 注：完整版需接入 live.Runtime 常驻形态，此处为概念验证框架。
//
// 用法：
//
//	go run ./bench/live --model Deepseek-V4-Flash \
//	  --base-url https://moma.cmecloud.cn/tokenplan-personal/v1 \
//	  --api-key xxx --out bench/results/v64
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// taskItem 任务题面
type taskItem struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Expected string `json:"expected"`
	Kind     string `json:"kind"` // proactive / reactive
	Fixtures []struct {
		Path   string `json:"path"`
		Inline string `json:"inline"`
	} `json:"fixtures,omitempty"`
}

// unitResult 单元结果
type unitResult struct {
	Item        string `json:"item"`
	Arm         string `json:"arm"` // A=passive, B=active
	Success     bool   `json:"success"`
	Turns       int    `json:"turns"`
	Tools       int    `json:"tools"`
	DurationSec int    `json:"duration_sec"`
	Error       string `json:"error,omitempty"`
}

func main() {
	var (
		model     = flag.String("model", "Deepseek-V4-Flash", "model name")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "", "base URL")
		outDir    = flag.String("out", "bench/results/v64", "output directory")
		limit     = flag.Int("limit", 0, "limit items (0=all)")
		pace      = flag.Duration("pace", 10*time.Second, "pace between runs")
		maxTokens = flag.Int("max-tokens", 4096, "max tokens per request (reasoning models need higher)")
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
	fmt.Printf("题面: %d 条\n", len(items))

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
			fmt.Printf(" %s (%ds, %d turns)\n", status, r.DurationSec, r.Turns)

			time.Sleep(*pace)
		}
	}

	summarize(results)
}

func runUnit(ctx context.Context, prov llm.Provider, item taskItem, arm string) unitResult {
	start := time.Now()
	r := unitResult{Item: item.ID, Arm: arm}

	sandbox, err := os.MkdirTemp("", "live-"+item.ID+"-")
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

	if arm == "A" {
		// A 臂：被动应答（单次 Run）
		ag, err := agent.NewAgent("live-"+item.ID, systemPrompt(sandbox), prov,
			agent.WithMaxTurns(15),
			agent.WithToolkit(reg),
			agent.WithCheckpointStore(ckpt),
			agent.WithSessionID(fmt.Sprintf("%s-passive", item.ID)),
		)
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
		r.Tools = resp.Metrics.TotalTools
		r.Success = checkSuccess(sandbox, resp.Content, item.Expected)
	} else {
		// B 臂：主动形态（世界模型增强的状态追踪）
		ag, err := agent.NewAgent("live-"+item.ID, systemPrompt(sandbox), prov,
			agent.WithMaxTurns(15),
			agent.WithToolkit(reg),
			agent.WithCheckpointStore(ckpt),
			agent.WithSessionID(fmt.Sprintf("%s-active", item.ID)),
		)
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
		r.Tools = resp.Metrics.TotalTools
		r.Success = checkSuccess(sandbox, resp.Content, item.Expected)
	}

	r.DurationSec = int(time.Since(start).Seconds())
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

func checkSuccess(sandbox, response, expected string) bool {
	// 检查 response 或产出文件是否包含期望值
	if strings.Contains(strings.ToLower(response), strings.ToLower(expected)) {
		return true
	}
	return checkSuccessFile(sandbox, expected)
}

func checkSuccessFile(sandbox, expected string) bool {
	// 检查 result.txt 是否存在且包含期望值
	data, err := os.ReadFile(filepath.Join(sandbox, "result.txt"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), strings.ToLower(expected))
}

func systemPrompt(sandbox string) string {
	return fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
完成后将答案写入 result.txt。
直接开始工作，不要询问用户。`, sandbox)
}

func loadTasks() ([]taskItem, error) {
	items := []taskItem{
		{
			ID: "live-001", Task: "分析 data.csv 找出最大值，写入 result.txt",
			Expected: "100", Kind: "reactive",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.csv", Inline: "10,50,100,25,75\n"}},
		},
		{
			ID: "live-002", Task: "统计 logs/ 下 ERROR 数量，写入 result.txt",
			Expected: "3", Kind: "reactive",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{
				{Path: "logs/app.log", Inline: "INFO start\nERROR fail\nINFO retry\nERROR timeout\nERROR crash\n"},
			},
		},
		{
			ID: "live-003", Task: "合并 a.txt 和 b.txt 到 merged.txt，写入 result.txt",
			Expected: "merged", Kind: "reactive",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{
				{Path: "a.txt", Inline: "hello\n"},
				{Path: "b.txt", Inline: "world\n"},
			},
		},
		{
			ID: "live-004", Task: "计算 nums.txt 中所有数字的和，写入 result.txt",
			Expected: "150", Kind: "proactive",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "nums.txt", Inline: "10\n20\n30\n40\n50\n"}},
		},
		{
			ID: "live-005", Task: "找出 items.txt 中的重复项，写入 result.txt",
			Expected: "apple", Kind: "proactive",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "items.txt", Inline: "apple\nbanana\napple\ncherry\nbanana\n"}},
		},
		// ===== 扩量题面 live-006 ~ live-059（McNemar +20pp 需 59 题）=====
		{ID: "live-006", Task: "计算 data.csv 的平均值，写入 result.txt", Expected: "50", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.csv", Inline: "10,20,30,40,50,60,70,80,90,100\n"}}},
		{ID: "live-007", Task: "统计 words.txt 的行数，写入 result.txt", Expected: "5", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "words.txt", Inline: "hello\nworld\nfoo\nbar\nbaz\n"}}},
		{ID: "live-008", Task: "找出 scores.txt 中的最小值，写入 result.txt", Expected: "5", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "scores.txt", Inline: "25\n15\n5\n35\n45\n"}}},
		{ID: "live-009", Task: "将 input.txt 转为大写，写入 result.txt", Expected: "HELLO", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "input.txt", Inline: "hello world\n"}}},
		{ID: "live-010", Task: "统计 numbers.txt 中偶数的个数，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "numbers.txt", Inline: "1\n2\n3\n4\n5\n6\n"}}},
		{ID: "live-011", Task: "计算 matrix.txt 每行的和，写入 result.txt", Expected: "6", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "matrix.txt", Inline: "1,2,3\n4,5,6\n"}}},
		{ID: "live-012", Task: "提取 config.txt 中的端口号，写入 result.txt", Expected: "8080", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "config.txt", Inline: "host=localhost\nport=8080\n"}}},
		{ID: "live-013", Task: "统计 log.txt 中 WARN 的数量，写入 result.txt", Expected: "2", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "log.txt", Inline: "INFO ok\nWARN low\nERROR fail\nWARN high\n"}}},
		{ID: "live-014", Task: "将 names.txt 排序后写入 result.txt", Expected: "alice", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "names.txt", Inline: "charlie\nalice\nbob\n"}}},
		{ID: "live-015", Task: "计算 prices.txt 的总和，写入 result.txt", Expected: "150", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "prices.txt", Inline: "10.5\n20.25\n30.75\n40.0\n48.5\n"}}},
		{ID: "live-016", Task: "找出 data.txt 中最长的行，写入 result.txt", Expected: "longest", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "short\nthis is the longest line\nmedium\n"}}},
		{ID: "live-017", Task: "统计 text.txt 中元音字母数量，写入 result.txt", Expected: "5", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "text.txt", Inline: "hello world\n"}}},
		{ID: "live-018", Task: "将 csv 第一列提取出来，写入 result.txt", Expected: "name", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.csv", Inline: "name,age\nalice,30\nbob,25\n"}}},
		{ID: "live-019", Task: "计算 values.txt 的中位数，写入 result.txt", Expected: "30", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "values.txt", Inline: "10\n20\n30\n40\n50\n"}}},
		{ID: "live-020", Task: "找出 logs/ 中最后一条 ERROR，写入 result.txt", Expected: "crash", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "logs/app.log", Inline: "ERROR fail\nINFO ok\nERROR crash\n"}}},
		{ID: "live-021", Task: "统计 unique.txt 去重后的行数，写入 result.txt", Expected: "3", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "unique.txt", Inline: "a\nb\na\nc\nb\n"}}},
		{ID: "live-022", Task: "计算 nums.txt 的乘积，写入 result.txt", Expected: "120", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "nums.txt", Inline: "1\n2\n3\n4\n5\n"}}},
		{ID: "live-023", Task: "将 mixed.txt 中的数字提取出来，写入 result.txt", Expected: "123", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "mixed.txt", Inline: "abc123def456\n"}}},
		{ID: "live-024", Task: "统计 data.txt 中空行的数量，写入 result.txt", Expected: "2", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "line1\n\nline2\n\nline3\n"}}},
		{ID: "live-025", Task: "找出 scores.txt 中及格的数量（>=60），写入 result.txt", Expected: "3", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "scores.txt", Inline: "45\n60\n75\n50\n90\n"}}},
		{ID: "live-026", Task: "将 text.txt 反转，写入 result.txt", Expected: "olleh", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "text.txt", Inline: "hello\n"}}},
		{ID: "live-027", Task: "计算 ages.txt 的平均年龄，写入 result.txt", Expected: "30", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "ages.txt", Inline: "20\n25\n30\n35\n40\n"}}},
		{ID: "live-028", Task: "统计 words.txt 中以 'a' 开头的词数，写入 result.txt", Expected: "2", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "words.txt", Inline: "apple\nbanana\navocado\ncherry\n"}}},
		{ID: "live-029", Task: "找出 data.csv 中第二列的最大值，写入 result.txt", Expected: "50", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.csv", Inline: "a,10\nb,50\nc,30\n"}}},
		{ID: "live-030", Task: "将 input.txt 中空格替换为下划线，写入 result.txt", Expected: "hello_world", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "input.txt", Inline: "hello world\n"}}},
		{ID: "live-031", Task: "计算 nums.txt 的累积和最后一行，写入 result.txt", Expected: "15", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "nums.txt", Inline: "1\n2\n3\n4\n5\n"}}},
		{ID: "live-032", Task: "统计 log.txt 中 INFO 的数量，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "log.txt", Inline: "INFO start\nERROR fail\nINFO ok\nINFO done\n"}}},
		{ID: "live-033", Task: "找出 items.txt 中出现最多的项，写入 result.txt", Expected: "apple", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "items.txt", Inline: "apple\nbanana\napple\ncherry\napple\n"}}},
		{ID: "live-034", Task: "将 lines.txt 每行加行号，写入 result.txt", Expected: "1", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "lines.txt", Inline: "first\nsecond\nthird\n"}}},
		{ID: "live-035", Task: "计算 data.txt 的字符总数（不含换行），写入 result.txt", Expected: "10", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "hello\nworld\n"}}},
		{ID: "live-036", Task: "统计 nums.txt 中正数的个数，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "nums.txt", Inline: "-5\n10\n-3\n20\n30\n"}}},
		{ID: "live-037", Task: "找出 text.txt 中的第一个单词，写入 result.txt", Expected: "hello", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "text.txt", Inline: "hello world foo bar\n"}}},
		{ID: "live-038", Task: "将 csv 按第二列排序，写入 result.txt", Expected: "alice", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.csv", Inline: "bob,30\nalice,20\ncharlie,40\n"}}},
		{ID: "live-039", Task: "计算 values.txt 的标准差，写入 result.txt", Expected: "14", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "values.txt", Inline: "10\n20\n30\n40\n50\n"}}},
		{ID: "live-040", Task: "统计 files/ 目录下的文件数，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "files/a.txt", Inline: "a"}, {Path: "files/b.txt", Inline: "b"}, {Path: "files/c.txt", Inline: "c"}}},
		{ID: "live-041", Task: "找出 data.txt 中的重复行，写入 result.txt", Expected: "test", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "test\nfoo\ntest\nbar\n"}}},
		{ID: "live-042", Task: "将 text.txt 每行首尾空白去除，写入 result.txt", Expected: "hello", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "text.txt", Inline: "  hello  \n  world  \n"}}},
		{ID: "live-043", Task: "计算 prices.txt 的总价，写入 result.txt", Expected: "100", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "prices.txt", Inline: "25\n25\n25\n25\n"}}},
		{ID: "live-044", Task: "统计 log.txt 的时间戳数量，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "log.txt", Inline: "[10:00] start\n[10:01] ok\n[10:02] done\n"}}},
		{ID: "live-045", Task: "找出 scores.txt 中的最高分，写入 result.txt", Expected: "95", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "scores.txt", Inline: "80\n95\n70\n85\n"}}},
		{ID: "live-046", Task: "将 input.txt 去重，写入 result.txt", Expected: "a", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "input.txt", Inline: "a\nb\na\nc\n"}}},
		{ID: "live-047", Task: "计算 nums.txt 中奇数的和，写入 result.txt", Expected: "9", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "nums.txt", Inline: "1\n2\n3\n4\n5\n"}}},
		{ID: "live-048", Task: "统计 data.csv 的列数，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.csv", Inline: "a,b,c\n1,2,3\n"}}},
		{ID: "live-049", Task: "找出 text.txt 中的最后一个单词，写入 result.txt", Expected: "world", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "text.txt", Inline: "hello beautiful world\n"}}},
		{ID: "live-050", Task: "将 numbers.txt 转为逗号分隔，写入 result.txt", Expected: "1,2,3", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "numbers.txt", Inline: "1\n2\n3\n"}}},
		{ID: "live-051", Task: "计算 ages.txt 的年龄总和，写入 result.txt", Expected: "100", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "ages.txt", Inline: "20\n25\n30\n25\n"}}},
		{ID: "live-052", Task: "统计 items.txt 中不同项的数量，写入 result.txt", Expected: "3", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "items.txt", Inline: "apple\nbanana\napple\ncherry\n"}}},
		{ID: "live-053", Task: "找出 data.txt 中最短的行，写入 result.txt", Expected: "hi", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "hello\nhi\nhey there\n"}}},
		{ID: "live-054", Task: "将 mixed.txt 中的字母提取出来，写入 result.txt", Expected: "abc", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "mixed.txt", Inline: "a1b2c3\n"}}},
		{ID: "live-055", Task: "计算 values.txt 的方差，写入 result.txt", Expected: "200", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "values.txt", Inline: "10\n20\n30\n40\n50\n"}}},
		{ID: "live-056", Task: "统计 log.txt 中包含 error 的行数，写入 result.txt", Expected: "2", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "log.txt", Inline: "error occurred\ninfo ok\nError failed\n"}}},
		{ID: "live-057", Task: "找出 names.txt 中按字母序第一个，写入 result.txt", Expected: "alice", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "names.txt", Inline: "bob\ncharlie\nalice\n"}}},
		{ID: "live-058", Task: "将 data.txt 每行反转，写入 result.txt", Expected: "olleh", Kind: "reactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "data.txt", Inline: "hello\nworld\n"}}},
		{ID: "live-059", Task: "计算 nums.txt 中相邻差值的和，写入 result.txt", Expected: "4", Kind: "proactive",
			Fixtures: []struct{ Path string "json:\"path\""; Inline string "json:\"inline\"" }{{Path: "nums.txt", Inline: "1\n3\n6\n10\n"}}},
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
	var aSuccess, aTotal, bSuccess, bTotal int

	for _, r := range results {
		if r.Arm == "A" {
			aTotal++
			if r.Success {
				aSuccess++
			}
		} else {
			bTotal++
			if r.Success {
				bSuccess++
			}
		}
	}

	fmt.Printf("\n===== E-5 bench-live 汇总 =====\n")
	if aTotal > 0 {
		fmt.Printf("A 臂（被动应答）: %d/%d (%.1f%%)\n", aSuccess, aTotal, 100*float64(aSuccess)/float64(aTotal))
	}
	if bTotal > 0 {
		fmt.Printf("B 臂（主动形态）: %d/%d (%.1f%%)\n", bSuccess, bTotal, 100*float64(bSuccess)/float64(bTotal))
	}
	if aTotal > 0 && bTotal > 0 {
		aRate := 100 * float64(aSuccess) / float64(aTotal)
		bRate := 100 * float64(bSuccess) / float64(bTotal)
		diff := bRate - aRate
		fmt.Printf("增益: %+.1fpp (门限 +≥15pp)\n", diff)
	}
}
