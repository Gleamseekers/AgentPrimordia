package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type apConfig struct {
	Name     string        `json:"name" yaml:"name"`
	Template string        `json:"template" yaml:"template"`
	LLM      *llmConfig    `json:"llm,omitempty" yaml:"llm,omitempty"`
	Memory   *memoryConfig `json:"memory,omitempty" yaml:"memory,omitempty"`
	Agent    *agentConfig  `json:"agent,omitempty" yaml:"agent,omitempty"`
	MCP      *mcpConfig    `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Plugins  []string      `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

type llmConfig struct {
	Provider string `json:"provider" yaml:"provider"`
	Model    string `json:"model" yaml:"model"`
	APIKey   string `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
}

type memoryConfig struct {
	Backend string `json:"backend" yaml:"backend"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
}

type agentConfig struct {
	MaxTurns     int    `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`
}

// loadAPConfig 从当前目录读取 .ap.yaml 或 .ap.json 配置
func loadAPConfig() *apConfig {
	dir, err := findProjectDir()
	if err != nil {
		return &apConfig{}
	}
	return loadAPConfigFromDir(dir)
}

// loadAPConfigFromDir 从指定目录读取 .ap.yaml 或 .ap.json 配置
// 优化（perf-v3）：当调用方已有项目目录时，避免冗余的 findProjectDir() 调用
func loadAPConfigFromDir(dir string) *apConfig {
	// 优先读取 .ap.yaml（主要配置格式）
	yamlPath := filepath.Join(dir, ".ap.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		var cfg apConfig
		if unmarshalErr := yaml.Unmarshal(data, &cfg); unmarshalErr != nil {
			log.Printf("warning: failed to parse .ap.yaml: %v", unmarshalErr)
		} else {
			// 验证配置合法性
			for _, e := range cfg.Validate() {
				log.Printf("warning: .ap.yaml: %s", e)
			}
			return &cfg
		}
	}

	// 回退读取 .ap.json（兼容旧格式）
	jsonPath := filepath.Join(dir, ".ap.json")
	if data, err := os.ReadFile(jsonPath); err == nil {
		var cfg apConfig
		if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
			log.Printf("warning: failed to parse .ap.json: %v", unmarshalErr)
		} else {
			return &cfg
		}
	}

	return &apConfig{}
}

// saveAPConfig 保存配置到 .ap.yaml
func saveAPConfig(config *apConfig) error {
	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("serialize config failed: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, ".ap.yaml"), data, 0o644)
}

// Validate 验证 .ap.yaml 配置合法性，返回所有错误（如有）。
func (c *apConfig) Validate() []string {
	var errs []string

	if c.Name == "" {
		errs = append(errs, "name cannot be empty")
	}

	if c.LLM != nil {
		supportedProviders := map[string]bool{
			"openai": true, "anthropic": true, "gemini": true,
			"ollama": true, "azure": true, "qwen": true,
			"deepseek": true, "glm": true, "mistral": true,
			"cohere": true,
		}
		if c.LLM.Provider != "" && !supportedProviders[strings.ToLower(c.LLM.Provider)] {
			errs = append(errs, fmt.Sprintf("unsupported llm.provider %q, supported: %s",
				c.LLM.Provider, strings.Join(providerList(), ", ")))
		}
	}

	if c.Agent != nil {
		if c.Agent.MaxTurns < 0 {
			errs = append(errs, "agent.max_turns cannot be negative")
		}
		if c.Agent.MaxTurns > 1000 {
			errs = append(errs, "agent.max_turns exceeds maximum 1000")
		}
	}

	if c.Memory != nil {
		backends := map[string]bool{"sqlite": true, "memory": true}
		if c.Memory.Backend != "" && !backends[c.Memory.Backend] {
			errs = append(errs, fmt.Sprintf("unsupported memory.backend %q, supported: sqlite, memory", c.Memory.Backend))
		}
	}

	return errs
}

func providerList() []string {
	return []string{"openai", "anthropic", "gemini", "ollama", "azure", "qwen", "deepseek", "glm", "mistral", "cohere"}
}
