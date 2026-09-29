// soak-v64 — v6.4 命题 1 压缩口径真机 soak 驱动器（2026-09-01 维护者裁定）。
//
// 判据（docs/V7路线图.md §五裁定记录）：真机常驻 ≥72h + 加速崩溃注入 ≥75 次
// 全自愈（点估计 ≥99%、Wilson 下界 ≥95%）+ 小时级资源遥测全量披露。
//
// 代谢形态披露：echo-synthetic（无 LLM）——本门测的是常驻运行时工程
// （自唤醒/预算/守护自愈/审计链/资源斜率），不测模型质量；代谢为合成执行面。
// 机制在 internal/agent/live/soak.go；本文件只做真实时钟装配与报告落盘。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/live"
)

func main() {
	duration := flag.Duration("duration", 72*time.Hour, "目标常驻时长")
	injectEvery := flag.Duration("inject-every", 45*time.Minute, "崩溃注入间隔（72h/45min → 96 次 ≥75）")
	wakeEvery := flag.Duration("wake-every", 10*time.Minute, "定时代谢唤醒间隔")
	idleEvery := flag.Duration("idle-every", time.Hour, "闲时作业冷却间隔")
	telemetryEvery := flag.Duration("telemetry-every", time.Hour, "资源遥测间隔（兼报告落盘节奏）")
	step := flag.Duration("step", time.Second, "Step 推进粒度")
	out := flag.String("out", "bench/results/v64-soak", "报告输出目录")
	statePath := flag.String("state", "", "跨重启累计状态文件路径（空 = <out>/soak-state.json；累计在线口径，2026-09-03 修订）")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	if *statePath == "" {
		*statePath = filepath.Join(*out, "soak-state.json")
	}
	reportPath := filepath.Join(*out, "soak-report.json")

	// 装配：合成代谢面 + 混沌注入包装 + 真实时钟 + 不限预算（护栏语义仍由
	// 预算记账路径覆盖；上限 0 = 不启用）
	chaos := live.NewChaosRunner(live.RunnerFunc(func(task live.TaskSpec) (string, int, error) {
		return "echo:" + task.Input, 1, nil
	}))
	clock := live.RealClock{}
	rt := live.NewRuntime(chaos, live.NewWaker(clock, 0), clock, &live.Budget{})
	rt.RegisterIdleJob(live.IdleJob{
		Name:     "soak-metabolism",
		Priority: 10,
		Interval: *idleEvery,
		Run: func(now time.Time) (string, error) {
			return "闲时代谢心跳 " + now.UTC().Format(time.RFC3339), nil
		},
	})
	soak := live.NewSoak(rt, chaos, clock, live.SoakConfig{
		TargetDuration: *duration,
		InjectEvery:    *injectEvery,
		WakeEvery:      *wakeEvery,
		TelemetryEvery: *telemetryEvery,
		StateDir:       *out,
		StatePath:      *statePath,
		Metabolism:     "echo-synthetic（无 LLM：长活门测运行时工程，不测模型质量）",
	})

	if st, err := os.Stat(*statePath); err == nil {
		fmt.Printf("soak 启动（续计）：目标累计 %v / 注入每 %v / 状态文件 %s（%s）\n", *duration, *injectEvery, *statePath, st.ModTime().Format("01-02 15:04"))
	} else {
		fmt.Printf("soak 启动：目标累计 %v / 注入每 %v / 遥测每 %v / 输出 %s\n", *duration, *injectEvery, *telemetryEvery, *out)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	stepTick := time.NewTicker(*step)
	telemTick := time.NewTicker(*telemetryEvery)
	defer stepTick.Stop()
	defer telemTick.Stop()

	idleTick := time.NewTicker(*idleEvery)
	defer idleTick.Stop()

	interrupted := false
	for !interrupted {
		select {
		case <-sigs:
			interrupted = true
		case <-idleTick.C:
			rt.IdleStep()
		case <-telemTick.C:
			rep := soak.Report()
			if err := live.WriteSoakReport(reportPath, rep); err != nil {
				fatal(err)
			}
			fmt.Printf("[%s] elapsed %.1fh 注入 %d 自愈 %d 失败 %d 遥测 %d 样本\n",
				time.Now().UTC().Format("01-02 15:04"), rep.ElapsedSec/3600,
				rep.Injections, rep.Healed, rep.Failures, len(rep.Samples))
		case <-stepTick.C:
			if soak.Step(time.Now()) {
				rep := soak.Report()
				if err := live.WriteSoakReport(reportPath, rep); err != nil {
					fatal(err)
				}
				printVerdict(rep)
				if !rep.Verdict.Pass {
					os.Exit(1)
				}
				return
			}
		}
	}

	// 信号中断：如实落盘累计状态与部分证据（Interrupted 标记），判定不通过
	_ = soak.Save(time.Now())
	rep := soak.Report()
	rep.Interrupted = true
	if err := live.WriteSoakReport(reportPath, rep); err != nil {
		fatal(err)
	}
	printVerdict(rep)
	os.Exit(2)
}

func printVerdict(rep live.SoakReport) {
	fmt.Printf("soak 结束：elapsed %.2fh 注入 %d 自愈 %d 失败 %d Wilson下界 %.4f Pass=%v\n",
		rep.ElapsedSec/3600, rep.Injections, rep.Healed, rep.Failures, rep.HealWilsonLB95, rep.Verdict.Pass)
	for _, n := range rep.Verdict.Notes {
		fmt.Println("  - " + n)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "soak-v64:", err)
	os.Exit(1)
}
