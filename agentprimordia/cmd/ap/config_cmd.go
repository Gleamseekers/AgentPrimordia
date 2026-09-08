package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func runConfig(args []string) error {
	if len(args) == 0 {
		printConfigHelp()
		return nil
	}

	subcmd := args[0]
	switch subcmd {
	case "validate":
		return runConfigValidate(args[1:])
	case "set":
		return runConfigSet(args[1:])
	case "show":
		return runConfigShow(args[1:])
	case "--help", "-h", "help":
		printConfigHelp()
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q, run %s for help", subcmd, bold("ap config --help"))
	}
}

func runConfigValidate(args []string) error {
	configPath := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--file", "-f":
			i++
			if i >= len(args) {
				return fmt.Errorf("--file requires a path argument")
			}
			configPath = args[i]
		case "--help", "-h":
			fmt.Print(`ap config validate — validate .ap.yaml configuration

Usage:
  ap config validate [--file PATH]

Options:
  --file, -f PATH    specify config file path (default: .ap.yaml in project root)

Examples:
  ap config validate
  ap config validate --file /path/to/.ap.yaml
`)
			return nil
		}
	}

	// Determine config file path
	if configPath == "" {
		dir, err := findProjectDir()
		if err != nil {
			return fmt.Errorf("could not find project directory: %w", err)
		}
		configPath = filepath.Join(dir, ".ap.yaml")
	}

	// Check file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			// Try .ap.json as fallback
			jsonPath := strings.TrimSuffix(configPath, ".yaml") + ".json"
			if _, jsonErr := os.Stat(jsonPath); jsonErr == nil {
				configPath = jsonPath
			} else {
				return fmt.Errorf("config file not found: %s", configPath)
			}
		} else {
			return fmt.Errorf("cannot access config file: %w", err)
		}
	}

	// Read and parse
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg apConfig
	if strings.HasSuffix(configPath, ".json") {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse JSON config: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse YAML config: %w", err)
		}
	}

	// Run validation
	errs := cfg.Validate()
	if len(errs) == 0 {
		successf("Configuration is valid: %s", configPath)

		// Print summary
		if cfg.Name != "" {
			fmt.Printf("  name:     %s\n", cfg.Name)
		}
		if cfg.LLM != nil {
			fmt.Printf("  llm:      provider=%s, model=%s\n", cfg.LLM.Provider, cfg.LLM.Model)
		}
		if cfg.Agent != nil {
			fmt.Printf("  agent:    max_turns=%d\n", cfg.Agent.MaxTurns)
		}
		if cfg.Memory != nil {
			fmt.Printf("  memory:   backend=%s\n", cfg.Memory.Backend)
		}
		if len(cfg.Plugins) > 0 {
			fmt.Printf("  plugins:  %s\n", strings.Join(cfg.Plugins, ", "))
		}
		if cfg.MCP != nil && len(cfg.MCP.Servers) > 0 {
			fmt.Printf("  mcp:      %d server(s)\n", len(cfg.MCP.Servers))
		}
		return nil
	}

	// Report errors
	errorf("Configuration has %d error(s):", len(errs))
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "  • %s\n", e)
	}
	return fmt.Errorf("configuration validation failed with %d error(s)", len(errs))
}

func runConfigSet(args []string) error {
	if len(args) == 0 {
		fmt.Print(`ap config set — set a configuration value

Usage:
  ap config set <key> [value]

Keys:
  api-key        LLM API key
  provider       LLM provider (openai/gemini/qwen/ollama/deepseek/glm/azure/anthropic)
  model          model name (e.g. gpt-4o, gemini-pro, qwen-max)
  base-url       API base URL
  max-turns      max agent turns
  system-prompt  agent system prompt

Examples:
  ap config set api-key
  ap config set provider openai
  ap config set model gpt-4o
`)
		return nil
	}

	key := args[0]
	var value string
	if len(args) > 1 {
		value = args[1]
	}

	cfg := loadAPConfig()
	if cfg.LLM == nil {
		cfg.LLM = &llmConfig{}
	}

	switch key {
	case "api-key":
		if value == "" {
			fmt.Print("Enter API key: ")
			fmt.Scanln(&value)
		}
		if value == "" {
			return fmt.Errorf("API key cannot be empty")
		}
		cfg.LLM.APIKey = value
		successf("API key 已设置")

	case "provider":
		if value == "" {
			fmt.Print("Enter provider (openai/gemini/qwen/ollama/deepseek/glm): ")
			fmt.Scanln(&value)
		}
		cfg.LLM.Provider = value
		successf("Provider 已设置为 %q", value)

	case "model":
		if value == "" {
			fmt.Print("Enter model name: ")
			fmt.Scanln(&value)
		}
		cfg.LLM.Model = value
		successf("Model 已设置为 %q", value)

	case "base-url":
		if value == "" {
			fmt.Print("Enter base URL: ")
			fmt.Scanln(&value)
		}
		cfg.LLM.BaseURL = value
		successf("Base URL 已设置")

	case "max-turns":
		if value == "" {
			fmt.Print("Enter max turns: ")
			fmt.Scanln(&value)
		}
		if cfg.Agent == nil {
			cfg.Agent = &agentConfig{}
		}
		n := 20
		fmt.Sscanf(value, "%d", &n)
		cfg.Agent.MaxTurns = n
		successf("Max turns 已设置为 %d", n)

	case "system-prompt":
		if value == "" {
			fmt.Print("Enter system prompt: ")
			fmt.Scanln(&value)
		}
		if cfg.Agent == nil {
			cfg.Agent = &agentConfig{}
		}
		cfg.Agent.SystemPrompt = value
		successf("System prompt 已设置")

	default:
		return fmt.Errorf("unknown config key %q, run %s for help", key, bold("ap config set"))
	}

	return saveAPConfig(cfg)
}

func runConfigShow(args []string) error {
	cfg := loadAPConfig()

	fmt.Println("Current configuration:")
	fmt.Println()
	if cfg.Name != "" {
		fmt.Printf("  name:     %s\n", cfg.Name)
	}
	if cfg.Template != "" {
		fmt.Printf("  template: %s\n", cfg.Template)
	}
	if cfg.LLM != nil {
		fmt.Println("  llm:")
		if cfg.LLM.Provider != "" {
			fmt.Printf("    provider: %s\n", cfg.LLM.Provider)
		}
		if cfg.LLM.Model != "" {
			fmt.Printf("    model:    %s\n", cfg.LLM.Model)
		}
		if cfg.LLM.APIKey != "" {
			masked := cfg.LLM.APIKey
			if len(masked) > 8 {
				masked = masked[:4] + "****" + masked[len(masked)-4:]
			} else {
				masked = "****"
			}
			fmt.Printf("    api_key:  %s\n", masked)
		}
		if cfg.LLM.BaseURL != "" {
			fmt.Printf("    base_url: %s\n", cfg.LLM.BaseURL)
		}
	}
	if cfg.Memory != nil {
		fmt.Println("  memory:")
		fmt.Printf("    backend: %s\n", cfg.Memory.Backend)
		if cfg.Memory.Path != "" {
			fmt.Printf("    path:    %s\n", cfg.Memory.Path)
		}
	}
	if cfg.Agent != nil {
		fmt.Println("  agent:")
		if cfg.Agent.MaxTurns > 0 {
			fmt.Printf("    max_turns:     %d\n", cfg.Agent.MaxTurns)
		}
		if cfg.Agent.SystemPrompt != "" {
			fmt.Printf("    system_prompt: %s\n", cfg.Agent.SystemPrompt)
		}
	}
	if len(cfg.Plugins) > 0 {
		fmt.Printf("  plugins:  %s\n", strings.Join(cfg.Plugins, ", "))
	}
	if cfg.MCP != nil && len(cfg.MCP.Servers) > 0 {
		fmt.Printf("  mcp:      %d server(s)\n", len(cfg.MCP.Servers))
	}
	return nil
}

func printConfigHelp() {
	fmt.Print(`ap config — manage agent configuration

Usage:
  ap config <subcommand> [options]

Subcommands:
  validate       validate .ap.yaml configuration file
  set            set a configuration value
  show           display current configuration

Run "ap config <subcommand> --help" for details.
`)
}
