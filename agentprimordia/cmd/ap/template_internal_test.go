package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoTemplateReferencesInternal 守护全部脚手架模板不引用框架 internal/ 包。
//
// 背景：模板生成的是**用户项目**（独立 module），Go 的 internal 规则禁止用户
// import 依赖模块的 internal/——历史上 plugin 模板因此生成的项目在仓库外
// 永远编译不过（2026-10-12 模板普查实证，本测试即该发现的回归防线）。
// 参照：quickstart 模板的同款断言在 TestRunInit_BasicTemplate，本测试将其
// 推广到 cmd/ap/scaffold/ 下全部模板。
func TestNoTemplateReferencesInternal(t *testing.T) {
	const internalPath = "agentprimordia/internal/"
	templates, err := os.ReadDir("scaffold")
	if err != nil {
		t.Fatalf("读取 scaffold 目录失败: %v", err)
	}
	if len(templates) == 0 {
		t.Fatal("scaffold 目录为空，模板普查失去意义")
	}
	checked := 0
	for _, e := range templates {
		if !e.IsDir() {
			continue
		}
		tplDir := filepath.Join("scaffold", e.Name())
		_ = filepath.Walk(tplDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Errorf("读取 %s 失败: %v", path, readErr)
				return nil
			}
			checked++
			if strings.Contains(string(data), internalPath) {
				t.Errorf("模板 %s 引用了框架 internal/ 包（%s）——用户项目无法 import 依赖模块的 internal/，请在 pkg/ 公共面找等价符号", path, internalPath)
			}
			return nil
		})
	}
	if checked == 0 {
		t.Fatal("未检查到任何模板 .go 文件")
	}
	t.Logf("已检查 %d 个模板 Go 文件", checked)
}
