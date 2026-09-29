// main.go — AgentPrimordia V7 弧线 S0-1：真实轨（nightly）评测运行器
//
// 对 docs/evals/ 冻结题面的留出子集跑真实 LLM Provider，产出 R3 口径报告：
//
//	--mode external  外部泛化集：逐题问答 + answer_check.exact 机检判分；
//	--mode judge     judge 标定集：LLM-as-judge 裁决 vs 注册客观标签 → Cohen κ（门 ≥0.6）。
//
// 报告落 bench/results/s0/，含题面 sha256 与冻结 commit（与 manifest.json 对账，趋势可溯源）。
// 未配置 API Key 时打印降级豁免说明并退出 0——真实线依赖 A1（CI secrets），
// 按 V7路线图 §九 记录豁免，绝不伪造数字。
//
// 用法：
//
//	export OPENAI_API_KEY=sk-xxx
//	go run ./bench/eval-real --mode external --set external-general-v1.json --holdout --out bench/results/s0/x.json
//	go run ./bench/eval-real --mode judge --set judge-calibration-v1.json --holdout --out bench/results/s0/j.json
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/eval"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

func main() {
	var (
		mode      = flag.String("mode", "external", "评测模式: external（机检问答）/ judge（κ 标定）")
		set       = flag.String("set", "external-general-v1.json", "题面文件名（docs/evals/ 下）")
		holdout   = flag.Bool("holdout", true, "只跑留出子集（验收口径；false 跑全量）")
		limit     = flag.Int("limit", 0, "仅运行前 N 条（0 = 全部；冒烟用）")
		provider  = flag.String("provider", "openai", "LLM Provider: openai/anthropic/deepseek/qwen/gemini/glm")
		model     = flag.String("model", "gpt-4o-mini", "被测模型名")
		apiKey    = flag.String("api-key", "", "API Key（默认从环境变量读取）")
		baseURL   = flag.String("base-url", "", "自定义 Base URL（OpenAI 兼容网关）")
		out       = flag.String("out", "", "报告输出路径（空则只打印汇总）")
		timeout   = flag.Duration("timeout", 30*time.Minute, "单次运行总超时")
		kappaGate = flag.Float64("kappa-gate", 0.6, "judge 标定 κ 门限（仅 --mode judge）")
	)
	flag.Parse()

	key := *apiKey
	if key == "" {
		key = envKeyForProvider(*provider)
	}
	if key == "" {
		// 降级豁免（V7路线图 §九 A1）：真实轨依赖 CI secrets，未配置时明确记录，不伪造数字。
		fmt.Printf("SKIP provider=%s model=%s set=%s 原因=API_KEY 未配置（降级豁免 A1：真实轨待 secrets，回放轨照常）\n",
			*provider, *model, *set)
		return
	}

	p, err := newProvider(*provider, key, *model, *baseURL)
	if err != nil {
		fatal(err)
	}

	items, err := eval.LoadSet(eval.DefaultEvalsDir(), *set, *holdout)
	if err != nil {
		fatal(fmt.Errorf("装载题面失败: %w", err))
	}
	if *limit > 0 && *limit < len(items) {
		items = items[:*limit]
	}
	if len(items) == 0 {
		fatal(fmt.Errorf("题面 %s（holdout=%v）为空", *set, *holdout))
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	complete := func(_ context.Context, prompt string) (string, error) {
		resp, err := p.Complete(ctx, &llm.CompletionRequest{Messages: []llm.ChatMessage{{Role: "user", Content: prompt}}})
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}

	report := eval.RealEvalReport{
		Set: *set, Track: eval.TrackReal, Provider: *provider, Model: *model,
		HoldoutOnly: *holdout, GeneratedAt: eval.NowRFC3339(),
	}
	if reg, rerr := eval.LoadRegistry(eval.DefaultEvalsDir()); rerr == nil {
		report.FreezeCommit = reg.FreezeCommit
		for _, f := range reg.Files {
			if filepath.Base(f.File) == *set {
				report.SetSHA256 = f.SHA256
			}
		}
	}

	switch *mode {
	case "external":
		report.Mode = "external"
		records, rerr := eval.RunExternalGeneralReal(ctx, items, complete)
		if rerr != nil {
			fatal(rerr)
		}
		report.Records = records
	case "judge":
		report.Mode = "judge"
		judge := func(_ context.Context, task, response string) (string, error) {
			reply, cerr := complete(context.Background(), eval.JudgePrompt(task, response))
			if cerr != nil {
				return "", cerr
			}
			return eval.ParseJudgeVerdict(reply)
		}
		records, rerr := eval.RunJudgeCalibration(ctx, items, judge)
		if rerr != nil {
			fatal(rerr)
		}
		report.Records = records
	default:
		fatal(fmt.Errorf("未知模式 %q", *mode))
	}

	eval.SortRecords(report.Records)
	rp, serr := eval.SummarizeRealEval(report.Records)
	if serr != nil {
		fatal(serr)
	}
	report.Summary = rp

	if report.Mode == "judge" {
		k, used, dropped, kerr := eval.JudgeCalibrationKappa(report.Records)
		if kerr != nil {
			fatal(kerr)
		}
		report.Kappa = &k
		report.KappaDropped = dropped
		fmt.Printf("judge 标定：κ=%.4f（双标 %d，剔除披露 %d）；一致率 %s\n", k, used, dropped, rp.String())
		if k < *kappaGate {
			fmt.Printf("❌ κ=%.4f < 门限 %.2f（S0-1 验收：judge 与客观标签 κ ≥0.6）\n", k, *kappaGate)
			os.Exit(1)
		}
		fmt.Println("✅ κ 达标")
	} else {
		fmt.Println("真实轨通过率", rp.String())
	}

	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fatal(err)
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(*out, append(data, byte(10)), 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("报告已写入 %s\n", *out)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "eval-real:", err)
	os.Exit(1)
}

// newProvider 按名称构造 Provider（与 bench/llm-bench 同一工厂口径）。
func newProvider(name, apiKey, model, baseURL string) (llm.Provider, error) {
	if apiKey == "" {
		return nil, errors.New("API key 为空")
	}
	cfg := llm.Config{APIKey: apiKey, Model: model, BaseURL: baseURL}
	switch name {
	case "openai":
		return llm.NewOpenAIProvider(cfg)
	case "anthropic":
		return llm.NewAnthropicProvider(cfg)
	case "deepseek":
		return llm.NewDeepSeekProvider(cfg)
	case "qwen":
		return llm.NewQwenProvider(cfg)
	case "gemini":
		return llm.NewGeminiProvider(cfg)
	case "glm":
		return llm.NewGLMProvider(cfg)
	default:
		return nil, fmt.Errorf("未知 Provider %q", name)
	}
}

func envKeyForProvider(name string) string {
	// 返回环境变量的「值」而非名字——漏掉 Getenv 会把变量名当密钥用，
	// 导致真实轨带着假密钥打网关并全部计失败（2026-08-31 冒烟实测踩过）。
	switch name {
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "deepseek":
		return os.Getenv("DEEPSEEK_API_KEY")
	case "qwen":
		return os.Getenv("QWEN_API_KEY")
	case "gemini":
		return os.Getenv("GEMINI_API_KEY")
	case "glm":
		return os.Getenv("GLM_API_KEY")
	default:
		return os.Getenv(strings.ToUpper(name) + "_API_KEY")
	}
}
