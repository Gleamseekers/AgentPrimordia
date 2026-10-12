package main

import (
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
)

// Version 由发布流水线经 ldflags 注入（release.yml 注入去 v tag 的语义化版本，
// ci.yml 注入带 v 的 ref_name）。未经 ldflags 的构建（go install module@version、
// go build）回填为 "dev"——此时尝试从 build info 读取模块版本，让
// `go install .../cmd/ap@v7.5.1` 的用户看到真实版本而不是 dev。
var Version = "dev"

func init() {
	Version = resolveVersion(Version, debug.ReadBuildInfo)
}

// resolveVersion 决定对外展示的版本号：ldflags 注入值优先；否则取 build info
// 的模块版本——但仅接受干净语义化版本（go install module@vX.Y.Z 的场景）；
// 本地构建的伪版本（vX.Y.Z-0.<ts>-<commit>[+dirty]）与 (devel) 一律保持 dev，
// 避免把不可复现的构建标识当成版本号展示。
// readBuildInfo 为依赖注入点（测试替身），nil 表示无 build info。
func resolveVersion(injected string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if injected != "dev" {
		return injected
	}
	bi, ok := readBuildInfo()
	if !ok || bi == nil {
		return injected
	}
	if cleanSemverRe.MatchString(bi.Main.Version) {
		return bi.Main.Version
	}
	return injected
}

// cleanSemverRe 匹配干净语义化版本：vX.Y.Z 或 vX.Y.Z-<prerelease>，其中
// prerelease 段不含点——据此排除 go build 的伪版本（vX.Y.Z-0.<14 位时间戳>-<commit>[+dirty]，
// 其时间戳段必含点）。ldflags 注入的真实版本不受此限（优先返回）。
var cleanSemverRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z-]+)?$`)

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
  migrate      rewrite module import paths for the next major (v7 -> v8)
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
	case "migrate":
		err = runMigrate(args)
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
