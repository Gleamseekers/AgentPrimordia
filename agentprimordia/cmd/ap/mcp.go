package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func runMCP(args []string) error {
	if len(args) == 0 {
		fmt.Print(`ap mcp — manage MCP servers

Usage:
  ap mcp <subcommand> [arguments]

Subcommands:
  list              list registered MCP servers
  add <name>        register a new MCP server
  remove <name>     remove MCP server
  start <name>      start MCP server
  stop <name>       stop MCP server
  test <name>       test MCP server connectivity
  tools <name>      list tools provided by MCP server

Examples:
  ap mcp add filesystem --command "npx" --args "@modelcontextprotocol/server-filesystem,/tmp"
  ap mcp list
  ap mcp test filesystem
`)
		return nil
	}

	subcmd := args[0]
	subargs := args[1:]

	switch subcmd {
	case "list":
		return mcpList()
	case "add":
		return mcpAdd(subargs)
	case "remove":
		return mcpRemove(subargs)
	case "start":
		return mcpStart(subargs)
	case "stop":
		return mcpStop(subargs)
	case "test":
		return mcpTest(subargs)
	case "tools":
		return mcpTools(subargs)
	default:
		return fmt.Errorf("unknown subcommand %q, run %s for help", subcmd, bold("ap mcp --help"))
	}
}

func mcpList() error {
	config := loadAPConfig()
	if config.MCP == nil || len(config.MCP.Servers) == 0 {
		fmt.Println("no MCP servers registered")
		fmt.Println()
		fmt.Println("use ap mcp add <name> to register a server")
		return nil
	}

	fmt.Printf("%-20s %-30s %-10s %-20s\n", "Name", "Command", "AutoStart", "Endpoint")
	fmt.Println(strings.Repeat("-", 85))
	for name, srv := range config.MCP.Servers {
		autoStart := "no"
		if srv.AutoStart {
			autoStart = "yes"
		}
		cmd := srv.Command
		if cmd == "" {
			cmd = "-"
		}
		endpoint := srv.BaseURL
		if endpoint == "" {
			endpoint = fmt.Sprintf("%s %s", srv.Command, strings.Join(srv.Args, " "))
		}
		fmt.Printf("%-20s %-30s %-10s %-20s\n", name, truncate(cmd, 28), autoStart, truncate(endpoint, 18))
	}
	return nil
}

func mcpAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name\nUsage: ap mcp add <name> --command <cmd> [--args ...] [--url <url>]")
	}

	name := args[0]
	var (
		command   string
		argsList  []string
		baseURL   string
		autoStart bool
		envVars   []string
	)

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--command", "-c":
			i++
			if i >= len(args) {
				return fmt.Errorf("--command requires a value")
			}
			command = args[i]
		case "--args", "-a":
			i++
			if i >= len(args) {
				return fmt.Errorf("--args requires a value")
			}
			argsList = strings.Split(args[i], ",")
		case "--url", "-u":
			i++
			if i >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			baseURL = args[i]
		case "--auto-start":
			autoStart = true
		case "--env", "-e":
			i++
			if i >= len(args) {
				return fmt.Errorf("--env requires KEY=VALUE")
			}
			envVars = strings.Split(args[i], ",")
		}
	}

	if command == "" && baseURL == "" {
		return fmt.Errorf("either --command or --url is required")
	}

	config := loadAPConfig()
	if config.MCP == nil {
		config.MCP = &mcpConfig{}
	}
	if config.MCP.Servers == nil {
		config.MCP.Servers = make(map[string]mcpServerConfig)
	}

	env := make(map[string]string)
	for _, ev := range envVars {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	config.MCP.Servers[name] = mcpServerConfig{
		Command:   command,
		Args:      argsList,
		BaseURL:   baseURL,
		AutoStart: autoStart,
		Env:       env,
	}

	if err := saveAPConfig(config); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	successf("MCP server %q registered", name)
	if autoStart {
		infof("auto-start: enabled")
	}
	return nil
}

func mcpRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name")
	}

	name := args[0]
	config := loadAPConfig()
	if config.MCP == nil || config.MCP.Servers == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}

	if _, ok := config.MCP.Servers[name]; !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	delete(config.MCP.Servers, name)
	if err := saveAPConfig(config); err != nil {
		return fmt.Errorf("save config failed: %w", err)
	}

	successf("MCP server %q removed", name)
	return nil
}

func mcpStart(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name")
	}

	name := args[0]
	config := loadAPConfig()
	if config.MCP == nil || config.MCP.Servers == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}
	srvCfg, ok := config.MCP.Servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	registry := ap.NewMCPRegistry()
	registry.Register(ap.MCPClientConfig{
		Name:    name,
		Command: srvCfg.Command,
		Args:    srvCfg.Args,
		BaseURL: srvCfg.BaseURL,
	})

	infof("starting MCP server %q ...", name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := registry.Start(ctx, name); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	successf("MCP server %q started", name)
	return nil
}

func mcpStop(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name")
	}

	name := args[0]
	config := loadAPConfig()
	if config.MCP == nil || config.MCP.Servers == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}

	srvCfg, ok := config.MCP.Servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	// 尝试通过 MCPRegistry.Stop 停止进程
	// 注意：由于 ap 是 CLI 工具，mcpStart 启动的进程的 registry 引用在进程退出时已丢失。
	// 此处重新创建 registry 并注册配置后尝试 Stop，对 URL 类型 server 会关闭客户端连接，
	// 对 command 类型 server 由于缺少进程句柄可能无法真正终止。
	registry := ap.NewMCPRegistry()
	registry.Register(ap.MCPClientConfig{
		Name:    name,
		Command: srvCfg.Command,
		Args:    srvCfg.Args,
		BaseURL: srvCfg.BaseURL,
	})
	stopErr := registry.Stop(name)

	// 无论 Stop 是否成功，都禁用 auto-start
	if srvCfg.AutoStart {
		srvCfg.AutoStart = false
		config.MCP.Servers[name] = srvCfg
		if err := saveAPConfig(config); err != nil {
			warnf("save config failed: %v", err)
		}
	}

	if stopErr != nil {
		// Stop 失败（通常是进程未在当前 registry 中运行）
		warnf("could not stop process for %q: %v", name, stopErr)
		if srvCfg.Command != "" {
			infof("auto-start has been disabled")
			infof("to kill the running process manually: taskkill /f /im %s (Windows) or pkill %s (Unix)", srvCfg.Command, srvCfg.Command)
		} else {
			infof("auto-start has been disabled for URL-based server")
		}
	} else {
		successf("MCP server %q stopped", name)
	}
	return nil
}

func mcpTest(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name")
	}

	name := args[0]
	config := loadAPConfig()
	if config.MCP == nil || config.MCP.Servers == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}
	srvCfg, ok := config.MCP.Servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	if srvCfg.BaseURL == "" {
		return fmt.Errorf("MCP server %q has no URL configured, cannot test", name)
	}

	client := ap.NewMCPClient(srvCfg.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	infof("testing MCP server %q connectivity...", name)
	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	tools := client.Tools()
	successf("connected, found %d tools:", len(tools))
	for _, t := range tools {
		fmt.Printf("  - %s: %s\n", t.Name, t.Description)
	}
	return nil
}

func mcpTools(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("please specify server name")
	}

	name := args[0]
	config := loadAPConfig()
	if config.MCP == nil || config.MCP.Servers == nil {
		return fmt.Errorf("MCP server %q not found", name)
	}
	srvCfg, ok := config.MCP.Servers[name]
	if !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}

	if srvCfg.BaseURL == "" {
		return fmt.Errorf("MCP server %q has no URL, please start it first", name)
	}

	client := ap.NewMCPClient(srvCfg.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	tools := client.Tools()
	if len(tools) == 0 {
		fmt.Println("no tools registered on this server")
		return nil
	}

	for _, t := range tools {
		fmt.Printf("Name: %s\n", t.Name)
		fmt.Printf("Description: %s\n", t.Description)
		if t.InputSchema != nil {
			schema, _ := json.MarshalIndent(t.InputSchema, "  ", "  ")
			fmt.Printf("Parameters: %s\n", string(schema))
		}
		fmt.Println()
	}
	return nil
}

// ===== Config types =====

type mcpServerConfig struct {
	Command   string            `json:"command" yaml:"command"`
	Args      []string          `json:"args,omitempty" yaml:"args,omitempty"`
	BaseURL   string            `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	AutoStart bool              `json:"auto_start,omitempty" yaml:"auto_start,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type mcpConfig struct {
	Servers map[string]mcpServerConfig `json:"servers" yaml:"servers"`
}

func truncate(s string, n int) string {
	if n <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}
