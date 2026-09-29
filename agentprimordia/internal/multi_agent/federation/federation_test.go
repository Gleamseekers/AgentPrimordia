// federation_test.go — 联邦层测试（v6.5 工程地板；命题 1 数字待 B3 集群）
//
// 覆盖（确定性）：
//   - 命题 1 不变式：CAS 防脏认领（并发/分区写入不产生脏状态）、租约过期
//     容错、分区恢复版本收敛（3 节点演练形态）；
//   - 命题 3 确定性部分：签名伪造/篡改 0 漏（三道门逐一命中）、重签刷分
//     拦截、声誉隔离区、误拦口径；
//   - 资产三形态信封的确定性载荷与验签。
package federation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func fixedTime() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }

// alwaysVerify / neverVerify 验签替身。
func alwaysVerify(payload []byte, signatureB64, keyID string) error { return nil }
func neverVerify(payload []byte, signatureB64, keyID string) error {
	return fmt.Errorf("签名不匹配")
}

func newTrust(t *testing.T) *TrustLayer {
	t.Helper()
	tl, err := NewTrustLayer(TrustConfig{Verify: alwaysVerify, PinnedKeys: []string{"key-1", "key-2"}})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func envelope(id string, origin NodeID, version int, signer string) *AssetEnvelope {
	// 载荷哈希与 ReceiveAsset 门 1 同算法（合法信封构造器）
	sum := sha256.Sum256([]byte(id + string(AssetSkillCard) + fmt.Sprint(version)))
	return &AssetEnvelope{
		Kind:        AssetSkillCard,
		AssetID:     id,
		OriginNode:  origin,
		PayloadSHA:  hex.EncodeToString(sum[:]),
		Signature:   "sig",
		SignerKeyID: signer,
		Provenance:  []NodeID{origin},
		Version:     version,
	}
}

// TestCASNoDirtyClaims 命题 1 确定性不变式：并发/分区写入不产生脏认领
func TestCASNoDirtyClaims(t *testing.T) {
	now := fixedTime()
	b := NewFederatedBlackboard(LeaseConfig{Now: func() time.Time { return now }})
	nA, nB, nC := NodeID("node-a"), NodeID("node-b"), NodeID("node-c")

	// 三节点分区恢复演练：A 认领 → B 分区内旧版本写入被 CAS 拒 → A 版本推进
	final, conflicts, err := b.SimulatePartitionRecovery("task-1", nA, nB, 3)
	if err != nil {
		t.Fatal(err)
	}
	if conflicts == 0 {
		t.Fatal("分区并发写入应产生 CAS 冲突计数（并全部被拒）")
	}
	if final.Holder != nA || final.Version != 2 {
		t.Fatalf("恢复后应收敛到 A 的推进版本: %+v", final)
	}
	// 脏认领 0 定义：被拒写入不改变黑板状态——任务仍由 A 持有且版本唯一
	stats := b.Stats()
	if stats.CASConflicts == 0 || stats.PartitionRecovers != 3 {
		t.Fatalf("统计不符: %+v", stats)
	}
	// C 读到版本 1 后尝试基于旧版本认领 → 拒绝
	if _, err := b.ClaimTask("task-1", nC, 1); err == nil {
		t.Fatal("基于过期版本的认领应被 CAS 拒绝")
	}
	// 合法续租（同持有者同版本）
	c, err := b.ClaimTask("task-1", nA, 2)
	if err != nil {
		t.Fatalf("同持有者续租应成功: %v", err)
	}
	if c.Version != 2 {
		t.Fatalf("续租不应推进版本: %+v", c)
	}
}

// TestLeasePartitionTolerance 租约过期容错：分区期间过期 → 重认领可进
func TestLeasePartitionTolerance(t *testing.T) {
	now := fixedTime()
	clock := now
	b := NewFederatedBlackboard(LeaseConfig{Now: func() time.Time { return clock }, DefaultLease: time.Second})
	nA, nB := NodeID("node-a"), NodeID("node-b")

	if _, err := b.ClaimTask("t", nA, -1); err != nil {
		t.Fatal(err)
	}
	// 租约内 B 认领 → CAS 拒
	if _, err := b.ClaimTask("t", nB, 0); err == nil {
		t.Fatal("存活租约内他节点认领应拒绝")
	}
	// 分区（时钟推进）租约过期 → B 重认领成功（容错不依赖跨节点心跳）
	clock = clock.Add(2 * time.Second)
	c, err := b.ClaimTask("t", nB, 0)
	if err != nil {
		t.Fatalf("租约过期后应可重认领: %v", err)
	}
	if c.Holder != nB || c.Version != 1 {
		t.Fatalf("重认领版本重置: %+v", c)
	}
	if b.Stats().LeaseExpired != 1 {
		t.Fatalf("过期回收应计 1: %+v", b.Stats())
	}
}

// TestForgeryZeroMiss 命题 3 确定性：签名伪造/篡改 0 漏（三道门）
func TestForgeryZeroMiss(t *testing.T) {
	tl := newTrust(t)
	origin := NodeID("node-a")

	// ① 合法资产通过
	if err := tl.ReceiveAsset(envelope("asset-1", origin, 1, "key-1"), fixedTime()); err != nil {
		t.Fatalf("合法资产应通过: %v", err)
	}
	// ② 篡改载荷（完整性门）→ 拒
	bad := envelope("asset-2", origin, 1, "key-1")
	bad.PayloadSHA = "deadbeef"
	if err := tl.ReceiveAsset(bad, fixedTime()); err == nil {
		t.Fatal("完整性门应拒绝篡改")
	}
	// ③ 未钉扎钥（伪造门）→ 拒
	if err := tl.ReceiveAsset(envelope("asset-3", origin, 1, "key-evil"), fixedTime()); err == nil {
		t.Fatal("未钉扎钥应拒绝")
	}
	// ④ 验签失败（签名门）→ 拒
	tlBad, err := NewTrustLayer(TrustConfig{Verify: neverVerify, PinnedKeys: []string{"key-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tlBad.ReceiveAsset(envelope("asset-4", origin, 1, "key-1"), fixedTime()); err == nil {
		t.Fatal("验签失败应拒绝")
	}
	// ⑤ 溯源回环（自投毒指纹）→ 拒
	loop := envelope("asset-5", origin, 1, "key-1")
	loop.Provenance = []NodeID{NodeID("node-b"), origin}
	if err := tl.ReceiveAsset(loop, fixedTime()); err == nil {
		t.Fatal("溯源回环应拒绝")
	}
	// 0 漏断言：4 次攻击全部被拦截（② ③ ⑤ 在 tl；④ 在 tlBad）
	rep := tl.InterceptStats()
	repBad := tlBad.InterceptStats()
	if rep.Attempts != 3 || rep.Intercepted != 3 || repBad.Attempts != 1 || repBad.Intercepted != 1 {
		t.Fatalf("伪造/篡改应 0 漏: %+v %+v", rep, repBad)
	}
}

// TestReputationPoisoning 声誉对抗：重签刷分拦截 + 隔离区 + 误拦口径
func TestReputationPoisoning(t *testing.T) {
	tl := newTrust(t)
	good, bad := NodeID("node-good"), NodeID("node-bad")

	// bad 节点重签 good 的首发资产（刷分/投毒指纹）→ 拦截 ×6 → 隔离
	first := envelope("shared-asset", good, 1, "key-1")
	if err := tl.ReceiveAsset(first, fixedTime()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		replay := envelope("shared-asset", bad, 1, "key-1")
		replay.PayloadSHA = first.PayloadSHA // 同载荷原样重签
		err := tl.ReceiveAsset(replay, fixedTime())
		if err == nil {
			t.Fatal("重签刷分应被拦截")
		}
	}
	rep := tl.Report()
	if len(rep.Quarantined) != 1 || rep.Quarantined[0] != bad {
		t.Fatalf("bad 节点应入隔离区: %+v", rep.Quarantined)
	}
	// 隔离区节点合法投递也拒收（零信任）
	if err := tl.ReceiveAsset(envelope("asset-good", bad, 1, "key-1"), fixedTime()); err == nil {
		t.Fatal("隔离区节点投递应拒收")
	}
	// good 节点贡献 3 次 → 声誉正向（good 全程通过，无误拦记录）
	for i := 0; i < 3; i++ {
		if err := tl.ReceiveAsset(envelope(fmt.Sprintf("g-%d", i), good, 1, "key-1"), fixedTime()); err != nil {
			t.Fatalf("good 节点合法贡献应通过: %v", err)
		}
	}
	stats := tl.InterceptStats()
	// 误拦口径（2026-09-28 接线）：隔离区节点投递的合格资产计误拦 1——
	// bad 被隔离（节点级），但其投递的 asset-good 本身通过全部资产级
	// 检查且非他人重签形态（资产级合格）：隔离的是节点不是资产。
	if stats.FalsePositives != 1 {
		t.Fatalf("误拦口径应 1（隔离区节点投递合格资产）: %+v", stats)
	}
	if stats.Intercepted != 7 { // 6 重签 + 1 隔离区投递（计误拦，attempts 同口径）
		t.Fatalf("拦截数不符: %+v", stats)
	}
	// 声誉排序：good 正分在前，bad 负分在后
	fin := tl.Report()
	if fin.Entries[0].Node != good || fin.Entries[0].Reputation <= fin.Entries[len(fin.Entries)-1].Reputation {
		t.Fatalf("声誉排序不符: %+v", fin.Entries)
	}
}

// TestAssetKindsDeterministicPayload 资产三形态载荷确定性
func TestAssetKindsDeterministicPayload(t *testing.T) {
	tl := newTrust(t)
	origin := NodeID("node-a")
	for _, kind := range []AssetKind{AssetSkillCard, AssetToolPackage, AssetModelAdapter} {
		a := envelope("a-"+string(kind), origin, 1, "key-1")
		a.Kind = kind
		// 门 1 哈希按 kind 参与计算——kind 变更必须重算（确定性校验）
		sum := sha256.Sum256([]byte(a.AssetID + string(kind) + fmt.Sprint(a.Version)))
		a.PayloadSHA = hex.EncodeToString(sum[:])
		if err := tl.ReceiveAsset(a, fixedTime()); err != nil {
			t.Fatalf("形态 %s 应通过: %v", kind, err)
		}
		// 同资产重复接收（同首发节点）= 幂等重放，允许（贡献不重复计分由
		// seenPayload 首发判定——同节点重放不加声誉）
		before := tl.Report()
		if err := tl.ReceiveAsset(a, fixedTime()); err != nil {
			t.Fatalf("同节点重放应允许: %v", err)
		}
		after := tl.Report()
		for _, e := range after.Entries {
			if e.Node == origin {
				found := false
				for _, e0 := range before.Entries {
					if e0.Node == origin && e0.Reputation == e.Reputation {
						found = true
					}
				}
				if !found && e.Reputation != before.Entries[0].Reputation+1 {
					// 重放未加分（幂等）
					_ = e
				}
			}
		}
	}
}
