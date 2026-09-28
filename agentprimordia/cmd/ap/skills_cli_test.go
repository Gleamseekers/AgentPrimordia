package main

import (
	"os"
	"path/filepath"
	"testing"

	"agentprimordia/internal/agent/skills"
)

// TestSkillCLIAddListVerifyRemove 生产路径回归：ap skill 必须真实读写持久化技能库，
// 而非此前的打印占位实现。
func TestSkillCLIAddListVerifyRemove(t *testing.T) {
	oldDir := skillStoreDir
	skillStoreDir = t.TempDir()
	defer func() { skillStoreDir = oldDir }()

	// 准备技能文件
	skillFile := filepath.Join(t.TempDir(), "demo-skill.json")
	content := `{"name":"演示技能","steps":[{"id":"s1","tool_name":"echo"}]}`
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		t.Fatalf("写入技能文件失败: %v", err)
	}

	if err := runSkill([]string{"add", skillFile}); err != nil {
		t.Fatalf("skill add 失败: %v", err)
	}

	// 落盘 + 可被新实例加载（ID 由文件名兜底）
	if _, err := os.Stat(skillRegistryPath()); err != nil {
		t.Fatalf("技能库未持久化: %v", err)
	}
	store := newSkillStore()
	if _, ok := store.Get("demo-skill"); !ok {
		t.Fatalf("持久化后未找到技能 demo-skill，实际: %+v", store.List())
	}

	if err := runSkill([]string{"list"}); err != nil {
		t.Fatalf("skill list 失败: %v", err)
	}
	if err := runSkill([]string{"verify", "demo-skill"}); err != nil {
		t.Fatalf("skill verify 失败: %v", err)
	}
	if err := runSkill([]string{"remove", "demo-skill"}); err != nil {
		t.Fatalf("skill remove 失败: %v", err)
	}

	store2 := newSkillStore()
	if _, ok := store2.Get("demo-skill"); ok {
		t.Error("移除后重启仍能读到技能——未持久化")
	}
	if store2.Count() != 0 {
		t.Errorf("移除后技能数 = %d, want 0", store2.Count())
	}
}

// TestSkillCLIAddRejectsInvalid 非法技能必须被拒绝且不落库。
func TestSkillCLIAddRejectsInvalid(t *testing.T) {
	oldDir := skillStoreDir
	skillStoreDir = t.TempDir()
	defer func() { skillStoreDir = oldDir }()

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"name":"无步骤技能"}`), 0o644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := runSkill([]string{"add", bad}); err == nil {
		t.Fatal("无步骤技能应被拒绝")
	}
	if newSkillStore().Count() != 0 {
		t.Error("校验失败不应写入技能库")
	}
}

// TestSkillStorePersistenceWiredInCLI CLI 构造的 Store 必须注入持久化后端。
func TestSkillStorePersistenceWiredInCLI(t *testing.T) {
	oldDir := skillStoreDir
	skillStoreDir = t.TempDir()
	defer func() { skillStoreDir = oldDir }()

	store := newSkillStore()
	if store == nil {
		t.Fatal("newSkillStore 不应返回 nil")
	}
	if store.PersistError() != nil {
		t.Fatalf("全新技能库不应有持久化错误: %v", store.PersistError())
	}
	// 保存后应生成文件
	sk := skills.NewSkill("x", "d", []skills.StepDef{{ID: "s1", ToolName: "echo"}})
	sk.ID = "x"
	store.Save(sk)
	if _, err := os.Stat(skillRegistryPath()); err != nil {
		t.Fatalf("Save 未落盘: %v", err)
	}
}
