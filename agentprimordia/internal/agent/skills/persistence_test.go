package skills

import (
	"path/filepath"
	"testing"
)

func persistTestSkill(id string) *Skill {
	sk := NewSkill("技能-"+id, "测试技能", []StepDef{{ID: "s1", ToolName: "echo"}})
	sk.ID = id
	return sk
}

// TestSkillsJSONFileStoreRoundTrip 持久化后端保存/加载往返。
func TestSkillsJSONFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")
	fs := NewJSONFileStore(path)

	if err := fs.Save([]*Skill{persistTestSkill("a"), persistTestSkill("b")}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	out, err := fs.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("技能数 = %d, want 2", len(out))
	}
	if out[0].ID != "a" || out[1].ID != "b" {
		t.Errorf("往返内容错误: %+v", out)
	}
}

// TestSkillsStorePersistsAndReloads Save/Delete 必须落盘，并可被新实例加载。
func TestSkillsStorePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skills.json")

	s1 := NewStore(WithPersistence(NewJSONFileStore(path)))
	s1.Save(persistTestSkill("a"))
	s1.Save(persistTestSkill("b"))
	if err := s1.PersistError(); err != nil {
		t.Fatalf("持久化失败: %v", err)
	}

	// 模拟重启
	s2 := NewStore(WithPersistence(NewJSONFileStore(path)))
	if got := s2.Count(); got != 2 {
		t.Fatalf("重启后技能数 = %d, want 2", got)
	}
	if _, ok := s2.Get("a"); !ok {
		t.Error("重启后应存在技能 a")
	}

	// 删除持久化
	s2.Delete("a")
	s3 := NewStore(WithPersistence(NewJSONFileStore(path)))
	if _, ok := s3.Get("a"); ok {
		t.Error("删除未持久化")
	}
	if got := s3.Count(); got != 1 {
		t.Errorf("删除后技能数 = %d, want 1", got)
	}
}

// TestSkillsStoreMissingFileEmpty 文件不存在视为空库而非错误。
func TestSkillsStoreMissingFileEmpty(t *testing.T) {
	s := NewStore(WithPersistence(NewJSONFileStore(filepath.Join(t.TempDir(), "none.json"))))
	if err := s.PersistError(); err != nil {
		t.Fatalf("缺失文件不应报错: %v", err)
	}
	if s.Count() != 0 {
		t.Errorf("空库技能数 = %d, want 0", s.Count())
	}
}

// TestSkillsStoreWithoutPersistenceUnchanged 未注入持久化时行为不变（内存模式）。
func TestSkillsStoreWithoutPersistenceUnchanged(t *testing.T) {
	s := NewStore()
	s.Save(persistTestSkill("a"))
	if s.Count() != 1 {
		t.Errorf("内存模式技能数 = %d, want 1", s.Count())
	}
	if s.PersistError() != nil {
		t.Errorf("未注入持久化时 PersistError 应为 nil，实际 %v", s.PersistError())
	}
}
