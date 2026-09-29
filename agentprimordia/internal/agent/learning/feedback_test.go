// feedback_test.go — v5.4 自进化闭环：结果反馈回路 + 自改进安全边界测试
package learning

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/persist"
)

// fakeFailureStore 测试用失败库
type fakeFailureStore struct {
	mu    sync.Mutex
	items []*persist.FailureRecord
}

func (f *fakeFailureStore) Record(_ context.Context, rec *persist.FailureRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, rec)
	return nil
}

// fakeSkillSynth 测试用技能合成器
type fakeSkillSynth struct{ calls int }

func (s *fakeSkillSynth) Synthesize(_ context.Context, _, _ string) (string, error) {
	s.calls++
	return "skill-1", nil
}

func newLoop() (*FeedbackLoop, *fakeFailureStore, *fakeSkillSynth) {
	fs := &fakeFailureStore{}
	ss := &fakeSkillSynth{}
	loop := NewFeedbackLoop(memory.NewSelfModel(), fs, ss)
	return loop, fs, ss
}

func TestRecordOutcomeWritesBothSides(t *testing.T) {
	loop, fs, _ := newLoop()
	loop.RecordOutcome(context.Background(), Outcome{
		Domain: "deploy", Success: false, Turns: 6,
		FailureSignature: "image_pull_backoff", ErrorText: "ImagePullBackOff", Input: "部署服务",
	})
	if len(fs.items) != 1 {
		t.Fatalf("失败应入库，得到 %d", len(fs.items))
	}
	if fs.items[0].Error != "ImagePullBackOff" {
		t.Errorf("错误文本应保留: %q", fs.items[0].Error)
	}
	weak := loop.Model().WeakDomains(1, 60)
	if len(weak) != 1 || weak[0].Domain != "deploy" {
		t.Errorf("画像应记录弱项: %+v", weak)
	}
}

func TestSuggestFromMitigationAndProfile(t *testing.T) {
	loop, _, _ := newLoop()
	loop.Model().RecordOutcome("deploy", false, 5, "image_pull_backoff")
	loop.Model().SetMitigation("image_pull_backoff", "预先拉取镜像")

	sugg := loop.Suggest("deploy", "image_pull_backoff")
	if len(sugg) == 0 {
		t.Fatal("已知缓解手段应生成建议")
	}
	found := false
	for _, s := range sugg {
		if s.Scope == ScopePrompt && strings.Contains(s.Description, "预先拉取镜像") {
			found = true
		}
		if s.Scope == ScopeCode {
			t.Error("任何建议不得触碰 code 层")
		}
	}
	if !found {
		t.Error("应包含 prompt 层建议（缓解手段→提示词增强）")
	}
}

func TestSafetyBoundaryCodeScopeRejected(t *testing.T) {
	loop, _, _ := newLoop()
	err := loop.Propose(Suggestion{Scope: ScopeCode, Description: "重构 react_loop.go"})
	if !errors.Is(err, ErrImprovementScopeViolation) {
		t.Fatalf("code 层改进必须被沙箱拦截，得到 %v", err)
	}
}

func TestApplyRequiresHumanApproval(t *testing.T) {
	loop, _, _ := newLoop()
	s := Suggestion{ID: "s1", Scope: ScopePrompt, Description: "增加验证步骤提示", Payload: map[string]string{"append": "完成后自检"}}
	if err := loop.Propose(s); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.ApplyApproved(context.Background()); err != nil || loop.appliedCount() != 0 {
		t.Fatalf("未经批准不得应用: %v", err)
	}
	loop.Approve("s1")
	applied, err := loop.ApplyApproved(context.Background())
	if err != nil || len(applied) != 1 || applied[0].ID != "s1" {
		t.Fatalf("批准后应应用: %v %v", applied, err)
	}
}

func TestSuccessTrajectorySynthesizesSkill(t *testing.T) {
	loop, _, ss := newLoop()
	// 高轮数成功任务 → 建议合成技能（复用层）
	loop.RecordOutcome(context.Background(), Outcome{
		Domain: "sql_migration", Success: true, Turns: 14,
		TrajectorySummary: "备份→分批迁移→校验行数→切换",
	})
	sugg := loop.Suggest("sql_migration", "")
	anySkill := false
	for _, s := range sugg {
		if s.Scope == ScopeSkill {
			anySkill = true
			if err := loop.Propose(s); err != nil {
				t.Fatal(err)
			}
			loop.Approve(s.ID)
		}
	}
	if !anySkill {
		t.Fatal("高轮数成功任务应触发技能合成建议")
	}
	applied, err := loop.ApplyApproved(context.Background())
	if err != nil || len(applied) == 0 {
		t.Fatalf("技能建议应可批准应用: %v", err)
	}
	if ss.calls == 0 {
		t.Error("应用时应调用技能合成器")
	}
}

func TestConcurrentRecordSafe(t *testing.T) {
	loop, fs, _ := newLoop()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			loop.RecordOutcome(context.Background(), Outcome{
				Domain: "d", Success: i%2 == 0, Turns: 2,
				FailureSignature: "sig", ErrorText: "e",
			})
		}(i)
	}
	wg.Wait()
	if len(fs.items) != 15 {
		t.Errorf("15 次失败应全部入库，得到 %d", len(fs.items))
	}
}
