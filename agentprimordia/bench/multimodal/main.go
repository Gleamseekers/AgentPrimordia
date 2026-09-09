// main.go — AgentPrimordia 多模态 A/B bench 运行器
//
// A 臂：纯文本（图像信息以 OCR 文字注入 system prompt）
// B 臂：原生多模态（图像通过 ContentParts 传入）
//
// 用法：
//
//	go run ./bench/multimodal --model sensenova-6.8-flash-lite \
//	  --base-url https://token.sensenova.cn/v1 \
//	  --api-key xxx --out bench/results/multimodal
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
	"agentprimordia/internal/agent/multimodal"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// mmTask 多模态题面
type mmTask struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"` // visual_qa / audio_transcribe / mixed_reasoning
	Task     string     `json:"task"`
	Expected string     `json:"expected"`
	Fixtures []mmFixture `json:"fixtures"`
}

// mmFixture 题面附件
type mmFixture struct {
	Path      string `json:"path"`
	InlineB64 string `json:"inline_b64"`
}

// mmResult 单元结果
type mmResult struct {
	Item        string `json:"item"`
	Kind        string `json:"kind"`
	Arm         string `json:"arm"`
	Success     bool   `json:"success"`
	Turns       int    `json:"turns"`
	DurationSec int    `json:"duration_sec"`
	Error       string `json:"error,omitempty"`
}

func main() {
	var (
		model     = flag.String("model", "sensenova-6.8-flash-lite", "model name")
		apiKey    = flag.String("api-key", "", "API key")
		baseURL   = flag.String("base-url", "", "base URL")
		outDir    = flag.String("out", "bench/results/multimodal", "output directory")
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

	tasks, err := loadTasks()
	if err != nil {
		fmt.Printf("加载题面失败: %v\n", err)
		os.Exit(1)
	}
	if *limit > 0 && *limit < len(tasks) {
		tasks = tasks[:*limit]
	}
	fmt.Printf("多模态题面: %d 条\n", len(tasks))

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
	for i, task := range tasks {
		for _, arm := range []string{"A", "B"} {
			key := fmt.Sprintf("%s/%s", task.ID, arm)
			if _, done := results[key]; done {
				continue
			}

			fmt.Printf("[%d/%d] %s arm=%s ...", i+1, len(tasks), task.ID, arm)
			r := runUnit(ctx, prov, task, arm)
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

func runUnit(ctx context.Context, prov llm.Provider, task mmTask, arm string) mmResult {
	start := time.Now()
	r := mmResult{Item: task.ID, Kind: task.Kind, Arm: arm}

	sandbox, err := os.MkdirTemp("", "mm-"+task.ID+"-")
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}
	defer os.RemoveAll(sandbox)

	// fixtures 注入沙箱
	for _, fx := range task.Fixtures {
		p := filepath.Join(sandbox, filepath.FromSlash(fx.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			r.Error = err.Error()
			r.DurationSec = int(time.Since(start).Seconds())
			return r
		}
		if err := os.WriteFile(p, []byte(fx.InlineB64), 0644); err != nil {
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

	// 构造系统提示与消息
	var sysPrompt string
	var inputMsg agent.Message

	if arm == "A" {
		// A 臂：纯文本模式——图像信息以 OCR 文字注入 system prompt
		sysPrompt = systemPromptText(sandbox, task)
		inputMsg = agent.UserMessage(task.Task)
	} else {
		// B 臂：原生多模态——图像通过 ContentParts 传入
		sysPrompt = systemPromptMultimodal(sandbox)
		inputMsg = buildMultimodalMessage(task)
	}

	ag, err := agent.NewAgent("mm-"+task.ID, sysPrompt, prov,
		agent.WithMaxTurns(20),
		agent.WithToolkit(reg),
		agent.WithCheckpointStore(ckpt),
		agent.WithSessionID(fmt.Sprintf("%s-%s", task.ID, arm)),
	)
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	resp, err := ag.Run(ctx, inputMsg)
	if err != nil {
		r.Error = err.Error()
		r.DurationSec = int(time.Since(start).Seconds())
		return r
	}

	r.Turns = resp.Metrics.TotalTurns
	r.DurationSec = int(time.Since(start).Seconds())

	// 判定：response 包含 expected 值
	r.Success = strings.Contains(
		strings.ToLower(resp.Content),
		strings.ToLower(task.Expected),
	)

	return r
}

// buildMultimodalMessage 构建多模态消息（B 臂使用）
func buildMultimodalMessage(task mmTask) agent.Message {
	parts := []multimodal.ContentPart{
		{Type: "text", Text: task.Task},
	}

	// 将图像附件作为 image_url ContentPart 添加
	for _, fx := range task.Fixtures {
		if isImageFixture(fx.Path) {
			parts = append(parts, multimodal.ContentPart{
				Type: "image_url",
				URL:  fx.Path, // 沙箱内路径，实际使用时需转为 data URI 或 URL
				MIME: guessMIME(fx.Path),
			})
		} else if isAudioFixture(fx.Path) {
			parts = append(parts, multimodal.ContentPart{
				Type: "audio",
				Data: fx.InlineB64,
				MIME: guessMIME(fx.Path),
			})
		}
	}

	return agent.Message{
		Role:         "user",
		ContentParts: parts,
	}
}

// isImageFixture 判断附件路径是否为图像
func isImageFixture(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp"
}

// isAudioFixture 判断附件路径是否为音频
func isAudioFixture(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".wav" || ext == ".mp3" || ext == ".ogg" || ext == ".flac"
}

// guessMIME 根据文件扩展名猜测 MIME 类型
func guessMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
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

func systemPromptText(sandbox string, task mmTask) string {
	base := fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
直接开始工作，不要询问用户。`, sandbox)

	// A 臂：将图像信息以 OCR 文字形式注入
	base += "\n\n注意：你无法直接看到图像。以下是图像的文字描述（OCR）：\n"
	for _, fx := range task.Fixtures {
		if isImageFixture(fx.Path) {
			base += fmt.Sprintf("- %s: [图像占位——实际 OCR 结果]\n", fx.Path)
		}
		if isAudioFixture(fx.Path) {
			base += fmt.Sprintf("- %s: [音频占位——实际转录结果]\n", fx.Path)
		}
	}
	return base
}

func systemPromptMultimodal(sandbox string) string {
	return fmt.Sprintf(`你在一个隔离沙箱目录中工作（根目录: %s）。
使用提供的工具完成任务。所有产物必须写在该目录内。
直接开始工作，不要询问用户。
你可以直接看到图像和听到音频，请充分利用多模态信息。`, sandbox)
}

func loadTasks() ([]mmTask, error) {
	data, err := os.ReadFile("bench/multimodal/tasks.json")
	if err != nil {
		return nil, fmt.Errorf("读取 tasks.json: %w", err)
	}
	var tasks []mmTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("解析 tasks.json: %w", err)
	}
	return tasks, nil
}

func loadResults(path string) (map[string]mmResult, error) {
	results := make(map[string]mmResult)
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
		var r mmResult
		if err := dec.Decode(&r); err != nil {
			break
		}
		results[fmt.Sprintf("%s/%s", r.Item, r.Arm)] = r
	}
	return results, nil
}

func summarize(results map[string]mmResult) {
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

	fmt.Printf("\n===== 多模态 A/B Bench 汇总 =====\n")
	if aTotal > 0 {
		fmt.Printf("A 臂（纯文本）: %d/%d (%.1f%%)\n", aSuccess, aTotal, 100*float64(aSuccess)/float64(aTotal))
	}
	if bTotal > 0 {
		fmt.Printf("B 臂（原生多模态）: %d/%d (%.1f%%)\n", bSuccess, bTotal, 100*float64(bSuccess)/float64(bTotal))
	}
	if aTotal > 0 && bTotal > 0 {
		gain := 100*float64(bSuccess)/float64(bTotal) - 100*float64(aSuccess)/float64(aTotal)
		fmt.Printf("多模态增益: %.1fpp\n", gain)
	}

	// 按题型分类统计
	kinds := map[string]struct{}{}
	for _, r := range results {
		kinds[r.Kind] = struct{}{}
	}
	for k := range kinds {
		var ka, kb, kaS, kbS int
		for _, r := range results {
			if r.Kind != k {
				continue
			}
			if r.Arm == "A" {
				ka++
				if r.Success {
					kaS++
				}
			} else {
				kb++
				if r.Success {
					kbS++
				}
			}
		}
		if ka > 0 || kb > 0 {
			fmt.Printf("  %s: A=%d/%d B=%d/%d\n", k, kaS, ka, kbS, kb)
		}
	}
}
