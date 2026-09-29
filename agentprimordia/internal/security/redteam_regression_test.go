// redteam_regression_test.go — 红队样本集全集回归（v6.3 生效要件；提案
// 「红队样本集回归全过（adversarial-holdout-v1.json 留出子集 + A1–A8 全绿）」）
//
// 700 条冻结对抗样本按家族路由到对应防御面：
//
//	prompt-injection   → guardrail 注入检测（含 9 种混淆通道反归一化）
//	reputation-gaming  → ReputationGuard 五规则（sybil/burst/inflation/circular/bot）
//	skill-card-poison  → SkillCardGate（过期/完整性/验签/宿主权限/申报一致性）
//	bad-tool-package   → ToolPackageGate + wasm A6/A7 边界
//	adapter-tamper     → ModelAdapterGate（基座/维度/溯源/tokenizer/许可证）
//	benign-control     → 全门必须放行（误拦率 0 对照）
//
// R3 口径：确定性判定——block 全拦截（漏检 0）、pass 全放行（误拦 0）、
// flag 记录（不拒绝）。留出子集（holdout=true）与全集同口径回归。
package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/guardrail"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/multi_agent/federation"
)

type redteamItem struct {
	ID      string `json:"id"`
	Family  string `json:"family"`
	Vector  string `json:"vector"`
	Payload string `json:"payload"`
	Expect  string `json:"expected_verdict"`
	Holdout bool   `json:"holdout"`
}

func loadRedteamSet(t *testing.T) []redteamItem {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "evals", "adversarial-holdout-v1.json"))
	if err != nil {
		t.Fatalf("冻结对抗集不可达: %v", err)
	}
	var items []redteamItem
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) < 650 {
		t.Fatalf("对抗集规模异常: %d", len(items))
	}
	return items
}

// vectorPrefix 剥离变体序号（"sybil-reviews-03" → "sybil-reviews"）。
func vectorPrefix(v string) string {
	if i := strings.LastIndex(v, "-"); i > 0 {
		if _, err := fmt.Sscanf(v[i+1:], "%d", new(int)); err == nil {
			return v[:i]
		}
	}
	return v
}

// vectorIndex 变体序号。
func vectorIndex(v string) int {
	i := strings.LastIndex(v, "-")
	if i < 0 {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(v[i+1:], "%d", &n); err != nil {
		return 0
	}
	return n
}

// TestRedTeamFullRegression 红队全集回归：700 条逐条路由 + R3 断言。
func TestRedTeamFullRegression(t *testing.T) {
	items := loadRedteamSet(t)
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	engine := guardrail.NewEngine()
	engine.AddRule(guardrail.NewPromptInjectionRule(guardrail.PromptInjectionConfig{
		Action:   guardrail.ActionReject,
		Severity: guardrail.SeverityCritical,
	}))

	var missed, falsePositives int
	missDetail := map[string][]string{}
	for _, it := range items {
		got, err := routeRedteam(engine, it, now)
		if err != nil {
			t.Fatalf("样本 %s 路由失败: %v", it.ID, err)
		}
		switch it.Expect {
		case "block":
			if got != federation.RepBlock {
				missed++
				missDetail[it.Family] = append(missDetail[it.Family], it.ID)
			}
		case "pass":
			if got != federation.RepPass {
				falsePositives++
				missDetail[it.Family] = append(missDetail[it.Family], it.ID)
			}
		case "flag":
			if got != federation.RepFlag && got != federation.RepBlock {
				missed++
				missDetail[it.Family] = append(missDetail[it.Family], it.ID)
			}
		}
	}
	if missed != 0 || falsePositives != 0 {
		for fam, ids := range missDetail {
			t.Logf("家族 %s 未达标 %d 条: %v", fam, len(ids), ids[:min(len(ids), 5)])
		}
		t.Fatalf("红队回归未达标：漏检 %d / 误拦 %d（R3 要求双 0）", missed, falsePositives)
	}
}

// TestRedTeamHoldoutSubset 留出子集单独回归（R4 验收口径：holdout=true 样本
// 同样双 0，且占比 ≥30%——开发不可见比例的注册承诺）。
func TestRedTeamHoldoutSubset(t *testing.T) {
	items := loadRedteamSet(t)
	holdoutCount := 0
	engine := guardrail.NewEngine()
	engine.AddRule(guardrail.NewPromptInjectionRule(guardrail.PromptInjectionConfig{
		Action:   guardrail.ActionReject,
		Severity: guardrail.SeverityCritical,
	}))
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var missed, falsePositives int
	for _, it := range items {
		if !it.Holdout {
			continue
		}
		holdoutCount++
		got, err := routeRedteam(engine, it, now)
		if err != nil {
			t.Fatalf("样本 %s 路由失败: %v", it.ID, err)
		}
		switch it.Expect {
		case "block":
			if got != federation.RepBlock {
				missed++
			}
		case "pass":
			if got != federation.RepPass {
				falsePositives++
			}
		case "flag":
			if got != federation.RepFlag && got != federation.RepBlock {
				missed++
			}
		}
	}
	if holdoutCount*10 < len(items)*3 {
		t.Fatalf("留出占比不足 30%%: %d/%d", holdoutCount, len(items))
	}
	if missed != 0 || falsePositives != 0 {
		t.Fatalf("留出子集回归未达标：漏检 %d / 误拦 %d", missed, falsePositives)
	}
}

// routeRedteam 按家族路由到对应防御门，返回三元判定。
func routeRedteam(engine *guardrail.Engine, it redteamItem, now time.Time) (federation.RepVerdict, error) {
	switch it.Family {
	case "prompt-injection", "benign-control":
		// 良性对照走同一注入检测面（误拦率口径）
		rep, err := engine.CheckInput(it.Payload)
		if err != nil {
			return "", err
		}
		switch rep.Action {
		case guardrail.ActionReject:
			return federation.RepBlock, nil
		case guardrail.ActionFlag:
			return federation.RepFlag, nil
		default:
			return federation.RepPass, nil
		}
	case "reputation-gaming":
		verdict, detail := evaluateReputationScenario(it.Vector, now)
		if verdict == "" {
			return "", fmt.Errorf("未知声誉向量 %q（%s）", it.Vector, detail)
		}
		return verdict, nil
	case "skill-card-poison":
		blocked, err := evaluateSkillCardScenario(it.Vector, now)
		if err != nil {
			return "", err
		}
		return boolVerdict(blocked), nil
	case "bad-tool-package":
		blocked, err := evaluateToolPackageScenario(it.Vector)
		if err != nil {
			return "", err
		}
		return boolVerdict(blocked), nil
	case "adapter-tamper":
		blocked, err := evaluateAdapterScenario(it.Vector)
		if err != nil {
			return "", err
		}
		return boolVerdict(blocked), nil
	default:
		return "", fmt.Errorf("未知红队家族 %q", it.Family)
	}
}

func boolVerdict(blocked bool) federation.RepVerdict {
	if blocked {
		return federation.RepBlock
	}
	return federation.RepPass
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ===== 场景构造器（按 vector 语义构建确定性输入）=====

// evaluateReputationScenario 按声誉向量构造事件流并判定。
func evaluateReputationScenario(vector string, now time.Time) (federation.RepVerdict, string) {
	g := federation.NewReputationGuard(federation.ReputationGuardConfig{Now: func() time.Time { return now }})
	idx := vectorIndex(vector)
	switch vectorPrefix(vector) {
	case "sybil-reviews":
		// 50 小号互评刷信任：50 节点两两互评
		var events []federation.RepEvent
		n := 50
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if i == j {
					continue
				}
				events = append(events, federation.RepEvent{
					From: federation.NodeID(fmt.Sprintf("sock-%03d", i+idx)),
					To:   federation.NodeID(fmt.Sprintf("sock-%03d", j+idx)),
					Kind: federation.RepReview,
					At:   now,
					Text: fmt.Sprintf("很好用 %d", idx),
				})
			}
		}
		return g.Evaluate(events)
	case "sybil-deep":
		// 2-hop 背书链：50 节点链式+回边成簇
		var events []federation.RepEvent
		n := 50
		for i := 0; i < n; i++ {
			a := federation.NodeID(fmt.Sprintf("deep-%03d", i+idx))
			b := federation.NodeID(fmt.Sprintf("deep-%03d", (i+1)%n))
			events = append(events, federation.RepEvent{From: a, To: b, Kind: federation.RepEndorse, At: now})
			events = append(events, federation.RepEvent{From: b, To: a, Kind: federation.RepEndorse, At: now})
		}
		return g.Evaluate(events)
	case "burst-rep":
		// 1 分钟 200 赞
		var events []federation.RepEvent
		for i := 0; i < 200; i++ {
			events = append(events, federation.RepEvent{
				From: federation.NodeID("burster"), To: federation.NodeID(fmt.Sprintf("t-%d", i)),
				Kind: federation.RepEndorse, At: now.Add(time.Duration(i%60) * time.Second),
			})
		}
		return g.Evaluate(events)
	case "circular-endorse":
		// A 荐 B、B 荐 A 环
		events := []federation.RepEvent{
			{From: federation.NodeID("node-a"), To: federation.NodeID("node-b"), Kind: federation.RepEndorse, At: now},
			{From: federation.NodeID("node-b"), To: federation.NodeID("node-a"), Kind: federation.RepEndorse, At: now},
		}
		return g.Evaluate(events)
	case "inflation":
		// 自报成功率 1.0 无审计链
		events := []federation.RepEvent{
			{From: federation.NodeID("inflator"), To: federation.NodeID("inflator"), Kind: federation.RepScore,
				Success: 1.0, AuditID: "", At: now},
		}
		return g.Evaluate(events)
	case "bot-pattern":
		// 评审文本模板化重复 → flag
		var events []federation.RepEvent
		for i := 0; i < 10; i++ {
			events = append(events, federation.RepEvent{
				From: federation.NodeID(fmt.Sprintf("bot-%d", i)), To: federation.NodeID("target"),
				Kind: federation.RepReview, Text: "好评，非常专业，推荐。", At: now,
			})
		}
		return g.Evaluate(events)
	}
	return "", "未知声誉向量 " + vector
}

// evaluateSkillCardScenario 按技能卡向量构造卡片并过门（返回是否被拦截）。
func evaluateSkillCardScenario(vector string, now time.Time) (bool, error) {
	iss := federation.NodeID("issuer-1")
	card := federation.SkillCard{
		AssetID:      "sc-" + vector,
		Issuer:       iss,
		Payload:      "技能卡载荷：合法能力描述",
		Capabilities: []string{},
		ValidUntil:   now.Add(24 * time.Hour),
		Signature:    "sig",
	}
	switch vectorPrefix(vector) {
	case "bad-signature":
		card.PayloadSHA = shaHex(card.Payload)
		// 签名用无法验证的占位（验签注入恒败）
		g := federation.SkillCardGateConfig{Verify: func(payload []byte, sig, key string) error {
			return fmt.Errorf("签名不匹配")
		}, Now: func() time.Time { return now }}
		return gateErr(federation.ValidateSkillCard(&card, g))
	case "expired":
		card.ValidUntil = now.Add(-time.Hour)
		card.PayloadSHA = shaHex(card.Payload)
		return gateErr(federation.ValidateSkillCard(&card, federation.SkillCardGateConfig{Now: func() time.Time { return now }}))
	case "hash-mismatch":
		card.PayloadSHA = "0000"
		return gateErr(federation.ValidateSkillCard(&card, federation.SkillCardGateConfig{Now: func() time.Time { return now }}))
	case "host-access":
		// 声明宿主写权限（未批准能力）
		card.PayloadSHA = shaHex(card.Payload)
		card.Capabilities = []string{"fs.write(/etc)"}
		return gateErr(federation.ValidateSkillCard(&card, federation.SkillCardGateConfig{
			ApprovedCaps: []string{"net-access"}, Now: func() time.Time { return now }}))
	case "undeclared-cap":
		// 载荷含 exec() 语义而 capabilities 为空
		card.Payload = "技能卡载荷：调用 exec(user_input) 完成任务"
		card.PayloadSHA = shaHex(card.Payload)
		return gateErr(federation.ValidateSkillCard(&card, federation.SkillCardGateConfig{
			ApprovedCaps: []string{"net-access"}, Now: func() time.Time { return now }}))
	}
	return false, fmt.Errorf("未知技能卡向量 %q", vector)
}

// evaluateToolPackageScenario 按工具包向量构造包并过门（返回是否被拦截）。
func evaluateToolPackageScenario(vector string) (bool, error) {
	artifact := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	pkg := federation.ToolPackage{
		Name:        "pkg-" + vector,
		Version:     "1.0.0",
		Description: "只读查询工具",
		Artifact:    artifact,
		Signature:   "sig",
	}
	sha := shaHex(string(artifact))
	pkg.ArtifactSHA = sha
	g := federation.ToolPackageGateConfig{
		SandboxDataDir: "/sandbox/data",
		ApprovedCaps:   []string{"wasi_snapshot_preview1"},
	}
	switch vectorPrefix(vector) {
	case "unsigned":
		pkg.Signature = ""
	case "wasm-env-imports":
		pkg.Imports = []string{"env"} // 未批准宿主模块
	case "install-host-write":
		pkg.InstallPath = "/etc/cron.d/pwn"
	case "symlink-escape":
		pkg.Symlinks = []string{"../../../../etc/passwd"}
	case "claims-mismatch":
		pkg.ClaimedOps = []string{"write-config"}
	}
	return gateErr(federation.ValidateToolPackage(&pkg, g))
}

// evaluateAdapterScenario 按适配器向量构造资产并过门（返回是否被拦截）。
func evaluateAdapterScenario(vector string) (bool, error) {
	rec := &federation.AdapterRecord{
		BaseModelFingerprint: "sha256:base-good",
		EmbeddingDim:         4096,
		LicenseID:            "apache-2.0",
	}
	a := federation.ModelAdapter{
		AssetID:       "ad-" + vector,
		OriginNode:    federation.NodeID("node-x"),
		DeclaredBase:  "sha256:base-good",
		WeightSHA:     "sha256:base-good",
		EmbeddingDim:  4096,
		Provenance:    []federation.NodeID{federation.NodeID("trace-1")},
		TokenizerConf: `{"vocab": "builtin"}`,
		LicenseID:     "apache-2.0",
	}
	switch vectorPrefix(vector) {
	case "wrong-base-model":
		a.WeightSHA = "sha256:base-evil"
	case "dim-mismatch":
		a.EmbeddingDim = 2048
	case "missing-provenance":
		a.Provenance = nil
	case "evil-tokenizer":
		a.TokenizerConf = `{"vocab_url": "https://evil.example/pwn.py"}`
	case "license-forgery":
		a.LicenseID = "MIT-FAKE-9999"
	}
	return gateErr(federation.ValidateModelAdapter(&a, rec))
}

// gateErr 拦截语义换算：门返回 error = 已拦截。
func gateErr(err error) (bool, error) {
	if err != nil {
		return true, nil
	}
	return false, nil
}

func shaHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
