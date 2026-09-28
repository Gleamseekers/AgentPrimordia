package builtin

import (
	"fmt"

	"agentprimordia/internal/concurrency"
	"agentprimordia/internal/tools"
)

type ToolkitConfig struct {
	RootDir      string
	EnableFS     bool
	EnableShell  bool
	EnableWeb    bool
	EnableSearch bool
	EnableUtils  bool
	ScopePolicy  tools.ScopePolicy
	ScopeAgent   string
	FileLock     *concurrency.FileLockManager
}

func DefaultToolkit(cfg ToolkitConfig) (*tools.Registry, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("rootDir is required")
	}

	reg := tools.NewRegistry()

	if cfg.EnableFS {
		fs, err := NewFileSystem(cfg.RootDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create filesystem tool: %w", err)
		}
		if cfg.ScopePolicy != nil {
			fs.WithScopePolicy(cfg.ScopePolicy, cfg.ScopeAgent)
		}
		if cfg.FileLock != nil {
			fs.WithFileLock(cfg.FileLock)
		}
		if err := reg.Register(fs); err != nil {
			return nil, fmt.Errorf("failed to register filesystem: %w", err)
		}
	}

	if cfg.EnableShell {
		shell := NewShell()
		// 安全默认（2026-09-28 P0 修复）：shell 与 filesystem 工具一样
		// 禁锢在 RootDir 内——此前 shell 对命令参数中的路径零校验，
		// filesystem 的 rootDir jail 可被一句 "cat /etc/passwd" 废弃。
		shell.WithAllowedWorkdirs([]string{cfg.RootDir})
		if cfg.ScopePolicy != nil {
			shell.WithScopePolicy(cfg.ScopePolicy, cfg.ScopeAgent)
		}
		if err := reg.Register(shell); err != nil {
			return nil, fmt.Errorf("failed to register shell: %w", err)
		}
	}

	if cfg.EnableWeb {
		web := NewWeb()
		if err := reg.Register(web); err != nil {
			return nil, fmt.Errorf("failed to register web: %w", err)
		}
	}

	if cfg.EnableUtils {
		calc := NewCalculator()
		if err := reg.Register(calc); err != nil {
			return nil, fmt.Errorf("failed to register calculator: %w", err)
		}
		dt := NewDateTime()
		if err := reg.Register(dt); err != nil {
			return nil, fmt.Errorf("failed to register datetime: %w", err)
		}
	}

	return reg, nil
}

func MinimalToolkit(rootDir string) (*tools.Registry, error) {
	return DefaultToolkit(ToolkitConfig{
		RootDir:     rootDir,
		EnableFS:    true,
		EnableShell: true,
	})
}
