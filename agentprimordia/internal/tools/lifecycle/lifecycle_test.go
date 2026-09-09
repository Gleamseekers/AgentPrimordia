// lifecycle_test.go — 工具生命周期测试（v6.3 工程地板）
//
// 覆盖：状态机全序推进/防跳段/防混证据、缺口报表确定性聚合与登记、
// 信任链（真实 ECDSA P-256 签名 + 钉扎 + 工件哈希锚定）、复用/退役策略
// （命题 2 口径）、强验证器族（彩排/表决/FAR 标定）。
package lifecycle

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"
)

// fixedNow 确定时钟。
func fixedNow() time.Time { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC) }

// TestStateMachineOrdering 状态机：全序推进成功 + 跳段/混证据/空工件拒绝
func TestStateMachineOrdering(t *testing.T) {
	m := NewManager()
	m.now = fixedNow
	id := "gap-missing_tool-code_exec"
	if err := m.Enroll(Candidate{ID: id, Name: "code_exec", Domain: "missing_tool", Description: "缺口"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Get(id); c.Stage != StageGap {
		t.Fatalf("登记后应处 gap_detected，got %s", c.Stage)
	}
	// 未附工件不得推进
	if err := m.Advance(id, Evidence{Probe: "generation", Pass: true, Detail: "x"}); err == nil {
		t.Fatal("未附工件推进应被拒绝")
	}
	// 附工件 → generated
	if err := m.AttachArtifact(id, []byte("wasm-bytes")); err != nil {
		t.Fatal(err)
	}
	// 混证据：用 rehearsal 证据推 generated → 拒绝
	if err := m.Advance(id, Evidence{Probe: "rehearsal", Pass: true}); err == nil {
		t.Fatal("证据类别混用应被拒绝")
	}
	if err := m.Advance(id, Evidence{Probe: "generation", Pass: true, Detail: "外部开发工件"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Advance(id, Evidence{Probe: "rehearsal", Pass: true, Detail: "3/3 用例"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Advance(id, Evidence{Probe: "adversarial", Pass: true, Detail: "对抗 0 漏检"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Advance(id, Evidence{Probe: "cosign", Pass: true, Detail: "钉扎验签通过"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Get(id); c.Stage != StageRegistered {
		t.Fatalf("应到 signed_registered，got %s", c.Stage)
	}
	if got := m.ListRegistered(); len(got) != 1 || got[0] != id {
		t.Fatalf("注册清单不符: %v", got)
	}
	// 注册态再推进 = 终态拒绝
	if err := m.Advance(id, Evidence{Probe: "rehearsal", Pass: true}); err == nil {
		t.Fatal("注册态不得再推进")
	}
	// 退役
	if err := m.Retire(id, "cold_tool"); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Get(id); c.Stage != StageRetired {
		t.Fatalf("应退役，got %s", c.Stage)
	}
	if err := m.VerifyAudit(); err != nil {
		t.Fatalf("审计链校验失败: %v", err)
	}
	// 未通过证据不得推进
	m2 := NewManager()
	m2.now = fixedNow
	_ = m2.Enroll(Candidate{ID: "x", Name: "t"})
	_ = m2.AttachArtifact("x", []byte("a"))
	if err := m2.Advance("x", Evidence{Probe: "generation", Pass: false, Detail: "彩排失败"}); err == nil {
		t.Fatal("未通过证据推进应被拒绝")
	}
}

// TestGapReport 缺口报表：确定性聚合 + 阈值 + 登记
func TestGapReport(t *testing.T) {
	a := NewGapAuditor(24*time.Hour, 3)
	now := fixedNow()
	// missing_tool：3 次 search_web 未注册调用
	for i := 0; i < 3; i++ {
		a.Record(GapCall{ToolName: "search_web", ErrText: "tool not found: search_web", At: now})
	}
	// repeated_failure：fetch_url 5 调 4 败（≥3 达阈值）
	for i := 0; i < 4; i++ {
		a.Record(GapCall{ToolName: "fetch_url", ErrText: fmt.Sprintf("timeout #%d", i%2), At: now})
	}
	a.Record(GapCall{ToolName: "fetch_url", OK: true, At: now})
	// 未达阈值：read_file 1 次失败
	a.Record(GapCall{ToolName: "read_file", ErrText: "perm", At: now})
	// 窗口外：不计入
	a.Record(GapCall{ToolName: "old_tool", ErrText: "tool not found: old_tool", At: now.Add(-48 * time.Hour)})

	rep := a.Report(now)
	if rep.Total != 9 {
		t.Fatalf("窗口内观测应 9 次，got %d", rep.Total)
	}
	if len(rep.Gaps) != 2 {
		t.Fatalf("应报 2 条缺口（missing 1 + repeated 1），got %+v", rep.Gaps)
	}
	// 排序：计数降序（fetch_url 4 > search_web 3）
	if rep.Gaps[0].Key != "fetch_url" || rep.Gaps[0].Kind != GapKindRepeatedFailure {
		t.Fatalf("首位应为 fetch_url repeated_failure: %+v", rep.Gaps[0])
	}
	if rep.Gaps[0].Count != 4 || len(rep.Gaps[0].SampleErrors) != 2 {
		t.Fatalf("fetch_url 聚合/去重不符: %+v", rep.Gaps[0])
	}
	if rep.Gaps[1].Key != "search_web" || rep.Gaps[1].Kind != GapKindMissingTool {
		t.Fatalf("次位应为 search_web missing_tool: %+v", rep.Gaps[1])
	}
	// 报表 → 生命周期登记（确定性 ID）
	m := NewManager()
	m.now = fixedNow
	enrolled := m.EnrollFromReport(rep, 1)
	if len(enrolled) != 2 {
		t.Fatalf("应登记 2 个候选: %v", enrolled)
	}
	if _, ok := m.Get("gap-missing_tool-search_web"); !ok {
		t.Fatal("缺口候选应可查")
	}
}

// TestTrustChainRealECDSA 信任链：真实 ECDSA P-256 签名 + 钉扎 + 哈希锚定
func TestTrustChainRealECDSA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pemEncodePubKey(&key.PublicKey)
	artifact := []byte("wasm-module-bytes")
	digest := sha256.Sum256(artifact) // cosign 口径：SHA-256(artifact) 直接签名
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// 注入 marketplace 同款验签（本地实现避免 import 倒置；算法一致）
	verify := func(payload []byte, signatureB64, publicKeyPEM string) error {
		got, err := base64.StdEncoding.DecodeString(signatureB64)
		if err != nil {
			return err
		}
		pk, err := pemDecodePubKey(publicKeyPEM)
		if err != nil {
			return err
		}
		d := sha256.Sum256(payload)
		if ecdsa.VerifyASN1(pk, d[:], got) {
			return nil
		}
		return errors.New("签名验证失败")
	}
	tc, err := NewTrustChain([]string{pubPEM}, verify)
	if err != nil {
		t.Fatal(err)
	}

	c := Candidate{ID: "c1", Name: "tool", Artifact: artifact, ArtifactSHA: sha256HexOf(artifact), SignerKeyPEM: pubPEM}
	if err := tc.VerifyCandidate(&c, sigB64); err != nil {
		t.Fatalf("合法签名应通过: %v", err)
	}
	// 篡改工件 → 哈希锚定拒绝
	bad := Candidate{ID: "c2", Name: "tool", Artifact: []byte("tampered"), ArtifactSHA: sha256HexOf(artifact), SignerKeyPEM: pubPEM}
	if err := tc.VerifyCandidate(&bad, sigB64); err == nil {
		t.Fatal("工件哈希不符应拒绝")
	}
	// 未钉扎签名者 → 拒绝
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherPEM := pemEncodePubKey(&other.PublicKey)
	c3 := Candidate{ID: "c3", Name: "tool", Artifact: artifact, ArtifactSHA: sha256HexOf(artifact), SignerKeyPEM: otherPEM}
	if err := tc.VerifyCandidate(&c3, sigB64); err == nil {
		t.Fatal("未钉扎签名者应拒绝")
	}
	// 坏签名 → 拒绝
	if err := tc.VerifyCandidate(&c, base64.StdEncoding.EncodeToString([]byte("garbage"))); err == nil {
		t.Fatal("坏签名应拒绝")
	}
	// 钉扎轮换：Pin 新钥后，新钥签名通过
	otherSig, err := ecdsa.SignASN1(rand.Reader, other, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	tc.Pin(otherPEM)
	c4 := Candidate{ID: "c4", Name: "tool", Artifact: artifact, ArtifactSHA: sha256HexOf(artifact), SignerKeyPEM: otherPEM}
	if err := tc.VerifyCandidate(&c4, base64.StdEncoding.EncodeToString(otherSig)); err != nil {
		t.Fatalf("钉扎轮换后应通过: %v", err)
	}
	// 旧钥签名仍有效（轮换窗口内多钥并存）
	if err := tc.VerifyCandidate(&c, sigB64); err != nil {
		t.Fatalf("轮换窗口内旧钥应仍有效: %v", err)
	}
}

// TestReuseAndRetirement 复用追踪：命题 2 口径 + 退役策略
func TestReuseAndRetirement(t *testing.T) {
	now := fixedNow()
	tr := NewReuseTracker()
	// tool-a：窗口内 6 调 5 成功 → 保留；tool-b：6 调 5 败 → 劣化退役；tool-c：零调用 → 冷退役
	for i := 0; i < 6; i++ {
		tr.Record(ToolUsage{ToolID: "tool-a", Success: i != 0, At: now})
		tr.Record(ToolUsage{ToolID: "tool-b", Success: i == 0, At: now})
	}
	rep := tr.FleetReport([]string{"tool-a", "tool-b", "tool-c", "tool-ghost"}, 7, now)
	if rep.Registered != 4 || rep.Reused != 2 || rep.TotalCalls != 12 {
		t.Fatalf("舰队报表不符: %+v", rep)
	}
	if rep.ReuseRate != 0.5 {
		t.Fatalf("复用率应 0.5，got %.3f", rep.ReuseRate)
	}
	if rep.ReuseWilsonLo <= 0 || rep.ReuseWilsonLo >= rep.ReuseRate {
		t.Fatalf("Wilson 下界应落于 (0, rate): %.4f", rep.ReuseWilsonLo)
	}
	// 退役扫描（确定性，结果升序）
	m := NewManager()
	m.now = fixedNow
	for _, id := range []string{"tool-a", "tool-b", "tool-c"} {
		_ = m.Enroll(Candidate{ID: id, Name: id})
	}
	retired := tr.SweepRetirements(m, []string{"tool-a", "tool-b", "tool-c"}, 7, 5, now)
	if len(retired) != 2 || retired[0] != "tool-b" || retired[1] != "tool-c" {
		t.Fatalf("退役清单不符: %v", retired)
	}
	if err := m.VerifyAudit(); err != nil {
		t.Fatalf("退役后审计链断裂: %v", err)
	}
	// 注册名不在使用流（tool-ghost）计入分母但不计入复用——命题 2 口径验证
}

// TestVerifiers 强验证器族：彩排/表决/FAR 标定
func TestVerifiers(t *testing.T) {
	// 沙箱彩排：全过/有败/错误计败
	exec := &scriptedExecutor{outputs: map[string]string{"a": "A", "b": "B"}, errOn: make(map[string]bool)}
	v := &CodeExecVerifier{Executor: exec, Function: "tool_main"}
	if !v.Verify(context.Background(), []byte("w"), []SpecCase{{Name: "b", Input: "b", Expected: "B"}, {Name: "a", Input: "a", Expected: "A"}}).Pass {
		t.Fatal("全过用例应通过")
	}
	rep := v.Verify(context.Background(), []byte("w"), []SpecCase{{Name: "a", Input: "a", Expected: "X"}})
	if rep.Pass || rep.Probe != "rehearsal" {
		t.Fatalf("错答应不通过: %+v", rep)
	}
	exec.errOn["c"] = true
	rep = v.Verify(context.Background(), []byte("w"), []SpecCase{{Name: "c", Input: "c", Expected: "C"}})
	if rep.Pass {
		t.Fatal("执行错误应计失败")
	}

	// 表决：2/3 多数过；1/3+1 弃权不过（平票保守）
	judges := []JudgeFunc{
		func(context.Context, []byte, string) string { return "pass" },
		func(context.Context, []byte, string) string { return "pass" },
		func(context.Context, []byte, string) string { return "fail" },
	}
	ens := &EnsembleJudgeVerifier{Judges: judges}
	if !ens.Verify(context.Background(), nil, "in").Pass {
		t.Fatal("2/3 多数应通过")
	}
	ens2 := &EnsembleJudgeVerifier{Judges: []JudgeFunc{
		func(context.Context, []byte, string) string { return "pass" },
		func(context.Context, []byte, string) string { return "abstain" },
	}}
	if ens2.Verify(context.Background(), nil, "in").Pass {
		t.Fatal("平票应保守不过")
	}

	// FAR 标定：10 个对抗样本全拒 → FAR 0（R3：仍带 Wilson 区间披露）；漏 1 → 全量披露
	strict := func(s AdversarialSample) bool { return false }
	far, err := CalibrateFAR("adversarial", strict, tenAdversarial())
	if err != nil {
		t.Fatal(err)
	}
	if far.Accepted != 0 || far.Rate != 0 || len(far.AcceptedIDs) != 0 {
		t.Fatalf("全拒标定不符: %+v", far)
	}
	if far.WilsonHi <= 0 || far.WilsonHi >= 0.35 {
		t.Fatalf("FAR=0 也必须披露 Wilson 上界（R3 不裸报）: %.4f", far.WilsonHi)
	}
	leaky := func(s AdversarialSample) bool { return s.ID == "adv-03" }
	far2, err := CalibrateFAR("adversarial", leaky, tenAdversarial())
	if err != nil {
		t.Fatal(err)
	}
	if far2.Accepted != 1 || len(far2.AcceptedIDs) != 1 || far2.AcceptedIDs[0] != "adv-03" {
		t.Fatalf("漏检应全量披露: %+v", far2)
	}
	// 空样本集拒绝
	if _, err := CalibrateFAR("adversarial", strict, nil); err == nil {
		t.Fatal("空样本集标定应拒绝")
	}
}

// tenAdversarial 10 个确定性对抗样本。
func tenAdversarial() []AdversarialSample {
	var out []AdversarialSample
	for i := 0; i < 10; i++ {
		out = append(out, AdversarialSample{ID: fmt.Sprintf("adv-%02d", i), Artifact: []byte("evil"), Input: "注入"})
	}
	return out
}

// scriptedExecutor 确定性执行器替身。
type scriptedExecutor struct {
	outputs map[string]string
	errOn   map[string]bool
}

func (s *scriptedExecutor) Run(_ context.Context, _ []byte, _, input string) (string, error) {
	if s.errOn[input] {
		return "", fmt.Errorf("sandbox fault")
	}
	if v, ok := s.outputs[input]; ok {
		return v, nil
	}
	return "", fmt.Errorf("no case for %q", input)
}

// ===== 测试本地 crypto 助手（PEM/哈希；与 marketplace 算法口径一致）=====

const testPubPEMTmpl = "-----BEGIN PUBLIC KEY-----\n%s\n-----END PUBLIC KEY-----\n"

func pemEncodePubKey(pk *ecdsa.PublicKey) string {
	// 非压缩公钥：0x04 || X (32 bytes) || Y (32 bytes)，与 elliptic.Marshal 口径一致
	raw := make([]byte, 65)
	raw[0] = 0x04
	pk.X.FillBytes(raw[1:33])
	pk.Y.FillBytes(raw[33:65])
	return fmt.Sprintf(testPubPEMTmpl, base64.StdEncoding.EncodeToString(raw))
}

func pemDecodePubKey(pem string) (*ecdsa.PublicKey, error) {
	var b64 string
	if _, err := fmt.Sscanf(pem, testPubPEMTmpl, &b64); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	// 非压缩公钥：0x04 || X (32 bytes) || Y (32 bytes)，与 elliptic.Unmarshal 口径一致
	if len(raw) != 65 || raw[0] != 0x04 {
		return nil, errors.New("bad point")
	}
	x := new(big.Int).SetBytes(raw[1:33])
	y := new(big.Int).SetBytes(raw[33:65])
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

func sha256HexOf(b []byte) string {
	s := sha256.Sum256(b)
	return fmt.Sprintf("%x", s)
}

// 防止 big 包未用导入（ECDSA 签名校验路径预留）
var _ = big.NewInt
