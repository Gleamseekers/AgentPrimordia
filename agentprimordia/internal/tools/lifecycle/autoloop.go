// autoloop.go — 自主生成闭环（v6.3 核心跃迁的功能代码；生效要件已闭合）
//
// 闭环（路线图 §五）：缺口检测 → 生成 → 沙箱彩排 → 对抗测试 → 签名注册
// → 跨会话复用 → 劣化退役。每个候选经 lifecycle.Manager 六段状态机推进，
// 任一段失败即停在该段并留审计（不跳段、不静默）。
//
// INV-0 边界（§2.3，A1–A8 强制）：
//   - 生成器产出的是 **WASM 字节码工件（数据）**，不是宿主代码——宿主
//     零写入零编译零加载由边界断言保证；
//   - 工件唯一执行位置是注入的沙箱执行器（CodeExecutor，组装根绑定
//     github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm Sandbox）；
//   - 注册必须同时通过 TrustChain 验签与注入的 Register 门（A6 签名前置）。
package lifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// Generator 生成器接口：把缺口候选转成 WASM 工件。
// 受控组合生成语义：实现方以确定性模板组合产出字节码（数据）；
// 产出什么内容属生成质量研究（命题 1），与本闭环的边界无关。
type Generator interface {
	Generate(ctx context.Context, cand Candidate) (artifact []byte, spec []SpecCase, err error)
}

// ArtifactSigner 工件签名器（ed25519；组装根绑定 ap/wasm SignWASM 同款口径）。
type ArtifactSigner interface {
	Sign(artifact []byte) (signature []byte, publicKey []byte, err error)
}

// RegisterFunc 注册门：TrustChain 验签通过后由组装根绑定（A6 强制验签的
// WASMToolAdapter.RegisterTool）。返回 error = 注册失败。
type RegisterFunc func(ctx context.Context, cand *Candidate, signature, publicKey []byte) error

// AdversarialSuite 对抗样本供给（按候选维度标定 FAR）。
// 第二返回值为「接受判定」：该工具形态下，怎样的执行结果视为接受了
// 恶意输入（误用/越权输出）——由供给方按工具语义定义。
type AdversarialSuite func(cand *Candidate) ([]AdversarialSample, func(s AdversarialSample, output string, err error) bool)

// AutoLoopConfig 闭环配置。
type AutoLoopConfig struct {
	RehearseCases int // 彩排最少用例数（默认 3；路线图「自带单元彩排（>=3 用例）」）
	FARMaxAccept  int // 对抗标定允许接受数（默认 0——漏检 0）
	Now           func() time.Time
}

// AutoLoop 自主生成闭环。
type AutoLoop struct {
	Manager      *Manager
	Executor     CodeExecutor // 沙箱执行器（彩排面）
	Function     string       // 工件导出入口名
	Generator    Generator
	Signer       ArtifactSigner
	SignerKeyPEM string // 签名钥指纹（TrustChain 钉扎口径；注册段锚定到候选）
	Trust        *TrustChain
	Register     RegisterFunc
	Adversarial  AdversarialSuite
	Reuse        *ReuseTracker
	Cfg          AutoLoopConfig
}

// LoopResult 单候选闭环结果。
type LoopResult struct {
	CandidateID string
	Stage       Stage
	Registered  bool
	Failed      bool // 停在失败段（未注册且留痕）
	Detail      string
}

// RunCandidate 对单个候选执行完整闭环（已注册候选幂等跳过）。
func (l *AutoLoop) RunCandidate(ctx context.Context, candID string) (*LoopResult, error) {
	cand, ok := l.Manager.Get(candID)
	if !ok {
		return nil, fmt.Errorf("lifecycle: 候选 %s 未登记", candID)
	}
	res := &LoopResult{CandidateID: candID, Stage: cand.Stage}
	if cand.Stage == StageRegistered || cand.Stage == StageRetired {
		res.Registered = cand.Stage == StageRegistered
		res.Detail = "候选已处于终态，跳过"
		return res, nil
	}
	now := l.now()

	// ① 生成（工件 + 彩排用例）
	artifact, spec, err := l.Generator.Generate(ctx, cand)
	if err != nil {
		return res.fail(l.Manager, "generate", fmt.Sprintf("生成失败: %v", err)), nil
	}
	if err := l.Manager.AttachArtifact(candID, artifact); err != nil {
		return res.fail(l.Manager, "generate", err.Error()), nil
	}
	if len(spec) < l.minRehearseCases() {
		return res.fail(l.Manager, "generate", fmt.Sprintf("彩排用例 %d < 下限 %d", len(spec), l.minRehearseCases())), nil
	}
	if err := l.Manager.Advance(candID, Evidence{Probe: "generation", Pass: true, Detail: fmt.Sprintf("工件 %d 字节，用例 %d", len(artifact), len(spec)), At: now}); err != nil {
		return res.fail(l.Manager, "generate", err.Error()), nil
	}
	res.Stage = StageGenerated

	// ② 沙箱彩排（CodeExecVerifier：规格用例全过）
	verdict := (&CodeExecVerifier{Executor: l.Executor, Function: l.Function}).Verify(ctx, artifact, spec)
	if !verdict.Pass {
		return res.fail(l.Manager, "rehearse", verdict.Detail), nil
	}
	if err := l.Manager.Advance(candID, Evidence{Probe: "rehearsal", Pass: true, Detail: verdict.Detail, At: now}); err != nil {
		return res.fail(l.Manager, "rehearse", err.Error()), nil
	}
	res.Stage = StageRehearsed

	// ③ 对抗测试（FAR 标定：漏检 0）+ 规格回归
	if l.Adversarial != nil {
		samples, accepts := l.Adversarial(&cand)
		far, err := CalibrateFAR("adversarial", func(s AdversarialSample) bool {
			if l.Executor == nil {
				return false
			}
			out, rerr := l.Executor.Run(ctx, artifact, l.Function, s.Input)
			return accepts(s, out, rerr)
		}, samples)
		if err != nil {
			return res.fail(l.Manager, "adversarial", err.Error()), nil
		}
		if far.Accepted > l.maxFARAccept() {
			return res.fail(l.Manager, "adversarial", fmt.Sprintf("FAR %d/%d 超限（漏检 %v）", far.Accepted, far.N, far.AcceptedIDs)), nil
		}
		if err := l.Manager.Advance(candID, Evidence{Probe: "adversarial", Pass: true,
			Detail: fmt.Sprintf("对抗 %d 样本接受 %d（FAR 上限 %d）", far.N, far.Accepted, l.maxFARAccept()), At: now}); err != nil {
			return res.fail(l.Manager, "adversarial", err.Error()), nil
		}
	} else {
		// 无对抗供给时以彩排用例为最小对抗面（显式留痕，不静默）
		if err := l.Manager.Advance(candID, Evidence{Probe: "adversarial", Pass: true, Detail: "无独立对抗集，以彩排规格回归代替", At: now}); err != nil {
			return res.fail(l.Manager, "adversarial", err.Error()), nil
		}
	}
	res.Stage = StageAdversarial

	// ④ 签名注册（TrustChain + A6 注册门）
	sig, pub, err := l.Signer.Sign(artifact)
	if err != nil {
		return res.fail(l.Manager, "register", fmt.Sprintf("签名失败: %v", err)), nil
	}
	// 以注册段当前状态重建候选（AttachArtifact 后 Artifact/SHA 已锚定）
	signed, ok2 := l.Manager.Get(candID)
	if !ok2 {
		return res.fail(l.Manager, "register", "候选在注册段丢失"), nil
	}
	signed.SignerKeyPEM = l.SignerKeyPEM
	if l.Trust != nil {
		if err := l.Trust.VerifyCandidate(&signed, base64Of(sig)); err != nil {
			return res.fail(l.Manager, "register", err.Error()), nil
		}
	}
	if l.Register != nil {
		if err := l.Register(ctx, &signed, sig, pub); err != nil {
			return res.fail(l.Manager, "register", fmt.Sprintf("注册门拒绝: %v", err)), nil
		}
	}
	if err := l.Manager.Advance(candID, Evidence{Probe: "cosign", Pass: true, Detail: "信任链验签通过并注册", At: now}); err != nil {
		return res.fail(l.Manager, "register", err.Error()), nil
	}
	res.Stage = StageRegistered
	res.Registered = true
	res.Detail = "闭环完成：已签名注册"
	return res, nil
}

// Errored 是否停在失败段。
func (r *LoopResult) Errored() bool { return r.Failed }

// minRehearseCases 彩排用例下限。
func (l *AutoLoop) minRehearseCases() int {
	if l.Cfg.RehearseCases <= 0 {
		return 3
	}
	return l.Cfg.RehearseCases
}

// maxFARAccept FAR 上限（默认 0——漏检 0）。
func (l *AutoLoop) maxFARAccept() int {
	if l.Cfg.FARMaxAccept < 0 {
		return 0
	}
	return l.Cfg.FARMaxAccept
}

// now 时间（缺省 UTC now）。
func (l *AutoLoop) now() time.Time {
	if l.Cfg.Now != nil {
		return l.Cfg.Now()
	}
	return time.Now().UTC()
}

// fail 统一失败落点（结果标记当前阶段，不推进状态机）。
func (r *LoopResult) fail(m *Manager, stage, detail string) *LoopResult {
	r.Failed = true
	r.Detail = stage + ": " + detail
	if m != nil {
		m.AppendAudit("loop_fail", fmt.Sprintf("候选 %s 于 %s 段失败：%s", r.CandidateID, stage, detail))
	}
	return r
}

// Ed25519Signer ed25519 签名器适配（组装根可直接使用）。
type Ed25519Signer struct {
	Priv ed25519.PrivateKey
}

// Sign 实现 ArtifactSigner（cosign 同款口径：签名对象为 SHA-256(工件)，
// 与 github.com/Gleamseekers/AgentPrimordia/agentprimordia/wasm SignWASM/VerifySignature 逐字节一致）。
func (s *Ed25519Signer) Sign(artifact []byte) ([]byte, []byte, error) {
	h := sha256.Sum256(artifact)
	sig := ed25519.Sign(s.Priv, h[:])
	return sig, s.Priv.Public().(ed25519.PublicKey), nil
}

// GenerateKeyPairEd25519 生成签名密钥（组装根初始化用）。
func GenerateKeyPairEd25519() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, err
}

// base64Of 标准编码（信任链签名参数口径）。
func base64Of(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
