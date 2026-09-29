// tool_forge.go — 工具锻造装配根（INV-0 合规的唯一生产通道）。
//
// 背景：internal/tools/intelligence 的 RegisteringCreator 自 v7.4 起对
// agent 生成工具执行 fail-closed 门控（未验签不注册、宿主零执行），
// 但验签器与执行器需由组装根注入——本文件提供该生产装配：
//
//	┌────────────────────────────────────────────────────────┐
//	│ 缺口检测 → Creator 生成工件 → 验签门（ed25519 + 钉扎）  │
//	│   → 注册（0644 数据落盘）→ 执行（仅 wasm 沙箱）          │
//	└────────────────────────────────────────────────────────┘
//
// 安全语义（AGENTS.md §2.3 INV-0）：
//   - 未签名工件：拒绝注册；
//   - 签名公钥未钉扎：拒绝注册；
//   - 非 WASM 工件（如 shell 脚本）：沙箱加载失败即拒绝执行——
//     生成质量（WASM 化）属研究面（V7 §一），基础设施不降低门禁。
//
// 默认不启用：仅当用户显式传 --tool-forge 时装配（零配置路径行为为零）。
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence"
	"github.com/Gleamseekers/AgentPrimordia/wasm"
)

// ToolForge 工具锻造装配：验签门 + wasm 沙箱执行通道。
type ToolForge struct {
	creator *intelligence.RegisteringCreator
	adapter *wasm.WASMToolAdapter
	sandbox *wasm.Sandbox
	logger  *slog.Logger
}

// ToolForgeConfig 装配配置。
type ToolForgeConfig struct {
	// Registry 工具注册表（工件注册到此）。
	Registry *tools.Registry
	// Dir 工件落盘目录（<Dir>/.intel-tools/）。
	Dir string
	// PinnedKeys 钉扎的签名方公钥集合（ed25519 raw bytes；空集合 = 拒绝一切，
	// 与 fail-closed 语义一致——显式钉扎是启用前提）。
	PinnedKeys [][]byte
	// BaseCreator 基础生成器（缺口 → 工件）。
	BaseCreator intelligence.ToolCreator
	Logger      *slog.Logger
}

// NewToolForge 装配工具锻造链。
func NewToolForge(cfg ToolForgeConfig) (*ToolForge, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("tool forge: registry 不能为空")
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("tool forge: dir 不能为空")
	}
	if cfg.BaseCreator == nil {
		return nil, fmt.Errorf("tool forge: 基础生成器不能为空")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 钉扎公钥集合（指纹索引，供轮换窗口多键并存）。
	pinned := make(map[string]bool, len(cfg.PinnedKeys))
	for _, k := range cfg.PinnedKeys {
		if len(k) == 0 {
			return nil, fmt.Errorf("tool forge: 钉扎公钥不能为空")
		}
		pinned[string(k)] = true
	}

	// 验签门：ed25519 over SHA-256(Artifact)，公钥必须命中钉扎集合。
	verifier := &pinnedArtifactVerifier{pinned: pinned}

	// wasm 沙箱执行通道（唯一合法执行位置）。
	sandbox := wasm.NewSandbox(wasm.DefaultSandboxConfig())
	adapter := wasm.NewWASMToolAdapter(sandbox).WithLogger(logger)

	// 执行器：懒加载工件进沙箱后执行（加载失败即拒绝——非 WASM 工件
	// 在此被拦下，宿主零执行）。
	executor := func(ctx context.Context, art *intelligence.ToolArtifact, args json.RawMessage) (*tools.Result, error) {
		if _, ok := adapter.GetTool(art.Name); !ok {
			meta := wasm.ToolMetadata{
				Name:        art.Name,
				Description: art.Description,
				Parameters:  json.RawMessage(`{"type":"object"}`),
				ExecuteFunc: "tool_execute",
				Version:     "1.0.0",
				Signature:   art.Signature,
				PublicKey:   art.PublicKey,
			}
			if err := adapter.RegisterTool(ctx, meta, art.Artifact); err != nil {
				return tools.NewErrorResult(fmt.Sprintf("工件 %q 沙箱加载失败（非 WASM 或验签失败）: %v", art.Name, err)), err
			}
		}
		res, err := adapter.ExecuteTool(ctx, art.Name, args)
		if err != nil {
			return tools.NewErrorResult(fmt.Sprintf("工件 %q 沙箱执行失败: %v", art.Name, err)), err
		}
		if res.IsError {
			return tools.NewErrorResult(res.Content), nil
		}
		return tools.NewResult(res.Content), nil
	}

	creator := intelligence.NewRegisteringCreator(cfg.BaseCreator, cfg.Registry, cfg.Dir).
		WithVerifier(verifier).
		WithExecutor(executor)

	return &ToolForge{creator: creator, adapter: adapter, sandbox: sandbox, logger: logger}, nil
}

// Creator 返回门控后的生成器（供 ToolIntelligenceConfig.Creator 注入）。
func (f *ToolForge) Creator() intelligence.ToolCreator { return f.creator }

// ToolIntelligenceConfig 构造可注入 ReActConfig 的工具智能配置。
func (f *ToolForge) ToolIntelligenceConfig(profiler intelligence.ToolProfiler, detector intelligence.GapDetector) *agent.ToolIntelligenceConfig {
	return &agent.ToolIntelligenceConfig{
		Profiler: profiler,
		Detector: detector,
		Creator:  f.creator,
	}
}

// Close 关闭沙箱（释放 wazero 运行时）。
func (f *ToolForge) Close() error {
	if err := f.adapter.Close(); err != nil {
		return err
	}
	return f.sandbox.Close()
}

// pinnedArtifactVerifier 钉扎公钥的 ed25519 工件验签器。
type pinnedArtifactVerifier struct {
	pinned map[string]bool
}

// VerifyArtifact 验签（intelligence.ArtifactVerifier 实现）：
// 签名/公钥必填 → 公钥命中钉扎集合 → ed25519 验签 over SHA-256(工件)。
func (v *pinnedArtifactVerifier) VerifyArtifact(art *intelligence.ToolArtifact) error {
	if len(art.Signature) == 0 || len(art.PublicKey) == 0 {
		return fmt.Errorf("工件缺少签名/公钥（INV-0 签名前置）")
	}
	if !v.pinned[string(art.PublicKey)] {
		return fmt.Errorf("签名公钥未在钉扎集合中（指纹 %s）", wasm.KeyFingerprint(art.PublicKey))
	}
	digest := sha256.Sum256(art.Artifact)
	if err := wasm.VerifySignature(art.Artifact, art.Signature, art.PublicKey); err != nil {
		return fmt.Errorf("验签失败（sha256=%s）: %w", hex.EncodeToString(digest[:8]), err)
	}
	return nil
}

// signToolArtifact 为工件签名（生产侧供受信生成方使用；密钥管理归运维）。
// 返回 Signature/PublicKey 对，调用方填入 ToolArtifact。
func signToolArtifact(artifact []byte, privateKey ed25519.PrivateKey) (signature, publicKey []byte, err error) {
	return wasm.SignWASM(artifact, privateKey)
}

// forgeWorkspace 返回工件落盘目录（<dir>/.intel-tools 的父级）。
func forgeWorkspace(dir string) string { return filepath.Join(dir, ".intel-tools") }
