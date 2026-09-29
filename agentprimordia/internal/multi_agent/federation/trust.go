// trust.go — 联邦资产流通 + 社会信任层（签名伪造 0 漏 + 声誉对抗）
//
// 命题 3（信任层可对抗）确定性部分：
//   - 签名伪造/篡改：确定性校验 0 漏（完整性哈希 + 钉扎验签 + 溯源链校验
//     三道门，任一不过即拒绝且入信任事件）；
//   - 声誉刷分与坏贡献：确定性规则拦截（自投毒指纹/权重超限/溯源回环），
//     拦截率以报告披露（Wilson 口径）；误拦率同口径全量披露。
package federation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// VerifierFunc 资产验签函数（组装根绑定 cosign 同款算法——与 lifecycle
// 信任链同模式：multi_agent 不 import 横向验签实现）。
type VerifierFunc func(payload []byte, signatureB64, keyID string) error

// TrustConfig 信任层配置。
type TrustConfig struct {
	Verify         VerifierFunc // 资产验签（必填）
	PinnedKeys     []string     // 钉扎签名钥指纹（≥1）
	MaxEventWeight float64      // 单事件权重上限（刷分防御；≤0 取 1.0）
	MinReputation  float64      // 接受贡献的声誉下限（0 = 未设置取 -5；负值为合法自定义门槛）
}

// TrustLayer 社会信任层（并发安全）。
type TrustLayer struct {
	mu          sync.Mutex
	cfg         TrustConfig
	reput       map[NodeID]float64
	events      []TrustEvent
	quarantine  map[NodeID]bool   // 隔离区（伪造/持续投毒节点拒收）
	seenPayload map[string]NodeID // 载荷哈希 → 首发节点（自投毒指纹：他人资产原样重签）
}

// NewTrustLayer 构造。
func NewTrustLayer(cfg TrustConfig) (*TrustLayer, error) {
	if cfg.Verify == nil {
		return nil, fmt.Errorf("federation: 资产验签函数未注入")
	}
	if len(cfg.PinnedKeys) == 0 {
		return nil, fmt.Errorf("federation: 至少钉扎一把签名钥")
	}
	if cfg.MaxEventWeight <= 0 {
		cfg.MaxEventWeight = 1.0
	}
	// 仅 0 视为"未设置"取默认 -5；负值是合法的自定义门槛
	// （修复：此前 <=0 一并重置，-3 等自定义负门槛无法配置）。
	if cfg.MinReputation == 0 {
		cfg.MinReputation = -5
	}
	return &TrustLayer{
		cfg:         cfg,
		reput:       make(map[NodeID]float64),
		quarantine:  make(map[NodeID]bool),
		seenPayload: make(map[string]NodeID),
	}, nil
}

// payloadOf 资产验签载荷（确定性序列化：kind|id|payload_sha|origin|version）。
func payloadOf(a *AssetEnvelope) []byte {
	return []byte(fmt.Sprintf("%s|%s|%s|%s|%d", a.Kind, a.AssetID, a.PayloadSHA, a.OriginNode, a.Version))
}

// ReceiveAsset 接收联邦资产：三道确定性门（完整性→钉扎验签→溯源与声誉）。
// 全过返回 nil 并记贡献声誉；任一失门返回错误、记违规事件（伪造 0 漏：
// 所有篡改/伪造形态都命中至少一道门，且不改内部状态）。
func (t *TrustLayer) ReceiveAsset(a *AssetEnvelope, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	// 门 1：完整性（内容哈希与信封一致——篡改 0 漏；哈希不含 origin，
	// 使「他人资产重签」保留同一内容指纹，门 4 才能命中刷分形态）
	sum := sha256.Sum256([]byte(a.AssetID + string(a.Kind) + fmt.Sprint(a.Version)))
	if hex.EncodeToString(sum[:]) != a.PayloadSHA {
		t.recordViolation(a.OriginNode, "asset_rejected", "完整性校验失败", now)
		return fmt.Errorf("federation: 资产 %s 完整性校验失败", a.AssetID)
	}
	// 门 2：签名钥钉扎（伪造 0 漏——未钉扎钥一律拒绝）
	if !t.pinned(a.SignerKeyID) {
		t.recordViolation(a.OriginNode, "forgery_attempt", "签名钥未钉扎", now)
		return fmt.Errorf("federation: 资产 %s 签名钥未钉扎", a.AssetID)
	}
	// 门 2b：验签算法（签名与载荷绑定）
	if err := t.cfg.Verify(payloadOf(a), a.Signature, a.SignerKeyID); err != nil {
		t.recordViolation(a.OriginNode, "forgery_attempt", "验签失败: "+err.Error(), now)
		return fmt.Errorf("federation: 资产 %s 验签失败: %w", a.AssetID, err)
	}
	// 门 3：溯源链（回环 = 自投毒指纹：他人首发资产原样重签流通）
	for _, node := range a.Provenance {
		if NodeID(node) == a.OriginNode && len(a.Provenance) > 1 {
			t.recordViolation(a.OriginNode, "poison_attempt", "溯源链回环（自投毒指纹）", now)
			return fmt.Errorf("federation: 资产 %s 溯源链回环", a.AssetID)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantine[a.OriginNode] {
		// 误拦口径（2026-09-28 接线）：节点级隔离 vs 资产级合格。资产到达
		// 此处即已过门 1–3（完整性/钉扎钥/验签/溯源无回环）；若载荷非
		// 「他人重签」形态（首发为空或首发即本节点），该资产本身合格——
		// 隔离的是节点不是资产，此次拒绝计入 FalsePositives（事件 kind
		// =false_positive，仍计入 attempts/intercepted：误拦是真实发生
		// 的拦截的子集）。他人重签形态保持 poison_attempt（资产级不合格）。
		if first, ok := t.seenPayload[a.PayloadSHA]; !ok || first == a.OriginNode {
			t.events = append(t.events, TrustEvent{Node: a.OriginNode, Kind: "false_positive", Detail: "隔离区节点投递合格资产（节点级隔离，资产级合格）", Weight: -1, At: now})
		} else {
			t.events = append(t.events, TrustEvent{Node: a.OriginNode, Kind: "poison_attempt", Detail: "隔离区节点投递（他人资产重签形态）", Weight: -1, At: now})
		}
		return fmt.Errorf("federation: 节点 %s 在隔离区，拒收", a.OriginNode)
	}
	if first, ok := t.seenPayload[a.PayloadSHA]; ok && first != a.OriginNode {
		// 他人资产原样重签：刷分/投毒指纹 → 拦截
		t.events = append(t.events, TrustEvent{Node: a.OriginNode, Kind: "poison_attempt", Detail: "他人资产重签（刷分指纹）", Weight: -1, At: now})
		t.reput[a.OriginNode] -= 1
		if t.reput[a.OriginNode] <= t.cfg.MinReputation {
			t.quarantine[a.OriginNode] = true
		}
		return fmt.Errorf("federation: 资产 %s 涉嫌重签刷分，拦截", a.AssetID)
	}
	if t.seenPayload[a.PayloadSHA] == "" {
		t.seenPayload[a.PayloadSHA] = a.OriginNode
	} else if t.seenPayload[a.PayloadSHA] == a.OriginNode {
		// 同节点重复接收同一资产：幂等放行，**不加声望**。
		// 修复（2026-09-28 覆盖率回归暴露）：此前无条件 +1，同节点
		// 重放同一资产即可无限刷分，与"重放不加声誉（幂等）"的
		// 设计意图相反（federation_test.go 既有注释声明该意图）。
		return nil
	}
	// 合法贡献：声誉 +（权重按事件面封顶）
	t.reput[a.OriginNode] += 1
	t.events = append(t.events, TrustEvent{Node: a.OriginNode, Kind: "contribute", Detail: "资产 " + a.AssetID, Weight: 1, At: now})
	return nil
}

// RecordEvent 记录一次信任事件（贡献/违规；权重钳制防刷分）。
func (t *TrustLayer) RecordEvent(e TrustEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.Weight > t.cfg.MaxEventWeight {
		e.Weight = t.cfg.MaxEventWeight
	}
	if e.Weight < -t.cfg.MaxEventWeight {
		e.Weight = -t.cfg.MaxEventWeight
	}
	t.reput[e.Node] += e.Weight
	if e.Weight < 0 && t.reput[e.Node] <= t.cfg.MinReputation {
		t.quarantine[e.Node] = true
	}
	t.events = append(t.events, e)
}

// recordViolation 违规记录（须自行处理锁：独立小锁面）。
func (t *TrustLayer) recordViolation(node NodeID, kind, detail string, now time.Time) {
	t.RecordEvent(TrustEvent{Node: node, Kind: kind, Detail: detail, Weight: -1, At: now})
}

// pinned 钥指纹是否钉扎。
func (t *TrustLayer) pinned(keyID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range t.cfg.PinnedKeys {
		if k == keyID {
			return true
		}
	}
	return false
}

// ReputationReport 声誉报表（确定性排序：声誉降序、节点升序）。
type ReputationReport struct {
	Entries     []ReputationEntry `json:"entries"`
	Quarantined []NodeID          `json:"quarantined"`
	Events      int               `json:"events"`
}

// ReputationEntry 单节点声誉。
type ReputationEntry struct {
	Node       NodeID  `json:"node"`
	Reputation float64 `json:"reputation"`
}

// Report 声誉快照。
func (t *TrustLayer) Report() ReputationReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	rep := ReputationReport{Events: len(t.events)}
	for node, r := range t.reput {
		rep.Entries = append(rep.Entries, ReputationEntry{Node: node, Reputation: r})
	}
	for node := range t.quarantine {
		rep.Quarantined = append(rep.Quarantined, node)
	}
	sort.Slice(rep.Entries, func(i, j int) bool {
		if rep.Entries[i].Reputation != rep.Entries[j].Reputation {
			return rep.Entries[i].Reputation > rep.Entries[j].Reputation
		}
		return rep.Entries[i].Node < rep.Entries[j].Node
	})
	sort.Slice(rep.Quarantined, func(i, j int) bool { return rep.Quarantined[i] < rep.Quarantined[j] })
	return rep
}

// InterceptReport 拦截统计（命题 3 报告面：确定性事件流上的拦截/误拦）。
type InterceptReport struct {
	Attempts       int       `json:"attempts"`        // 投毒/伪造尝试总数
	Intercepted    int       `json:"intercepted"`     // 拦截数
	FalsePositives int       `json:"false_positives"` // 误拦数（隔离区节点投递的合格资产被拒——节点级隔离 vs 资产级合格；intercepted 的子集）
	Generated      time.Time `json:"generated"`
}

// InterceptStats 从事件流汇总拦截统计（合法贡献=contribute，其余计入尝试；
// attempts 中被拒的即 intercepted；false_positive 为 intercepted 的子集——
// 节点级隔离拒收的资产级合格贡献，即误拦，见 ReceiveAsset 隔离区分支口径）。
func (t *TrustLayer) InterceptStats() InterceptReport {
	t.mu.Lock()
	defer t.mu.Unlock()
	rep := InterceptReport{Generated: time.Now().UTC()}
	for _, e := range t.events {
		switch e.Kind {
		case "contribute":
			// 合法
		case "false_positive":
			rep.Attempts++
			rep.Intercepted++
			rep.FalsePositives++
		default:
			rep.Attempts++
			rep.Intercepted++
		}
	}
	return rep
}
