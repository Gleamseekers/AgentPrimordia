// main.go — AgentPrimordia V7 弧线 v6.3 命题 1/2：缺口生成增益与工具复用率
//
// 按计划书 docs/evals/plans/v6.3-命题12-缺口生成与复用.md 执行：
//   - 命题 1：A=基线（filesystem+shell）vs B=基线+缺口工具，McNemar +≥20pp
//   - 命题 2：注册工具跨任务复用率 ≥60%（Wilson 下界）
//
// 用法：
//
//	go run ./bench/gap --model Deepseek-V4-Flash \
//	  --base-url https://moma.cmecloud.cn/tokenplan-personal/v1 \
//	  --api-key xxx --out bench/results/v63
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// gapItem 缺口题面
type gapItem struct {
	ID          string `json:"id"`
	Task        string `json:"task"`
	Expected    string `json:"expected"`
	GapKind     string `json:"gap_kind"`
	MissingTool string `json:"missing_tool,omitempty"`
	Fixtures    []struct {
		Path   string `json:"path"`
		Inline string `json:"inline"`
	} `json:"fixtures,omitempty"`
	SuccessAssert []struct {
		Kind string `json:"kind"`
		Path string `json:"path,omitempty"`
		Contains string `json:"contains,omitempty"`
	} `json:"success_assert,omitempty"`
}

// unitResult 单元结果
type unitResult struct {
	Item        string   `json:"item"`
	Arm         string   `json:"arm"`
	Success     bool     `json:"success"`
	Turns       int      `json:"turns"`
	Tools       int      `json:"tools"`
	ToolNames   []string `json:"tool_names,omitempty"`
	DurationSec int      `json:"duration_sec"`
	Error       string   `json:"error,omitempty"`
}

func main() {
	var (
		model     = flag.String("model", "Deepseek-V4-Flash", "model name")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "", "base URL")
		outDir    = flag.String("out", "bench/results/v63", "output directory")
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

	items, err := loadGapItems()
	if err != nil {
		fmt.Printf("加载缺口题面失败: %v\n", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(items) {
		items = items[:*limit]
	}
	fmt.Printf("缺口题面: %d 条\n", len(items))

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

func runUnit(ctx context.Context, prov llm.Provider, item gapItem, arm string) unitResult {
	start := time.Now()
	r := unitResult{Item: item.ID, Arm: arm}

	sandbox, err := os.MkdirTemp("", "gap-"+item.ID+"-")
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

	// 构造工具集
	reg := sandboxToolkit(sandbox)
	var prompt string
	if arm == "B" && item.MissingTool != "" {
		// B 臂：注入缺口工具（简化版：注册专用 shell 脚本模拟缺口工具）
		registerGapTools(reg, sandbox, item)
		prompt = systemPrompt(sandbox, item.MissingTool)
	} else {
		prompt = systemPrompt(sandbox)
	}

	ag, err := agent.NewAgent("gap-"+item.ID, prompt, prov,
		agent.WithMaxTurns(20),
		agent.WithToolkit(reg),
		agent.WithCheckpointStore(ckpt),
		agent.WithSessionID(fmt.Sprintf("%s-%s", item.ID, arm)),
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
	r.DurationSec = int(time.Since(start).Seconds())

	// 判定：检查 success_assert
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

func registerGapTools(reg *tools.Registry, sandbox string, item gapItem) {
	// 为 B 臂注册缺口工具（用 shell 脚本模拟专用工具）
	// 实际应走 AutoLoop 闭环生成 WASM 工件
	switch item.MissingTool {
	case "csv_stats":
		script := `#!/bin/sh
awk -F',' '{for(i=1;i<=NF;i++) sum[i]+=$i; n=NF} END {s=0; for(i=1;i<=n;i++) s+=sum[i]/NR; printf "%.1f\n", s/NF}' "$1" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "csv_stats", script)
	case "log_parser":
		script := `#!/bin/sh
grep -c "ERROR" "$1" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "log_parser", script)
	case "json_merge":
		script := `#!/bin/sh
cat "$1" "$2" | python3 -c "import json,sys; a=json.load(sys.stdin); b=json.load(sys.stdin); print(json.dumps(list(set(a+b))))" 2>/dev/null || echo "[]"`
		writeGapTool(reg, sandbox, "json_merge", script)
	case "xml_parser":
		script := `#!/bin/sh
grep -oP "<$2>\K[^<]+" "$1" 2>/dev/null || sed -n "s/.*<$2>\([^<]*\)<\/$2>.*/\1/p" "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "xml_parser", script)
	case "date_calc":
		script := `#!/bin/sh
date -d "$1 + $2 days" +%Y-%m-%d 2>/dev/null || date -v+"$2"d -j -f "%Y-%m-%d" "$1" +%Y-%m-%d 2>/dev/null || echo "error"`
		writeGapTool(reg, sandbox, "date_calc", script)
	case "hash_gen":
		script := `#!/bin/sh
shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1 || sha256sum "$1" 2>/dev/null | cut -d' ' -f1 || echo "error"`
		writeGapTool(reg, sandbox, "hash_gen", script)
	case "tsv_extract":
		script := `#!/bin/sh
awk -F'\t' "NR>1{print \$$2}" "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "tsv_extract", script)
	case "yaml_get":
		script := `#!/bin/sh
grep -A1 "^  $2:" "$1" 2>/dev/null | tail -1 | awk '{print $2}' || echo ""`
		writeGapTool(reg, sandbox, "yaml_get", script)
	case "stats_median":
		script := `#!/bin/sh
sort -n "$1" | awk 'NF{a[NR]=$1} END{if(NR%2) print a[(NR+1)/2]; else print (a[NR/2]+a[NR/2+1])/2}' 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "stats_median", script)
	case "md_headings":
		script := `#!/bin/sh
grep "^#" "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "md_headings", script)
	case "email_extract":
		script := `#!/bin/sh
grep -oE '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "email_extract", script)
	case "date_range":
		script := `#!/bin/sh
sort "$1" 2>/dev/null | awk 'NR==1{min=$1} {max=$1} END{print min, max}' || echo ""`
		writeGapTool(reg, sandbox, "date_range", script)
	case "csv_transpose":
		script := `#!/bin/sh
awk -F',' '{for(i=1;i<=NF;i++) a[i]=a[i](a[i]?",":"") $i} END{for(i=1;i in a;i++) print a[i]}' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "csv_transpose", script)
	case "json_filter":
		script := `#!/bin/sh
python3 -c "import json,sys; d=json.load(open('$1')); print('\n'.join(x['message'] for x in d if x.get('$2')=='$3'))" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "json_filter", script)
	case "word_freq":
		script := `#!/bin/sh
tr ' ' '\n' < "$1" | sort | uniq -c | sort -rn | head -1 | awk '{print $2}' 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "word_freq", script)
	case "hex_convert":
		script := `#!/bin/sh
while read h; do printf "%d\n" "0x$h" 2>/dev/null; done < "$1" || echo ""`
		writeGapTool(reg, sandbox, "hex_convert", script)
	case "template_fill":
		script := `#!/bin/sh
sed "s/{{$2}}/$3/g" "$1" 2>/dev/null || cat "$1"`
		writeGapTool(reg, sandbox, "template_fill", script)
	case "matrix_det":
		script := `#!/bin/sh
python3 -c "
import sys
m=[list(map(int,l.split(','))) for l in open('$1')]
print(int(m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1])-m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0])+m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])))
" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "matrix_det", script)
	case "url_domain":
		script := `#!/bin/sh
sed -E 's|https?://([^/]+).*|\1|' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "url_domain", script)
	case "base64_decode":
		script := `#!/bin/sh
base64 -d "$1" 2>/dev/null || base64 --decode "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "base64_decode", script)
	case "stats_stddev":
		script := `#!/bin/sh
awk '{a[NR]=$1; s+=$1} END{m=s/NR; v=0; for(i=1;i<=NR;i++) v+=(a[i]-m)^2; printf "%.0f\n", sqrt(v/NR)}' "$1" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "stats_stddev", script)
	case "ini_parse":
		script := `#!/bin/sh
awk "/^\[$2\]/{f=1;next} /^\[/{f=0} f&&/=/{print}" "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "ini_parse", script)
	case "color_complement":
		script := `#!/bin/sh
awk -F',' '{printf "%d,%d,%d\n", 255-$1, 255-$2, 255-$3}' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "color_complement", script)
	case "roman_convert":
		script := `#!/bin/sh
python3 -c "
r='$1'
d={'I':1,'V':5,'X':10,'L':50,'C':100,'D':500,'M':1000}
s=0
for i in range(len(r)):
    if i+1<len(r) and d[r[i]]<d[r[i+1]]: s-=d[r[i]]
    else: s+=d[r[i]]
print(s)
" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "roman_convert", script)
	case "csv_rowsum":
		script := `#!/bin/sh
awk -F',' '{s=0; for(i=1;i<=NF;i++) s+=$i; print $0","s}' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "csv_rowsum", script)
	case "text_censor":
		script := `#!/bin/sh
sed -E 's/(bad|terrible|awful)/***/g' "$1" 2>/dev/null || cat "$1"`
		writeGapTool(reg, sandbox, "text_censor", script)
	case "json_pluck":
		script := `#!/bin/sh
python3 -c "import json; [print(x['$2']) for x in json.load(open('$1'))]" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "json_pluck", script)
	case "cumsum":
		script := `#!/bin/sh
awk '{s+=$1; print s}' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "cumsum", script)
	case "md_table_csv":
		script := `#!/bin/sh
grep "^|" "$1" | grep -v "^|-" | sed 's/| */,/g; s/, *|/,/g; s/^,//; s/,$//' 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "md_table_csv", script)
	case "log_timestamps":
		script := `#!/bin/sh
grep -oE '\[?[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\]?' "$1" 2>/dev/null || echo ""`
		writeGapTool(reg, sandbox, "log_timestamps", script)
	case "avg_word_len":
		script := `#!/bin/sh
awk '{for(i=1;i<=NF;i++){s+=length($i); n++}} END{printf "%.0f\n", s/n}' "$1" 2>/dev/null || echo "0"`
		writeGapTool(reg, sandbox, "avg_word_len", script)
	}
}

// shellCommandTool 包装 shell 脚本为命名工具
type shellCommandTool struct {
	name    string
	desc    string
	script  string
	workdir string
}

func (t *shellCommandTool) Name() string        { return t.name }
func (t *shellCommandTool) Description() string  { return t.desc }
func (t *shellCommandTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"args":{"type":"string","description":"传递给脚本的参数"}},"required":["args"]}`)
}

func (t *shellCommandTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	var params struct {
		Args string `json:"args"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.NewErrorResult("参数解析失败: " + err.Error()), nil
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", t.script)
	cmd.Args = append(cmd.Args, strings.Fields(params.Args)...)
	cmd.Dir = t.workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tools.NewResult(fmt.Sprintf("执行出错: %v\n输出: %s", err, string(out))), nil
	}
	return tools.NewResult(strings.TrimSpace(string(out))), nil
}

func writeGapTool(reg *tools.Registry, sandbox, name, script string) {
	path := filepath.Join(sandbox, ".gap-tools", name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Printf("创建工具目录失败: %v\n", err)
		return
	}
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		fmt.Printf("写入工具脚本失败: %v\n", err)
		return
	}

	descriptions := map[string]string{
		"csv_stats":      "计算 CSV 文件每列的平均值",
		"log_parser":     "统计日志文件中 ERROR 行数",
		"json_merge":     "合并两个 JSON 数组并去重",
		"xml_parser":     "从 XML 文件提取指定标签的值",
		"date_calc":      "计算日期加上指定天数后的日期",
		"hash_gen":       "计算文件的 SHA256 哈希",
		"tsv_extract":    "从 TSV 文件提取指定列",
		"yaml_get":       "从 YAML 文件提取指定字段值",
		"stats_median":   "计算文件中数字的中位数",
		"md_headings":    "提取 Markdown 文件的标题行",
		"email_extract":  "从文件中提取所有邮箱地址",
		"date_range":     "找出文件中最早和最晚日期",
		"csv_transpose":  "将 CSV 文件行列互换",
		"json_filter":    "从 JSON 数组中按条件过滤",
		"word_freq":      "统计文件中词频最高的词",
		"hex_convert":    "将十六进制数转为十进制",
		"template_fill":  "替换模板文件中的占位符",
		"matrix_det":     "计算 3x3 矩阵的行列式",
		"url_domain":     "从 URL 列表中提取域名",
		"base64_decode":  "解码 base64 编码的文件",
		"stats_stddev":   "计算文件中数字的标准差",
		"ini_parse":      "从 INI 文件提取指定 section 的数据",
		"color_complement": "计算 RGB 颜色的互补色",
		"roman_convert":  "将罗马数字转为阿拉伯数字",
		"csv_rowsum":     "为 CSV 每行追加总和",
		"text_censor":    "替换文本中的敏感词",
		"json_pluck":     "从 JSON 数组中提取指定字段",
		"cumsum":         "计算数字文件的累积和",
		"md_table_csv":   "将 Markdown 表格转为 CSV",
		"log_timestamps": "从日志中提取时间戳",
		"avg_word_len":   "计算文件的平均词长",
	}

	desc := descriptions[name]
	if desc == "" {
		desc = "执行 " + name + " 工具"
	}

	t := &shellCommandTool{
		name:    name,
		desc:    desc,
		script:  path,
		workdir: sandbox,
	}
	_ = reg.Register(t)
}

func checkAssertions(sandbox string, asserts []struct {
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	Contains string `json:"contains,omitempty"`
}, response string) bool {
	if len(asserts) == 0 {
		// 无断言时，检查 response 是否包含期望值
		return strings.Contains(strings.ToLower(response), strings.ToLower("done"))
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

func systemPrompt(sandbox string, gapTools ...string) string {
	base := fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
直接开始工作，不要询问用户。`, sandbox)

	if len(gapTools) > 0 {
		base += "\n\n可用专用工具（通过 shell 执行）：\n"
		for _, t := range gapTools {
			base += fmt.Sprintf("- %s/%s（用法: bash %s/%s <参数>）\n", 
				filepath.Join(sandbox, ".gap-tools"), t,
				filepath.Join(sandbox, ".gap-tools"), t)
		}
		base += "\n优先使用专用工具完成任务。"
	}
	return base
}

func loadGapItems() ([]gapItem, error) {
	// 缺口题面：需要专用工具的任务（路径为沙箱内相对路径）
	items := []gapItem{
		{
			ID: "gap-001", Task: "计算 data.csv 中所有数值的平均值，结果写入 result.txt",
			Expected: "42", GapKind: "missing_tool", MissingTool: "csv_stats",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.csv", Inline: "10,20,30\n40,50,60\n70,80,90\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "result.txt", Contains: "50"}},
		},
		{
			ID: "gap-002", Task: "从 log.txt 提取 ERROR 行数，写入 error_count.txt",
			Expected: "3", GapKind: "missing_tool", MissingTool: "log_parser",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "log.txt", Inline: "INFO start\nERROR failed\nINFO retry\nERROR timeout\nINFO done\nERROR crash\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "error_count.txt", Contains: "3"}},
		},
		{
			ID: "gap-003", Task: "合并 a.json 和 b.json 为去重数组，写入 merged.json",
			Expected: "merged", GapKind: "missing_tool", MissingTool: "json_merge",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{
				{Path: "a.json", Inline: "[1,2,3]"},
				{Path: "b.json", Inline: "[2,3,4]"},
			},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "merged.json", Contains: "1"}},
		},
		// 无缺口工具也能用 shell 完成的基线任务
		{
			ID: "gap-004", Task: "统计 files/ 目录下的文件数量，写入 count.txt",
			Expected: "5", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{
				{Path: "files/a.txt", Inline: "a"},
				{Path: "files/b.txt", Inline: "b"},
				{Path: "files/c.txt", Inline: "c"},
				{Path: "files/d.txt", Inline: "d"},
				{Path: "files/e.txt", Inline: "e"},
			},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "count.txt", Contains: "5"}},
		},
		{
			ID: "gap-005", Task: "将 input.txt 内容转为大写，写入 upper.txt",
			Expected: "done", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "input.txt", Inline: "hello world"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "upper.txt", Contains: "HELLO"}},
		},
		// 扩展题面：更多缺口工具类型
		{
			ID: "gap-006", Task: "从 config.xml 提取所有 <port> 标签的值，写入 ports.txt",
			Expected: "ports", GapKind: "missing_tool", MissingTool: "xml_parser",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "config.xml", Inline: "<config><port>8080</port><port>8443</port><port>9000</port></config>"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "ports.txt", Contains: "8080"}},
		},
		{
			ID: "gap-007", Task: "计算 2026-09-05 之后 30 天的日期，写入 future.txt",
			Expected: "date", GapKind: "missing_tool", MissingTool: "date_calc",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "future.txt", Contains: "2026-10-05"}},
		},
		{
			ID: "gap-008", Task: "计算 secret.txt 内容的 SHA256 哈希，写入 hash.txt",
			Expected: "hash", GapKind: "missing_tool", MissingTool: "hash_gen",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "secret.txt", Inline: "hello"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "hash.txt", Contains: "2cf24"}},
		},
		{
			ID: "gap-009", Task: "将 lines.txt 按字母排序，写入 sorted.txt",
			Expected: "sorted", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "lines.txt", Inline: "cherry\napple\nbanana\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "sorted.txt", Contains: "apple"}},
		},
		{
			ID: "gap-010", Task: "找出 nums.txt 中的最大值，写入 max.txt",
			Expected: "max", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "nums.txt", Inline: "15\n42\n28\n7\n33\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "max.txt", Contains: "42"}},
		},
		// ===== 扩量题面 gap-011 ~ gap-059（McNemar +20pp 需 59 题）=====
		// 缺口工具类（missing_tool）
		{
			ID: "gap-011", Task: "从 data.tsv 提取第二列所有值，写入 col2.txt",
			Expected: "tsv", GapKind: "missing_tool", MissingTool: "tsv_extract",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.tsv", Inline: "name\tage\tcity\nalice\t30\tBJ\nbob\t25\tSH\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "col2.txt", Contains: "30"}},
		},
		{
			ID: "gap-012", Task: "将 config.yaml 中 port 字段值提取到 port.txt",
			Expected: "yaml", GapKind: "missing_tool", MissingTool: "yaml_get",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "config.yaml", Inline: "server:\n  host: localhost\n  port: 8080\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "port.txt", Contains: "8080"}},
		},
		{
			ID: "gap-013", Task: "计算 numbers.txt 中所有数的中位数，写入 median.txt",
			Expected: "median", GapKind: "missing_tool", MissingTool: "stats_median",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "numbers.txt", Inline: "10\n20\n30\n40\n50\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "median.txt", Contains: "30"}},
		},
		{
			ID: "gap-014", Task: "将 markdown.md 中的标题行提取到 headings.txt",
			Expected: "headings", GapKind: "missing_tool", MissingTool: "md_headings",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "markdown.md", Inline: "# Title\nSome text\n## Section\nMore text\n### Sub\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "headings.txt", Contains: "# Title"}},
		},
		{
			ID: "gap-015", Task: "从 emails.txt 提取所有邮箱地址，写入 emails_found.txt",
			Expected: "emails", GapKind: "missing_tool", MissingTool: "email_extract",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "emails.txt", Inline: "Contact us at info@example.com or support@test.org for help.\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "emails_found.txt", Contains: "info@example.com"}},
		},
		{
			ID: "gap-016", Task: "计算 dates.txt 中最早和最晚日期，写入 range.txt",
			Expected: "dates", GapKind: "missing_tool", MissingTool: "date_range",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "dates.txt", Inline: "2026-03-15\n2026-01-01\n2026-12-31\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "range.txt", Contains: "2026-01-01"}},
		},
		{
			ID: "gap-017", Task: "将 csv_data.csv 转置（行列互换），写入 transposed.csv",
			Expected: "transpose", GapKind: "missing_tool", MissingTool: "csv_transpose",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "csv_data.csv", Inline: "a,b,c\n1,2,3\n4,5,6\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "transposed.csv", Contains: "a,1,4"}},
		},
		{
			ID: "gap-018", Task: "从 log.json 提取所有 level=ERROR 的 message，写入 errors.txt",
			Expected: "json_filter", GapKind: "missing_tool", MissingTool: "json_filter",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "log.json", Inline: "[{\"level\":\"INFO\",\"message\":\"start\"},{\"level\":\"ERROR\",\"message\":\"fail\"},{\"level\":\"ERROR\",\"message\":\"timeout\"}]\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "errors.txt", Contains: "fail"}},
		},
		{
			ID: "gap-019", Task: "计算 text.txt 的词频统计，取最高频词写入 top_word.txt",
			Expected: "wordfreq", GapKind: "missing_tool", MissingTool: "word_freq",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "the cat sat on the mat the cat\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "top_word.txt", Contains: "the"}},
		},
		{
			ID: "gap-020", Task: "将 hex.txt 中的十六进制数转为十进制，写入 decimal.txt",
			Expected: "hex2dec", GapKind: "missing_tool", MissingTool: "hex_convert",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "hex.txt", Inline: "FF\n1A\n2B\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "decimal.txt", Contains: "255"}},
		},
		{
			ID: "gap-021", Task: "从 template.txt 替换 {{name}} 为 Alice，写入 filled.txt",
			Expected: "template", GapKind: "missing_tool", MissingTool: "template_fill",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "template.txt", Inline: "Hello {{name}}, welcome!\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "filled.txt", Contains: "Alice"}},
		},
		{
			ID: "gap-022", Task: "计算 matrix.txt 中 3x3 矩阵的行列式，写入 det.txt",
			Expected: "matrix", GapKind: "missing_tool", MissingTool: "matrix_det",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "matrix.txt", Inline: "1,2,3\n4,5,6\n7,8,9\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "det.txt", Contains: "0"}},
		},
		{
			ID: "gap-023", Task: "从 urls.txt 提取所有域名，写入 domains.txt",
			Expected: "domains", GapKind: "missing_tool", MissingTool: "url_domain",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "urls.txt", Inline: "https://example.com/path\nhttp://test.org/page\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "domains.txt", Contains: "example.com"}},
		},
		{
			ID: "gap-024", Task: "将 base64.txt 解码，写入 decoded.txt",
			Expected: "base64", GapKind: "missing_tool", MissingTool: "base64_decode",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "base64.txt", Inline: "aGVsbG8gd29ybGQ=\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "decoded.txt", Contains: "hello world"}},
		},
		{
			ID: "gap-025", Task: "计算 scores.txt 的标准差，写入 stddev.txt",
			Expected: "stddev", GapKind: "missing_tool", MissingTool: "stats_stddev",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "scores.txt", Inline: "10\n20\n30\n40\n50\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "stddev.txt", Contains: "14"}},
		},
		// 基线类（baseline）——双臂都应能完成
		{
			ID: "gap-026", Task: "将 words.txt 去重后写入 unique.txt",
			Expected: "unique", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "words.txt", Inline: "apple\nbanana\napple\ncherry\nbanana\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "unique.txt", Contains: "cherry"}},
		},
		{
			ID: "gap-027", Task: "统计 data.txt 的总行数，写入 lines.txt",
			Expected: "lines", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.txt", Inline: "line1\nline2\nline3\nline4\nline5\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "lines.txt", Contains: "5"}},
		},
		{
			ID: "gap-028", Task: "将 mixed.txt 中的数字提取出来写入 numbers_only.txt",
			Expected: "extract_nums", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "mixed.txt", Inline: "abc123def456ghi789\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "numbers_only.txt", Contains: "123"}},
		},
		{
			ID: "gap-029", Task: "将 text.txt 反转（字符顺序颠倒），写入 reversed.txt",
			Expected: "reverse", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "hello\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "reversed.txt", Contains: "olleh"}},
		},
		{
			ID: "gap-030", Task: "找出 data.txt 中最长的行，写入 longest.txt",
			Expected: "longest", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.txt", Inline: "short\nthis is a longer line\nmedium line\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "longest.txt", Contains: "longer"}},
		},
		{
			ID: "gap-031", Task: "将 csv 的第一列作为 key、第二列作为 value 生成简单 key=value 文件",
			Expected: "kv", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "pairs.csv", Inline: "name,Alice\nage,30\ncity,BJ\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "kv.txt", Contains: "name=Alice"}},
		},
		{
			ID: "gap-032", Task: "统计 text.txt 中每个字符出现的次数，写入 char_count.txt",
			Expected: "charcount", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "aabbc\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "char_count.txt", Contains: "a"}},
		},
		{
			ID: "gap-033", Task: "将 input.txt 中的空格替换为下划线，写入 output.txt",
			Expected: "replace", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "input.txt", Inline: "hello world foo bar\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "output.txt", Contains: "hello_world"}},
		},
		{
			ID: "gap-034", Task: "找出 nums.txt 中最小的三个数，写入 top3_min.txt",
			Expected: "min3", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "nums.txt", Inline: "50\n10\n30\n20\n40\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "top3_min.txt", Contains: "10"}},
		},
		{
			ID: "gap-035", Task: "将 multi.txt 的空行去除，写入 compact.txt",
			Expected: "compact", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "multi.txt", Inline: "line1\n\nline2\n\n\nline3\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "compact.txt", Contains: "line1"}},
		},
		{
			ID: "gap-036", Task: "计算 nums.txt 中所有偶数的和，写入 even_sum.txt",
			Expected: "evensum", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "nums.txt", Inline: "1\n2\n3\n4\n5\n6\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "even_sum.txt", Contains: "12"}},
		},
		{
			ID: "gap-037", Task: "将 text.txt 按字典序排列每行，写入 sorted.txt",
			Expected: "sort", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "zebra\napple\nmango\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "sorted.txt", Contains: "apple"}},
		},
		{
			ID: "gap-038", Task: "统计 words.txt 中以 'a' 开头的单词数，写入 a_count.txt",
			Expected: "acount", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "words.txt", Inline: "apple\nbanana\navocado\napricot\ncherry\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "a_count.txt", Contains: "3"}},
		},
		{
			ID: "gap-039", Task: "将 input.txt 中每行首尾空白去除，写入 trimmed.txt",
			Expected: "trim", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "input.txt", Inline: "  hello  \n  world  \n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "trimmed.txt", Contains: "hello"}},
		},
		{
			ID: "gap-040", Task: "找出 data.txt 中出现次数最多的行，写入 mode.txt",
			Expected: "mode", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.txt", Inline: "apple\nbanana\napple\ncherry\napple\nbanana\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "mode.txt", Contains: "apple"}},
		},
		// 更多缺口工具类
		{
			ID: "gap-041", Task: "从 ini.txt 提取 [section] 下的 key=value 对，写入 section_data.txt",
			Expected: "ini", GapKind: "missing_tool", MissingTool: "ini_parse",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "ini.txt", Inline: "[database]\nhost=localhost\nport=5432\n[app]\nname=test\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "section_data.txt", Contains: "host=localhost"}},
		},
		{
			ID: "gap-042", Task: "计算 rgb.txt 中颜色的互补色（255-R,255-G,255-B），写入 complement.txt",
			Expected: "rgb", GapKind: "missing_tool", MissingTool: "color_complement",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "rgb.txt", Inline: "100,150,200\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "complement.txt", Contains: "155"}},
		},
		{
			ID: "gap-043", Task: "将 roman.txt 中的罗马数字转为阿拉伯数字，写入 arabic.txt",
			Expected: "roman", GapKind: "missing_tool", MissingTool: "roman_convert",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "roman.txt", Inline: "XIV\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "arabic.txt", Contains: "14"}},
		},
		{
			ID: "gap-044", Task: "从 csv 计算每行的总和，追加到行末，写入 with_sum.csv",
			Expected: "rowsum", GapKind: "missing_tool", MissingTool: "csv_rowsum",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.csv", Inline: "1,2,3\n4,5,6\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "with_sum.csv", Contains: "6"}},
		},
		{
			ID: "gap-045", Task: "将 text.txt 中的敏感词替换为 ***，写入 censored.txt",
			Expected: "censor", GapKind: "missing_tool", MissingTool: "text_censor",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "This is bad and terrible content\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "censored.txt", Contains: "***"}},
		},
		{
			ID: "gap-046", Task: "从 json 数组中提取所有 name 字段，写入 names.txt",
			Expected: "json_names", GapKind: "missing_tool", MissingTool: "json_pluck",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "people.json", Inline: "[{\"name\":\"Alice\",\"age\":30},{\"name\":\"Bob\",\"age\":25}]\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "names.txt", Contains: "Alice"}},
		},
		{
			ID: "gap-047", Task: "计算 numbers.txt 的累积和，写入 cumulative.txt",
			Expected: "cumsum", GapKind: "missing_tool", MissingTool: "cumsum",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "numbers.txt", Inline: "1\n2\n3\n4\n5\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "cumulative.txt", Contains: "10"}},
		},
		{
			ID: "gap-048", Task: "将 markdown 表格转换为 CSV 格式，写入 table.csv",
			Expected: "md2csv", GapKind: "missing_tool", MissingTool: "md_table_csv",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "table.md", Inline: "| name | age |\n|------|-----|\n| Alice | 30 |\n| Bob | 25 |\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "table.csv", Contains: "name,age"}},
		},
		{
			ID: "gap-049", Task: "从 log 中提取所有时间戳，写入 timestamps.txt",
			Expected: "timestamps", GapKind: "missing_tool", MissingTool: "log_timestamps",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "log.txt", Inline: "[2026-09-05 10:00:00] INFO start\n[2026-09-05 10:01:00] ERROR fail\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "timestamps.txt", Contains: "2026-09-05"}},
		},
		{
			ID: "gap-050", Task: "计算 text.txt 的平均词长，写入 avg_len.txt",
			Expected: "avgwordlen", GapKind: "missing_tool", MissingTool: "avg_word_len",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "hi there world\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "avg_len.txt", Contains: "4"}},
		},
		// 更多基线类
		{
			ID: "gap-051", Task: "将 a.txt 和 b.txt 合并为 ab.txt（交替行）",
			Expected: "interleave", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{
				{Path: "a.txt", Inline: "a1\na2\na3\n"},
				{Path: "b.txt", Inline: "b1\nb2\nb3\n"},
			},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "ab.txt", Contains: "a1"}},
		},
		{
			ID: "gap-052", Task: "找出 data.txt 中的重复行，写入 dupes.txt",
			Expected: "dupes", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.txt", Inline: "apple\nbanana\napple\ncherry\nbanana\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "dupes.txt", Contains: "apple"}},
		},
		{
			ID: "gap-053", Task: "将 text.txt 的每行加上行号前缀，写入 numbered.txt",
			Expected: "numbered", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "first\nsecond\nthird\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "numbered.txt", Contains: "1"}},
		},
		{
			ID: "gap-054", Task: "计算 nums.txt 中相邻两数的差，写入 diffs.txt",
			Expected: "diffs", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "nums.txt", Inline: "10\n15\n25\n40\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "diffs.txt", Contains: "5"}},
		},
		{
			ID: "gap-055", Task: "将 input.txt 中所有数字求和，写入 sum.txt",
			Expected: "digitsum", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "input.txt", Inline: "abc12def34ghi56\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "sum.txt", Contains: "102"}},
		},
		{
			ID: "gap-056", Task: "将 text.txt 按单词反转（每个单词内字符反转），写入 word_reversed.txt",
			Expected: "wordrev", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "hello world\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "word_reversed.txt", Contains: "olleh"}},
		},
		{
			ID: "gap-057", Task: "统计 text.txt 中元音字母的数量，写入 vowels.txt",
			Expected: "vowels", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "hello world apple\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "vowels.txt", Contains: "7"}},
		},
		{
			ID: "gap-058", Task: "将 csv 按第二列排序，写入 sorted.csv",
			Expected: "csvsort", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "data.csv", Inline: "charlie,30\nalice,25\nbob,35\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "sorted.csv", Contains: "alice"}},
		},
		{
			ID: "gap-059", Task: "计算 text.txt 的字符总数（不含换行），写入 char_total.txt",
			Expected: "chartotal", GapKind: "baseline",
			Fixtures: []struct {
				Path   string `json:"path"`
				Inline string `json:"inline"`
			}{{Path: "text.txt", Inline: "hello\nworld\n"}},
			SuccessAssert: []struct {
				Kind     string `json:"kind"`
				Path     string `json:"path,omitempty"`
				Contains string `json:"contains,omitempty"`
			}{{Kind: "file_contains", Path: "char_total.txt", Contains: "10"}},
		},
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
	toolUsage := make(map[string]int)

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
		for _, t := range r.ToolNames {
			toolUsage[t]++
		}
	}

	fmt.Printf("\n===== 汇总 =====\n")
	if aTotal > 0 {
		fmt.Printf("A 臂（基线）: %d/%d (%.1f%%)\n", aSuccess, aTotal, 100*float64(aSuccess)/float64(aTotal))
	}
	if bTotal > 0 {
		fmt.Printf("B 臂（缺口工具）: %d/%d (%.1f%%)\n", bSuccess, bTotal, 100*float64(bSuccess)/float64(bTotal))
	}

	reused := 0
	for _, count := range toolUsage {
		if count >= 2 {
			reused++
		}
	}
	totalTools := len(toolUsage)
	if totalTools > 0 {
		fmt.Printf("工具复用率: %d/%d (%.1f%%)\n", reused, totalTools, 100*float64(reused)/float64(totalTools))
	}
}
