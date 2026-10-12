// a2a-handoff — A2A 开放协议跨进程端到端委托 Demo（v7.7 楔子）。
//
// 场景：两个独立进程经开放 Agent2Agent 协议完成
// 「发现 Agent Card → 委托任务 → 轮询至终态 → 取回结果 → 取消」全链路：
//
//   - server 进程：ap Agent 经 OpenInteropServer 暴露开放协议端点
//     （/.well-known/agent.json + /a2a/v1 JSON-RPC）。Demo 模式：未设
//     AP_LLM_API_KEY 时使用关键词匹配 provider，零凭据可跑；
//   - client 进程：标准开放协议客户端（OpenInteropClient），经 HTTP
//     发现 server、委托任务、轮询状态、取回结果。
//
// 运行方式（一条命令，任意工作目录）：
//
//	./run.sh
//
// 或手动分两步（确认是两个独立进程）：
//
//	go run ./agentprimordia/ecosystem/examples/a2a-handoff server   # 终端 1
//	go run ./agentprimordia/ecosystem/examples/a2a-handoff client   # 终端 2
//
// 已知缺口（诚实登记，2026-10-12 核查）：A2A 任务级断点恢复不在当前协议面内——
// server 任务表是进程内内存 map（internal/agent/a2a/interop_server.go 的
// tasks 字段），杀掉 server 重启后原任务不可查（实证：重启后 GetTask 返回
// "Task not found"）。本 demo 因此不含「杀 server 重启 → 任务状态可查」情节，
// 详见同目录 README.md 与 docs/concepts/multi-agent-interop.md。
package main

import (
	"fmt"
	"os"
)

const usage = `Usage: a2a-handoff <subcommand>

Subcommands:
  server   启动开放协议 server 进程（Demo 模式，零凭据可跑）
  client   启动 client 进程（发现 → 委托 → 轮询 → 取回结果 → 取消）
  report   离线生成协议符合性报告（JSON，供 bench/results 入库）

Environment:
  AP_A2A_HANDOFF_ADDR          监听/连接地址（默认 127.0.0.1:9797；:0 = 随机端口）
  AP_A2A_HANDOFF_EXEC_DELAY_MS server 模拟任务处理耗时（默认 400，0 = 关闭，便于观察轮询）
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = runServer()
	case "client":
		err = runClient()
	case "report":
		err = runReport()
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "a2a-handoff: unknown subcommand %q（见 -h）\n", os.Args[1])
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "a2a-handoff: %v\n", err)
		os.Exit(1)
	}
}
