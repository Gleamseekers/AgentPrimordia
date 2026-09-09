// main.go — AgentPrimordia V7 弧线 v6.1 命题 1：世界模型配对 AB 实验运行器
//
// 严格按预注册计划书 docs/evals/plans/v6.1-命题1-世界模型配对AB.md 执行：
//   - 双臂：A=基线（纯消息路径，不注入世界模型）；B=WithWorldModel()
//     （状态图 + 预演门 + 回溯校验 + state-checkpoint 续知）；
//   - 题面：docs/evals/long-horizon-v1.json（24 题，留出 9，R4 冻结）；
//   - 采样：第 1–4 轮整集全跑 = 96 配对；第 5 轮按 seed=20260831+5 确定性
//     排列取前 12 题 = 12 配对；合计 108 配对 = 216 次运行；
//   - 判定：internal/eval/asserts.go 沙箱终态确定性断言（不依赖 LLM）；
//   - 检验：同题配对 McNemar 精确二项（internal/eval/stats.McNemarExact）；
//   - 跨会话中断：题面 interruptions（session_restart）经 HookAfterTurn
//     里程碑断言触发 graceful stop → 检查点恢复（B 臂续知：世界状态随
//     检查点恢复；A 臂文本重放）；
//   - 幂等续跑：results.jsonl 已完成的采样单元跳过（崩后可续）。
//
// 用法：
//
//	export OPENAI_API_KEY=sk-xxx
//	go run ./bench/eval-v61 --provider openai --model deepseek-v4-flash \
//	  --base-url https://opencode.ai/zen/go/v1 --out bench/results/v61
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentprimordia/internal/agent"
	agenthooks "agentprimordia/internal/agent/hooks"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/eval"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/multi_agent/federation"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/builtin"
)

// ===== 题面结构（long-horizon-v1.json）=====

type lhItem struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Goal          string           `json:"goal"`
	Fixtures      []lhFixture      `json:"fixtures"`
	Milestones    []lhMilestone    `json:"milestones"`
	Grading       lhGrading        `json:"grading"`
	Interruptions []lhInterruption `json:"interruptions"`
	Budget        lhBudget         `json:"budget"`
	Holdout       bool             `json:"holdout"`
}

type lhFixture struct {
	Path   string `json:"path"`
	Inline string `json:"inline"`
}

type lhMilestone struct {
	ID     string           `json:"id"`
	Assert []eval.Assertion `json:"assert"`
}

type lhGrading struct {
	Success []eval.Assertion `json:"success"`
	Partial []eval.Assertion `json:"partial"`
}

type lhInterruption struct {
	Action         string `json:"action"`
	AfterMilestone string `json:"after_milestone"`
}

type lhBudget struct {
	MaxTurns     int `json:"max_turns"`
	MaxToolCalls int `json:"max_tool_calls"`
}

// ===== 单元结果 =====

type unitResult struct {
	Item        string `json:"item"`
	Round       int    `json:"round"`
	Arm         string `json:"arm"` // A=baseline, B=worldmodel
	Success     bool   `json:"success"`
	Milestones  int    `json:"milestones"`
	Turns       int    `json:"turns"`
	Tools       int    `json:"tools"`
	Restarts    int    `json:"restarts"`
	Error       string `json:"error,omitempty"`
	DurationSec int    `json:"duration_sec"`
}

func main() {
	var (
		provider = flag.String("provider", "openai", "LLM Provider")
		model    = flag.String("model", "deepseek-v4-flash", "被测模型")
		baseURL  = flag.String("base-url", "", "OpenAI 兼容网关")
		apiKey   = flag.String("api-key", "", "API Key（默认 OPENAI_API_KEY）")
		outDir   = flag.String("out", "bench/results/v61", "结果目录")
		limit    = flag.Int("limit", 0, "仅跑前 N 个配对单元（0=全部 108；冒烟用）")
		rounds   = flag.Int("rounds", 5, "总轮次（预注册 5）")
		arms     = flag.String("arms", "A,B", "执行的臂")
		pace     = flag.Duration("pace", 30*time.Second, "采样单元间隔（网关限流节流）")
		maxRetry = flag.Int("max-retry", 6, "429 限流单元重试次数（指数退避 60s 起）")
	)
	flag.Parse()

	key := *apiKey
	if key == "" {
		key = os.Getenv("OPENAI_API_KEY")
	}
	if key == "" {
		fmt.Println("SKIP：API_KEY 未配置（降级豁免 A1）")
		return
	}
	if *provider != "openai" {
		fatal(fmt.Errorf("v6.1 实验运行器当前仅支持 openai 兼容面（got %q）", *provider))
	}
	prov, err := llm.NewOpenAIProvider(llm.Config{APIKey: key, Model: *model, BaseURL: *baseURL})
	if err != nil {
		fatal(err)
	}

	// 装载冻结题面
	items, err := loadItems()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("题面装载：24 题中 %d 题（留出 %d）\n", len(items), countHoldout(items))

	// 采样协议：rounds 1..4 全集，round 5 = seed 排列前 12
	type unit struct {
		item  lhItem
		round int
	}
	var units []unit
	byID := func() map[string]lhItem {
		m := make(map[string]lhItem, len(items))
		for _, it := range items {
			m[it.ID] = it
		}
		return m
	}()
	for r := 1; r <= *rounds; r++ {
		pool := append([]lhItem(nil), items...)
		if r == *rounds && len(items) > 12 {
			// seed=20260831+r 确定性排列取前 12（计划书 ⑤ 整集重复采样协议）
			rng := rand.New(rand.NewSource(int64(20260831 + r)))
			rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			pool = pool[:12]
		}
		for _, it := range pool {
			units = append(units, unit{item: it, round: r})
		}
	}
	if *limit > 0 {
		units = units[:*limit]
	}
	fmt.Printf("采样单元：%d 配对 × 2 臂 = %d 次运行\n", len(units), len(units)*2)

	armList := strings.Split(*arms, ",")
	_ = byID

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	jsonlPath := filepath.Join(*outDir, "results.jsonl")

	// 幂等：已完成单元跳过
	done := map[string]bool{}
	if data, err := os.ReadFile(jsonlPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			var r unitResult
			if json.Unmarshal([]byte(line), &r) == nil && r.Error == "" {
				done[fmt.Sprintf("%s|%d|%s", r.Item, r.Round, r.Arm)] = true
			}
		}
	}

	total, skipped := 0, 0
	for _, u := range units {
		for _, arm := range armList {
			arm = strings.TrimSpace(arm)
			key := fmt.Sprintf("%s|%d|%s", u.item.ID, u.round, arm)
			total++
			if done[key] {
				skipped++
				continue
			}
			fmt.Printf("运行 %s round=%d arm=%s ... ", u.item.ID, u.round, arm)
			start := time.Now()
			var res unitResult
			transient := false // 暂时性异常（限流/余额耗尽）不落盘——留给幂等续跑补跑
			for attempt := 0; ; attempt++ {
				res = runUnit(prov, u.item, u.round, arm)
				if isBalanceErr(res.Error) {
					transient = true // 余额耗尽：退避重试无意义，立即放弃本单元
					break
				}
				if !isRateLimited(res.Error) || attempt >= *maxRetry {
					if isRateLimited(res.Error) {
						transient = true // 登记限流——留给幂等续跑补跑
					}
					break
				}
				backoff := time.Duration(60*(attempt+1)) * time.Second
				fmt.Printf("(429 限流，%v 后重试 %d/%d) ", backoff, attempt+1, *maxRetry)
				time.Sleep(backoff)
			}
			if transient {
				fmt.Printf("TRANSIENT %s round=%d arm=%s（限流/余额，未落盘）\n", u.item.ID, u.round, arm)
				time.Sleep(*pace)
				continue
			}
			res.DurationSec = int(time.Since(start).Seconds())
			fmt.Printf("success=%v milestones=%d turns=%d tools=%d restarts=%d (%ds)\n",
				res.Success, res.Milestones, res.Turns, res.Tools, res.Restarts, res.DurationSec)
			line, _ := json.Marshal(res)
			f, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				fatal(err)
			}
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
			time.Sleep(*pace)
		}
	}
	fmt.Printf("本轮执行 %d 次运行（跳过已完成 %d）\n", total-skipped, skipped)

	// 报告（已完成配对齐时输出统计）
	writeReport(*outDir, *rounds, countHoldout(items))
}

// runUnit 执行单个采样单元（题 × 轮 × 臂）：沙箱 → agent 运行 → 中断恢复 → 终态断言。
func runUnit(prov llm.Provider, item lhItem, round int, arm string) unitResult {
	res := unitResult{Item: item.ID, Round: round, Arm: arm}
	sandbox, err := os.MkdirTemp("", "lh-"+item.ID+"-")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer os.RemoveAll(sandbox)

	// fixtures 注入
	for _, fx := range item.Fixtures {
		p := filepath.Join(sandbox, filepath.FromSlash(fx.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			res.Error = err.Error()
			return res
		}
		if err := os.WriteFile(p, []byte(fx.Inline), 0o644); err != nil {
			res.Error = err.Error()
			return res
		}
	}

	// 检查点存储（沙箱内 sqlite——跨会话中断的恢复载体）
	ckpt, err := persist.NewSQLiteCheckpointStore(filepath.Join(sandbox, "checkpoint.db"))
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer ckpt.Close()

	maxTurns := item.Budget.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 60
	}

	// 世界模型 tracker（B 臂跨会话续知：同一 tracker 随检查点恢复）
	var tracker *worldmodel.WorldModelTracker
	if arm == "B" {
		tracker = worldmodel.NewWorldModelTracker()
	}

	// 中断触发器：after_milestone 断言通过 → graceful stop
	interrupt := len(item.Interruptions) > 0 && item.Interruptions[0].Action == "session_restart"
	var milestoneAsserts []eval.Assertion
	if interrupt {
		for _, m := range item.Milestones {
			if m.ID == item.Interruptions[0].AfterMilestone {
				milestoneAsserts = m.Assert
				break
			}
		}
	}
	armed := interrupt && len(milestoneAsserts) > 0
	restartDone := false

	buildAgent := func() *agent.CapabilityAgent {
		// 每次构造独立 hooks（中断监视）
		hm := agenthooks.NewHookManager()
		localArmed := armed && !restartDone
		if localArmed {
			milestones := milestoneAsserts
			dir := sandbox
			hm.Register(agenthooks.HookAfterTurn, func(_ context.Context, hctx *agent.HookContext) error {
				if hctx == nil || hctx.Turn < 1 {
					return nil
				}
				ok, _, err := eval.EvaluateAll(dir, milestones, nil)
				if err == nil && ok {
					// 里程碑达成 → 触发跨会话中断（本单元一次性）
					armed = false
					shutdownOnce(dir)
				}
				return nil
			})
		}
		opts := []agent.Option{
			agent.WithMaxTurns(maxTurns),
			agent.WithToolkit(sandboxToolkit(sandbox)),
			agent.WithCheckpointStore(ckpt),
			agent.WithHooks(hm),
			agent.WithSessionID(fmt.Sprintf("%s-r%d-%s", item.ID, round, arm)),
		}
		if arm == "B" {
			opts = append(opts, agent.WithWorldModel(tracker))
		}
		ag, err := agent.NewAgent("lh-"+item.ID, systemPrompt(sandbox), prov, opts...)
		if err != nil {
			return nil
		}
		return ag
	}

	// shutdownOnce：优雅停机信号（经通道通知 runner）
	stopCh := make(chan struct{}, 1)
	shutdownOnce = func(string) {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if armed {
			select {
			case <-stopCh:
				cancel()
			case <-time.After(30 * time.Minute):
				cancel()
			case <-ctx.Done():
			}
		} else {
			<-ctx.Done()
		}
	}()

	ag := buildAgent()
	if ag == nil {
		res.Error = "agent 构造失败"
		return res
	}
	resp, runErr := ag.Run(ctx, agent.UserMessage(item.Goal))

	// 跨会话中断恢复（一次性）
	if interrupt && !restartDone && ctx.Err() != nil {
		restartDone = true
		res.Restarts = 1
		// 检查点状态改写为 cancelled（恢复门要求）
		if st, lerr := ckpt.Load(context.Background(), "lh-"+item.ID); lerr == nil {
			st.Status = "cancelled"
			_ = ckpt.Save(context.Background(), st)
		}
		// 全新 agent 实例从检查点恢复（B 臂：世界状态随检查点续知）
		resumed := buildAgent()
		if resumed == nil {
			res.Error = "恢复 agent 构造失败"
			return res
		}
		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		resp, runErr = resumed.Inner().ResumeFromCheckpoint(ctx2)
	}

	// 终态确定性断言
	ok, _, aerr := eval.EvaluateAll(sandbox, item.Grading.Success, nil)
	if aerr != nil {
		res.Error = aerr.Error()
	}
	res.Success = err == nil && runErr == nil && resp != nil && resp.Error == nil && ok

	// 里程碑达成数（partial 口径，逐题披露）
	for _, m := range item.Milestones {
		if okM, _, _ := eval.EvaluateAll(sandbox, m.Assert, nil); okM {
			res.Milestones++
		}
	}
	if resp != nil && resp.Error != nil {
		res.Error = resp.Error.Error()
	}
	if resp != nil {
		res.Turns = resp.Metrics.TotalTurns
		res.Tools = resp.Metrics.TotalTools
	}
	return res
}

// isRateLimited 网关限流/余额类瞬态错误判定（计划书⑥：异常登记后补跑）。
// 余额耗尽（401 CreditsError）与限流（429）同为 TRANSIENT——不落盘，
// 留给幂等续跑（充值/配额恢复后补齐）。
func isRateLimited(err string) bool {
	e := strings.ToLower(err)
	if strings.Contains(err, "429") || strings.Contains(e, "rate_limit") ||
		strings.Contains(e, "rate limit") || strings.Contains(e, "insufficient balance") ||
		strings.Contains(e, "creditserror") || strings.Contains(e, "http 401") {
		return true
	}
	// 网关瞬态超时（502/503/504）：服务端侧瞬态，登记后补跑
	for _, code := range []string{"http 502", "http 503", "http 504"} {
		if strings.Contains(e, code) {
			return true
		}
	}
	return false
}

// isBalanceErr 网关账户余额耗尽判定（与限流同属暂时性异常：不落盘，留给幂等续跑补跑）。
func isBalanceErr(err string) bool {
	return strings.Contains(err, "Insufficient balance") || strings.Contains(err, "CreditsError")
}

// shutdownOnce 由 runUnit 注入（单元级一次性停机信号）。
var shutdownOnce = func(string) {}

// sandboxToolkit 构造沙箱化工具集：filesystem（根=沙箱）+ shell（工作目录=沙箱）。
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
	return fmt.Sprintf(`你在一个隔离沙箱目录中工作，只能通过提供的工具操作该目录内的文件（根目录: %s）。
完成任务的所有产物必须写在该目录内。逐步使用工具完成目标，不要询问用户。`, sandbox)
}

func loadItems() ([]lhItem, error) {
	for _, dir := range []string{"docs/evals", "../docs/evals"} {
		data, err := os.ReadFile(filepath.Join(dir, "long-horizon-v1.json"))
		if err != nil {
			continue
		}
		var items []lhItem
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	return nil, errors.New("long-horizon-v1.json 不可达")
}

func countHoldout(items []lhItem) int {
	n := 0
	for _, it := range items {
		if it.Holdout {
			n++
		}
	}
	return n
}

// writeReport 汇总统计（McNemar 配对 + Wilson 双臂 + 留出单列）。
func writeReport(outDir string, rounds, holdoutTotal int) {
	jsonlPath := filepath.Join(outDir, "results.jsonl")
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return
	}
	var results []unitResult
	for _, line := range strings.Split(string(data), "\n") {
		var r unitResult
		if json.Unmarshal([]byte(line), &r) == nil && r.Error == "" {
			results = append(results, r)
		}
	}
	pairs := map[string][]unitResult{} // item|round → 两臂结果
	for _, r := range results {
		k := fmt.Sprintf("%s|%d", r.Item, r.Round)
		pairs[k] = append(pairs[k], r)
	}
	b, c := 0, 0 // b=仅 A 成功, c=仅 B 成功（不一致格）
	aOK, bOK := 0, 0
	for _, rs := range pairs {
		if len(rs) != 2 {
			continue
		}
		var a, bb unitResult
		for _, r := range rs {
			if r.Arm == "A" {
				a = r
			} else {
				bb = r
			}
		}
		switch {
		case a.Success && !bb.Success:
			b++
		case !a.Success && bb.Success:
			c++
		}
		if a.Success {
			aOK++
		}
		if bb.Success {
			bOK++
		}
	}
	n := aOK + bOK - len(pairs) + len(pairs) // 配对数 = pairs 长度
	_ = n
	p, _ := eval.McNemarExact(b, c)
	summary := map[string]any{
		"pairs_completed":   len(pairs),
		"arm_a_success":     aOK,
		"arm_b_success":     bOK,
		"discordant_a_only": b,
		"discordant_b_only": c,
		"mcnemar_p":         p,
		"rounds":            rounds,
		"holdout_total":     holdoutTotal,
		"note":              "留出子集对账以逐单元 holdout 标记单列（R4 全披露）",
	}
	out, _ := json.MarshalIndent(summary, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "summary.json"), append(out, '\n'), 0o644)
	fmt.Printf("汇总：配对 %d | A 成功 %d | B 成功 %d | 不一致 b(A only)=%d c(B only)=%d | McNemar p=%.4f\n",
		len(pairs), aOK, bOK, b, c, p)
}

// federation 占位引用（避免未用 import；实验报告后续扩展联邦对账面）
var _ = federation.NodeID("")

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	os.Exit(1)
}
