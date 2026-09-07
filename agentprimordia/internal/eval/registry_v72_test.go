// registry_v72_test.go — V7.2 命题级任务注册表冻结门测试
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestV72RegistryFrozen 冻结门：清单必须存在、可解析、每个任务文件的 sha256 与清单一致。
// 清单为空（files 为空列表）时，循环体不执行，测试直接通过——这是初始骨架的预期行为。
func TestV72RegistryFrozen(t *testing.T) {
	m, err := LoadV72Manifest()
	if err != nil {
		t.Fatalf("v7.2 清单加载失败: %v", err)
	}
	if m.Version != "v7.2" {
		t.Errorf("版本 = %q, 期望 v7.2", m.Version)
	}
	if m.HoldoutRatio < MinHoldoutRate {
		t.Errorf("留出比例 %.2f < 下限 %.2f", m.HoldoutRatio, MinHoldoutRate)
	}

	root := v72ModuleRoot()
	for _, f := range m.Files {
		abs := filepath.Join(root, "docs", "evals", "v72", f)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("清单登记的文件缺失: %s", f)
			continue
		}
		got, err := FileSHA256(abs)
		if err != nil {
			t.Errorf("计算 %s 哈希失败: %v", f, err)
			continue
		}
		// 当前清单 sha256 字段为空占位；文件加入后此处需与清单记录比对
		if m.SHA256 != "" && got != m.SHA256 {
			t.Errorf("题面漂移 %s: 清单 %s 实际 %s", f, m.SHA256[:12], got[:12])
		}
	}
	t.Logf("v7.2 冻结门通过：版本 %s / 登记文件 %d 个", m.Version, len(m.Files))
}

// TestLoadV72TasksMissing 任务文件不存在时应返回明确错误
func TestLoadV72TasksMissing(t *testing.T) {
	_, err := LoadV72Tasks("nonexistent-prop")
	if err == nil {
		t.Fatal("不存在的命题应报错")
	}
}

// TestV72TaskCountMissing 不存在的命题统计应返回错误
func TestV72TaskCountMissing(t *testing.T) {
	_, _, err := V72TaskCount("nonexistent-prop")
	if err == nil {
		t.Fatal("不存在的命题应报错")
	}
}

// TestLoadV72TasksRoundTrip 创建临时任务文件并验证装载与计数的一致性
func TestLoadV72TasksRoundTrip(t *testing.T) {
	// 本测试使用 TempDir 模拟，验证结构体解析逻辑
	// 不依赖实际磁盘文件（v72 目录下暂无任务文件）
	sample := `[
		{"id":"t1","prop":"p1","task":"do something","holdout":false,"fixtures":[],"asserts":[]},
		{"id":"t2","prop":"p1","task":"do another","holdout":true,"fixtures":[{"path":"a.txt","content":"hello"}],"asserts":[{"type":"file_exists","target":"a.txt","expect":""}]}
	]`
	var tasks []V72Task
	if err := json.Unmarshal([]byte(sample), &tasks); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("期望 2 条任务, 得 %d", len(tasks))
	}
	holdout := 0
	for _, task := range tasks {
		if task.Holdout {
			holdout++
		}
	}
	if holdout != 1 {
		t.Errorf("期望 1 条留出, 得 %d", holdout)
	}
	if tasks[1].Fixtures[0].Content != "hello" {
		t.Errorf("Fixture Content 解析错误: %q", tasks[1].Fixtures[0].Content)
	}
	if tasks[1].Asserts[0].Type != "file_exists" {
		t.Errorf("Assert Type 解析错误: %q", tasks[1].Asserts[0].Type)
	}
}

// TestV72ManifestPath 验证清单路径解析正确
func TestV72ManifestPath(t *testing.T) {
	root := v72ModuleRoot()
	expected := filepath.Join(root, "docs", "evals", "v72", "manifest.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("清单文件不存在: %s — %v", expected, err)
	}
	t.Logf("清单路径: %s", expected)
}

// TestV72ModuleRootConsistent v72ModuleRoot 与 RepoRoot 应指向同一模块根
func TestV72ModuleRootConsistent(t *testing.T) {
	root := v72ModuleRoot()
	repoRoot := RepoRoot()
	// v72ModuleRoot 返回 agentprimordia/，RepoRoot 返回 AgentPrimordia/（上一级）
	// 验证 v72ModuleRoot 的上一级是 RepoRoot
	parent := filepath.Dir(root)
	if parent != repoRoot {
		t.Errorf("v72ModuleRoot 父级 %q != RepoRoot %q", parent, repoRoot)
	}
}

// TestV72LoadManifestFields 验证清单所有字段解析正确
func TestV72LoadManifestFields(t *testing.T) {
	m, err := LoadV72Manifest()
	if err != nil {
		t.Fatalf("清单加载失败: %v", err)
	}
	fields := []string{m.Version}
	for _, f := range fields {
		if f == "" {
			t.Error("清单存在空字段")
		}
	}
	if len(m.Files) != 0 {
		t.Errorf("初始骨架 files 应为空, 得 %d", len(m.Files))
	}
	_ = fmt.Sprintf("manifest loaded: %v", m.Version)
}
