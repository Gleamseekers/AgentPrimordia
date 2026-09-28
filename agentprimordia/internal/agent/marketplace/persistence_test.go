package marketplace

import (
	"path/filepath"
	"testing"
)

func testTemplate(id string) *AgentTemplate {
	return &AgentTemplate{
		ID:           id,
		Name:         "模板-" + id,
		Version:      "1.0.0",
		Author:       "tester",
		SystemPrompt: "you are helpful",
	}
}

// TestJSONFileStoreRoundTrip 持久化后端保存/加载往返。
func TestJSONFileStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	s := NewJSONFileStore(path)

	if err := s.Save([]*AgentTemplate{testTemplate("a"), testTemplate("b")}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	out, err := s.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("加载模板数 = %d, want 2", len(out))
	}
	if out[0].ID != "a" || out[1].ID != "b" {
		t.Errorf("往返顺序/内容错误: %+v", out)
	}
}

// TestJSONFileStoreMissingFileReturnsEmpty 文件不存在应视为空目录而非错误。
func TestJSONFileStoreMissingFileReturnsEmpty(t *testing.T) {
	s := NewJSONFileStore(filepath.Join(t.TempDir(), "nope.json"))
	out, err := s.Load()
	if err != nil {
		t.Fatalf("缺失文件不应报错: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("缺失文件应返回空集合，实际 %d", len(out))
	}
}

// TestRegistryPersistsMutationsAndReloads 注册/更新/注销必须落盘，并可被新实例加载。
func TestRegistryPersistsMutationsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")

	r1 := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	if err := r1.Register(testTemplate("a")); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if err := r1.Register(testTemplate("b")); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}

	// 新实例（模拟进程重启）应自动加载
	r2 := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	if r2.LoadError() != nil {
		t.Fatalf("加载失败: %v", r2.LoadError())
	}
	if got := len(r2.List()); got != 2 {
		t.Fatalf("重启后模板数 = %d, want 2", got)
	}

	// 更新持久化
	upd := testTemplate("a")
	upd.Description = "updated"
	if err := r2.Update(upd); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	r3 := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	if got, _ := r3.Get("a"); got == nil || got.Description != "updated" {
		t.Errorf("更新未持久化: %+v", got)
	}

	// 注销持久化
	if err := r3.Unregister("b"); err != nil {
		t.Fatalf("Unregister 失败: %v", err)
	}
	r4 := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	if _, ok := r4.Get("b"); ok {
		t.Error("注销未持久化")
	}
	if got := len(r4.List()); got != 1 {
		t.Errorf("注销后模板数 = %d, want 1", got)
	}
}

// TestRegistryPersistsRatingAndDownloads 评分与下载计数也应落盘。
func TestRegistryPersistsRatingAndDownloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	r := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	if err := r.Register(testTemplate("a")); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if err := r.RateTemplate("a", 4); err != nil {
		t.Fatalf("RateTemplate 失败: %v", err)
	}
	r.IncrementDownloads("a")

	r2 := NewTemplateRegistry(WithStore(NewJSONFileStore(path)))
	got, _ := r2.Get("a")
	if got == nil {
		t.Fatal("应加载到模板 a")
	}
	if got.Rating != 4 {
		t.Errorf("评分未持久化: %v, want 4", got.Rating)
	}
	if got.Downloads != 1 {
		t.Errorf("下载计数未持久化: %d, want 1", got.Downloads)
	}
}

// TestRegistryWithoutStoreStillWorks 未注入存储时行为不变（内存模式，向后兼容）。
func TestRegistryWithoutStoreStillWorks(t *testing.T) {
	r := NewTemplateRegistry()
	if err := r.Register(testTemplate("a")); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}
	if got := len(r.List()); got != 1 {
		t.Errorf("内存模式模板数 = %d, want 1", got)
	}
	if r.LoadError() != nil {
		t.Errorf("未注入存储时 LoadError 应为 nil，实际 %v", r.LoadError())
	}
}
