// trust_false_positive_test.go — 误拦口径（InterceptStats.FalsePositives）确定性测试
//
// 口径（2026-09-28 接线，trust.go ReceiveAsset/InterceptStats）：
//   - FalsePositives 只计「节点级拒绝但资产级合格」的拦截：节点在隔离区
//     导致投递被拒，而该资产本身通过全部资产级检查（门 1–3：完整性哈希 /
//     钉扎钥 / 验签算法 / 溯源无回环——到达隔离区分支即已全过），且载荷
//     非「他人重签」形态（首发为空或首发即本节点）。隔离的是节点不是资产。
//   - 资产级拒绝（完整性失败 / 未钉扎钥 / 验签失败 / 溯源回环 / 他人资产
//     重签）一律不计误拦——资产本身不合格。
//   - FalsePositives ⊆ Intercepted ⊆ Attempts：误拦是真实发生的拦截的子集，
//     ReceiveAsset 对隔离区投递仍返回 error（零信任姿态不变）。
package federation

import "testing"

// quarantineViaEvents 以负权重事件把节点压穿默认 MinReputation(-5) 送入
// 隔离区（与 RecordEvent 生产路径同机制：5 × -1 = -5 ≤ -5 → 隔离）。
func quarantineViaEvents(t *testing.T, tl *TrustLayer, node NodeID) {
	t.Helper()
	for i := 0; i < 5; i++ {
		tl.RecordEvent(TrustEvent{Node: node, Kind: "violation", Weight: -1, At: fixedTime()})
	}
	rep := tl.Report()
	if len(rep.Quarantined) != 1 || rep.Quarantined[0] != node {
		t.Fatalf("节点 %s 应入隔离区: %+v", node, rep.Quarantined)
	}
}

// TestFalsePositiveQuarantinedNodeQualifiedAsset 隔离区节点投递合格资产：
// 仍被拒收（零信任），且计入误拦 +1（同时计入 attempts/intercepted——
// 误拦是拦截的子集）；事件流 +1；节点声誉不被该路径改变。
func TestFalsePositiveQuarantinedNodeQualifiedAsset(t *testing.T) {
	tl := newTrust(t)
	bad := NodeID("node-bad")
	quarantineViaEvents(t, tl, bad)

	before := tl.InterceptStats()
	beforeEvents := tl.Report().Events
	var reputBefore float64
	for _, e := range tl.Report().Entries {
		if e.Node == bad {
			reputBefore = e.Reputation
		}
	}

	// 合格资产：新载荷、验签通过、溯源无回环、非他人重签形态
	if err := tl.ReceiveAsset(envelope("asset-q", bad, 1, "key-1"), fixedTime()); err == nil {
		t.Fatal("隔离区节点投递应仍被拒收（零信任）")
	}

	after := tl.InterceptStats()
	if after.FalsePositives != before.FalsePositives+1 {
		t.Fatalf("隔离区节点投递合格资产应计误拦 +1: before=%+v after=%+v", before, after)
	}
	if after.Intercepted != before.Intercepted+1 || after.Attempts != before.Attempts+1 {
		t.Fatalf("误拦应同时计入 attempts/intercepted（子集关系）: before=%+v after=%+v", before, after)
	}
	if got := tl.Report().Events; got != beforeEvents+1 {
		t.Fatalf("事件流应 +1, got %d want %d", got, beforeEvents+1)
	}
	var reputAfter float64
	for _, e := range tl.Report().Entries {
		if e.Node == bad {
			reputAfter = e.Reputation
		}
	}
	if reputAfter != reputBefore {
		t.Fatalf("隔离区拒收路径不应改变节点声誉: before=%v after=%v", reputBefore, reputAfter)
	}
	// 重复投递合格资产：每次拒绝各计一次误拦（确定性累计）
	if err := tl.ReceiveAsset(envelope("asset-q2", bad, 1, "key-2"), fixedTime()); err == nil {
		t.Fatal("隔离区节点投递应仍被拒收（零信任）")
	}
	if got := tl.InterceptStats().FalsePositives; got != 2 {
		t.Fatalf("第二次合格投递应累计误拦 2, got %d", got)
	}
}

// TestFalsePositiveNotCountedForAssetLevelFailures 资产级拒绝不计误拦：
// 隔离区节点投递验签失败 / 完整性失败 / 未钉扎钥 / 溯源回环 / 他人重签
// 形态的资产——资产本身不合格，拒绝与隔离区无关（门 1–3 先行命中）。
func TestFalsePositiveNotCountedForAssetLevelFailures(t *testing.T) {
	// ① 验签失败（门 2b 先于隔离区分支命中）
	tlBad, err := NewTrustLayer(TrustConfig{Verify: neverVerify, PinnedKeys: []string{"key-1"}})
	if err != nil {
		t.Fatal(err)
	}
	quarantineViaEvents(t, tlBad, "node-bad")
	if err := tlBad.ReceiveAsset(envelope("asset-x", "node-bad", 1, "key-1"), fixedTime()); err == nil {
		t.Fatal("验签失败应拒绝")
	}
	if fp := tlBad.InterceptStats().FalsePositives; fp != 0 {
		t.Fatalf("验签失败的资产级拒绝不计误拦, got %d", fp)
	}

	// ② 他人重签形态：载荷首发是他人 → 隔离区分支内判定资产级不合格
	tl := newTrust(t)
	good, bad := NodeID("node-good"), NodeID("node-bad")
	first := envelope("shared-asset", good, 1, "key-1")
	if err := tl.ReceiveAsset(first, fixedTime()); err != nil {
		t.Fatal(err)
	}
	quarantineViaEvents(t, tl, bad)
	resign := envelope("shared-asset", bad, 1, "key-1")
	resign.PayloadSHA = first.PayloadSHA // 同载荷原样重签（他人资产重签形态）
	if err := tl.ReceiveAsset(resign, fixedTime()); err == nil {
		t.Fatal("隔离区节点投递应拒收")
	}
	if fp := tl.InterceptStats().FalsePositives; fp != 0 {
		t.Fatalf("他人重签形态的资产级拒绝不计误拦, got %d", fp)
	}

	// ③ 完整性失败 / ④ 未钉扎钥 / ⑤ 溯源回环：门 1–3 先行命中，不计误拦
	tl2 := newTrust(t)
	quarantineViaEvents(t, tl2, "node-bad")
	tampered := envelope("asset-t", "node-bad", 1, "key-1")
	tampered.PayloadSHA = "deadbeef"
	if err := tl2.ReceiveAsset(tampered, fixedTime()); err == nil {
		t.Fatal("完整性门应拒绝篡改")
	}
	if err := tl2.ReceiveAsset(envelope("asset-k", "node-bad", 1, "key-evil"), fixedTime()); err == nil {
		t.Fatal("未钉扎钥应拒绝")
	}
	loop := envelope("asset-l", "node-bad", 1, "key-1")
	loop.Provenance = []NodeID{NodeID("node-relay"), "node-bad"}
	if err := tl2.ReceiveAsset(loop, fixedTime()); err == nil {
		t.Fatal("溯源回环应拒绝")
	}
	if fp := tl2.InterceptStats().FalsePositives; fp != 0 {
		t.Fatalf("门 1–3 的资产级拒绝不计误拦, got %d", fp)
	}
}

// TestFalsePositiveNonQuarantinePathsUnchanged 非隔离路径一切行为不变：
// 合法贡献、同节点幂等重放、他人重签拦截、资产级失败均不计误拦。
func TestFalsePositiveNonQuarantinePathsUnchanged(t *testing.T) {
	tl := newTrust(t)
	// 合法贡献 + 同节点幂等重放（不加声誉）
	if err := tl.ReceiveAsset(envelope("a1", NodeID("n1"), 1, "key-1"), fixedTime()); err != nil {
		t.Fatal(err)
	}
	if err := tl.ReceiveAsset(envelope("a1", NodeID("n1"), 1, "key-1"), fixedTime()); err != nil {
		t.Fatal(err)
	}
	// 他人重签拦截（n3 声誉 -1，未到隔离区）
	first := envelope("shared", NodeID("n2"), 1, "key-1")
	if err := tl.ReceiveAsset(first, fixedTime()); err != nil {
		t.Fatal(err)
	}
	resign := envelope("shared", NodeID("n3"), 1, "key-1")
	resign.PayloadSHA = first.PayloadSHA
	if err := tl.ReceiveAsset(resign, fixedTime()); err == nil {
		t.Fatal("重签刷分应被拦截")
	}
	// 资产级失败（完整性 / 未钉扎钥 / 溯源回环）
	tampered := envelope("a2", NodeID("n1"), 1, "key-1")
	tampered.PayloadSHA = "deadbeef"
	if err := tl.ReceiveAsset(tampered, fixedTime()); err == nil {
		t.Fatal("完整性门应拒绝篡改")
	}
	if err := tl.ReceiveAsset(envelope("a3", NodeID("n1"), 1, "key-evil"), fixedTime()); err == nil {
		t.Fatal("未钉扎钥应拒绝")
	}
	loop := envelope("a4", NodeID("n1"), 1, "key-1")
	loop.Provenance = []NodeID{NodeID("n9"), NodeID("n1")}
	if err := tl.ReceiveAsset(loop, fixedTime()); err == nil {
		t.Fatal("溯源回环应拒绝")
	}
	// 无隔离区节点 → 无误拦；既有 attempts/intercepted 汇总口径不变
	if len(tl.Report().Quarantined) != 0 {
		t.Fatalf("不应有节点被隔离: %+v", tl.Report().Quarantined)
	}
	stats := tl.InterceptStats()
	if stats.FalsePositives != 0 {
		t.Fatalf("非隔离路径不应计误拦, got %d", stats.FalsePositives)
	}
	if stats.Attempts != 4 || stats.Intercepted != 4 { // 1 重签 + 3 资产级失败
		t.Fatalf("非隔离路径汇总口径应不变: %+v", stats)
	}
}
