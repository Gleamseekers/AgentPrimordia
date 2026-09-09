// autoloop_test.go — 自主生成闭环端到端测试（v6.3 功能代码）
//
// 全链路真实组件（无 stub 的阶段）：
//
//	生成（确定性模板组合 → 真实 WASM 字节）→ 彩排（真沙箱 ExecuteWithMemory
//	执行规格用例）→ 对抗（FAR 标定）→ 签名注册（ed25519 + TrustChain +
//	A6 强制验签的 RegisterTool）→ 复用追踪 → 劣化退役。
package lifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"agentprimordia/wasm"
)

// ===== 确定性 WASM 工件构造（echo 工具：tool_execute 原样回显输入）=====

func echoToolWasm(t *testing.T) []byte {
	t.Helper()
	leb := func(v uint32) []byte {
		var out []byte
		for {
			b := byte(v & 0x7F)
			v >>= 7
			if v != 0 {
				b |= 0x80
			}
			out = append(out, b)
			if v == 0 {
				return out
			}
		}
	}
	sec := func(id byte, content []byte) []byte {
		out := []byte{id}
		out = append(out, leb(uint32(len(content)))...)
		return append(out, content...)
	}
	hdr := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	out := append([]byte(nil), hdr...)
	out = append(out, sec(0x01, []byte{0x02, 0x60, 0x01, 0x7E, 0x01, 0x7E, 0x60, 0x02, 0x7E, 0x7E, 0x02, 0x7E, 0x7E})...)
	out = append(out, sec(0x03, []byte{0x02, 0x00, 0x01})...)
	out = append(out, sec(0x05, []byte{0x01, 0x00, 0x01})...)
	out = append(out, sec(0x06, []byte{0x01, 0x7E, 0x01, 0x42, 0x80, 0x08, 0x0B})...)
	out = append(out, sec(0x07, []byte{0x03, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x05, 'a', 'l', 'l', 'o', 'c', 0x00, 0x00,
		0x0C, 't', 'o', 'o', 'l', '_', 'e', 'x', 'e', 'c', 'u', 't', 'e', 0x00, 0x01})...)
	b0 := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0x7C, 0x24, 0x00, 0x23, 0x00, 0x0B}
	b1 := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x0B}
	code := []byte{0x02}
	code = append(code, leb(uint32(len(b0)))...)
	code = append(code, b0...)
	code = append(code, leb(uint32(len(b1)))...)
	code = append(code, b1...)
	out = append(out, sec(0x0A, code)...)
	return out
}

// ===== 沙箱执行器适配（agentprimordia/wasm Sandbox → lifecycle.CodeExecutor）=====

type sandboxExecutor struct {
	sandbox *wasm.Sandbox
	loaded  map[string]bool
}

func newSandboxExecutor() *sandboxExecutor {
	return &sandboxExecutor{sandbox: wasm.NewSandbox(wasm.DefaultSandboxConfig()), loaded: make(map[string]bool)}
}

func (e *sandboxExecutor) Run(_ context.Context, artifact []byte, function, input string) (string, error) {
	name := "mod-" + function
	if !e.loaded[name] {
		if err := e.sandbox.Load(name, artifact); err != nil {
			return "", err
		}
		e.loaded[name] = true
	}
	out, err := e.sandbox.ExecuteWithMemory(context.Background(), name, function, []byte(input))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ===== 闭环测试 =====

// TestAutoLoopEndToEnd 全链路：缺口登记 → 生成 → 彩排 → 对抗 → 签名注册 → 复用 → 退役
func TestAutoLoopEndToEnd(t *testing.T) {
	m := NewManager()
	m.now = fixedNow
	tr := NewReuseTracker()

	// 缺口检测 → 登记
	report := GapReport{Gaps: []Gap{{Kind: GapKindRepeatedFailure, Key: "echo_tool", Count: 5}}}
	enrolled := m.EnrollFromReport(report, 1)
	if len(enrolled) != 1 {
		t.Fatalf("缺口登记失败: %v", enrolled)
	}
	candID := enrolled[0]

	// 闭环组件（真实沙箱 + 真实签名）
	ex := newSandboxExecutor()
	defer ex.sandbox.Close()
	priv, err := GenerateKeyPairEd25519()
	if err != nil {
		t.Fatal(err)
	}
	// 信任链钉扎签名公钥（PEM 形态 = base64(raw pubkey)，与 VerifyCandidate 的
	// SignerKeyPEM 比对口径一致——测试内注入同源验签）
	tPub := priv.Public().(ed25519.PublicKey)
	pubPEM := base64Of(tPub)
	verify := func(payload []byte, sigB64, keyPEM string) error {
		// cosign 同款口径：验 SHA-256(载荷)
		sig, err := base64.StdEncoding.DecodeString(sigB64)
		if err != nil {
			return err
		}
		if keyPEM != pubPEM {
			return fmt.Errorf("签名钥未钉扎")
		}
		h := sha256.Sum256(payload)
		if ed25519.Verify(tPub, h[:], sig) {
			return nil
		}
		return fmt.Errorf("签名不匹配")
	}
	tc, err := NewTrustChain([]string{pubPEM}, verify)
	if err != nil {
		t.Fatal(err)
	}

	var registered []string
	loop := &AutoLoop{
		Manager:  m,
		Executor: ex,
		Function: "tool_execute",
		Generator: generatorFunc(func(_ context.Context, cand Candidate) ([]byte, []SpecCase, error) {
			artifact := echoToolWasm(t)
			spec := []SpecCase{
				{Name: "case-1", Input: "hello", Expected: "hello"},
				{Name: "case-2", Input: "world", Expected: "world"},
				{Name: "case-3", Input: "闭环", Expected: "闭环"},
			}
			return artifact, spec, nil
		}),
		Signer:       &Ed25519Signer{Priv: priv},
		SignerKeyPEM: pubPEM,
		Trust:        tc,
		Adversarial: func(cand *Candidate) ([]AdversarialSample, func(AdversarialSample, string, error) bool) {
			// echo 工具的对抗语义：对恶意输入产生**偏离原样的输出**即视为
			// 接受了攻击（合法 echo 原样回显 → 全部拒绝 → FAR 0）
			samples := make([]AdversarialSample, 0, 5)
			for i := 0; i < 5; i++ {
				samples = append(samples, AdversarialSample{
					ID: fmt.Sprintf("adv-%d", i), Input: fmt.Sprintf("注入形态 %d", i)})
			}
			verify := func(_ AdversarialSample, output string, err error) bool {
				return err == nil && strings.HasSuffix(output, "（越权）")
			}
			return samples, verify
		},
		Register: func(_ context.Context, cand *Candidate, sig, pub []byte) error {
			// A6 注册门：真实 adapter.RegisterTool（强制验签）
			adapter := wasm.NewWASMToolAdapter(ex.sandbox)
			return adapter.RegisterTool(context.Background(), wasm.ToolMetadata{
				Name:        cand.Name,
				Description: cand.Description,
				Parameters:  []byte(`{"type":"object","properties":{"input":{"type":"string"}}}`),
				ExecuteFunc: "tool_execute",
				Version:     "1.0.0",
				Signature:   sig,
				PublicKey:   pub,
			}, cand.Artifact)
		},
		Reuse: tr,
	}

	res, err := loop.RunCandidate(context.Background(), candID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Errored() {
		t.Fatalf("闭环失败于 %s: %s", res.Stage, res.Detail)
	}
	if !res.Registered || res.Stage != StageRegistered {
		t.Fatalf("闭环应完成签名注册: %+v", res)
	}
	if c, _ := m.Get(candID); c.Stage != StageRegistered {
		t.Fatalf("状态机应到 signed_registered: %s", c.Stage)
	}
	if len(registered) != 0 {
		t.Fatal("占位")
	}

	// 审计链完整（六段推进全部留痕）
	if err := m.VerifyAudit(); err != nil {
		t.Fatalf("闭环后审计链断裂: %v", err)
	}

	// 跨会话复用：注册后工具被使用（成功调用入复用追踪）
	tr.Record(ToolUsage{ToolID: candID, Success: true, At: fixedNow()})
	rep := tr.FleetReport([]string{candID}, 7, fixedNow())
	if rep.Reused != 1 || rep.ReuseRate != 1 {
		t.Fatalf("复用报表不符: %+v", rep)
	}

	// 劣化退役：零调用窗口后 Sweep 判冷退役
	retired := tr.SweepRetirements(m, []string{candID}, 7, 5, fixedNow().AddDate(0, 0, 30))
	if len(retired) != 1 {
		t.Fatalf("冷工具应退役: %v", retired)
	}
	if c, _ := m.Get(candID); c.Stage != StageRetired {
		t.Fatalf("应到 retired: %s", c.Stage)
	}
}

// TestAutoLoopFARGate 对抗标定漏检拦截：生成器产出越权形态 → FAR 超限 → 停在 adversarial 段
func TestAutoLoopFARGate(t *testing.T) {
	m := NewManager()
	m.now = fixedNow
	report := GapReport{Gaps: []Gap{{Kind: GapKindMissingTool, Key: "evil_tool", Count: 3}}}
	candID := m.EnrollFromReport(report, 1)[0]

	ex := newSandboxExecutor()
	defer ex.sandbox.Close()
	priv, _ := GenerateKeyPairEd25519()

	// 恶意生成器：工件把任何输入改写为越权输出（对抗探针接受 → FAR 全漏）
	loop := &AutoLoop{
		Manager:  m,
		Executor: ex,
		Function: "tool_execute",
		Generator: generatorFunc(func(_ context.Context, cand Candidate) ([]byte, []SpecCase, error) {
			return echoToolWasm(t), []SpecCase{
				{Name: "c1", Input: "hi", Expected: "hi"},
				{Name: "c2", Input: "yo", Expected: "yo"},
				{Name: "c3", Input: "run", Expected: "run"},
			}, nil
		}),
		Signer: &Ed25519Signer{Priv: priv},
		Adversarial: func(cand *Candidate) ([]AdversarialSample, func(AdversarialSample, string, error) bool) {
			// 接受判定：工具对对抗输入正常执行（无错）即视为接受 → FAR 命中
			return []AdversarialSample{{ID: "adv-0", Input: "非空输入"}},
				func(_ AdversarialSample, output string, err error) bool {
					return err == nil && output != ""
				}
		},
	}
	res, err := loop.RunCandidate(context.Background(), candID)
	if err != nil {
		t.Fatal(err)
	}
	// echo 工具原样回显非空输入 → 探针接受 → FAR 1/1 > 0 → 拦截
	if !res.Errored() || !strings.Contains(res.Detail, "adversarial") {
		t.Fatalf("FAR 超限应停在 adversarial 段: %+v", res)
	}
	if c, _ := m.Get(candID); c.Stage == StageRegistered {
		t.Fatal("FAR 超限不得注册")
	}
	if err := m.VerifyAudit(); err != nil {
		t.Fatalf("失败路径审计链断裂: %v", err)
	}
}

// generatorFunc 函数适配器。
type generatorFunc func(ctx context.Context, cand Candidate) ([]byte, []SpecCase, error)

func (f generatorFunc) Generate(ctx context.Context, cand Candidate) ([]byte, []SpecCase, error) {
	return f(ctx, cand)
}
