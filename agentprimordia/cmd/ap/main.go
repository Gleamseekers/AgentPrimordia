package main

import (
	"fmt"
	"os"
)

var Version = "dev"

const (
	usage = `AgentPrimordia (ap) — Go Agent Framework CLI

Usage:
  ap <command> [arguments]

Commands:
  start        create and run an agent in one step (recommended for beginners)
  init         create a new agent project
  run          build and run the current project
  debug        start debug server
  loop         ReAct loop engineering (trace/inspect/resume)
  test         run eval test suite
  config       manage configuration
  mcp          manage MCP servers
  plugin       manage plugins
  cluster      manage cluster (init/join/status/leave/scale)
  marketplace  manage agent templates
  autonomy     long-horizon autonomous goal execution
  skill        manage evolved skills (list/add/remove/verify)
  profile      display agent growth profile
  live            常驻长活模式（自唤醒/闲时自调度/崩溃自愈）
  a2a          A2A protocol interop (interop-check)
  realtime     realtime multimodal session (voice)
  create-edge-agent  create an Edge Agent project
  doctor       health check
  completion   generate shell completion scripts
  version      show version

Run "ap <command> --help" for subcommand details.
`
)

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "start":
		err = runStart(args)
	case "init":
		err = runInit(args)
	case "run":
		err = runRun(args)
	case "debug":
		err = runDebug(args)
	case "loop":
		err = runLoop(args)
	case "test":
		err = runTest(args)
	case "config":
		err = runConfig(args)
	case "mcp":
		err = runMCP(args)
	case "plugin":
		err = runPlugin(args)
	case "cluster":
		err = runCluster(args)
	case "marketplace":
		err = runMarketplace(args)
	case "autonomy":
		err = runAutonomy(args)
	case "live":
		err = runLive(args)
	case "skill":
		err = runSkill(args)
	case "profile":
		err = runProfile(args)
	case "a2a":
		err = runA2A(args)
	case "realtime":
		err = runRealtime(args)
	case "create-edge-agent":
		err = runCreateEdgeAgent(args)
	case "doctor":
		err = runDoctor(args)
	case "completion":
		err = runCompletion(args)
	case "version", "-v", "--version":
		fmt.Printf("AgentPrimordia CLI %s\n", Version)
	case "--help", "-h", "help":
		fmt.Print(usage)
	default:
		errorf("unknown command %q, run %s to see available commands", cmd, bold("ap --help"))
		os.Exit(1)
	}

	if err != nil {
		errorf("%v", err)
		os.Exit(1)
	}
}
