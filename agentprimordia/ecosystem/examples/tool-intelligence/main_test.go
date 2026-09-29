// tool-intelligence 示例冒烟测试：
//  1. TestDemoOutputSmoke — 运行 main() 捕获 stdout，固化演示输出关键标记
//     （5 个 ✓ + INV-0 演示段 + 汇总段），防止迁移或重构改变演示行为；
//  2. TestExampleNoInternalImports — 固化 AGENTS.md §4.2：ecosystem 示例仅经
//     pkg 公共 API 交互，不得直依赖 internal/*。
package main

import (
	"os"
	"strings"
	"testing"
)

// wantMarkers 演示输出的关键标记（与迁移前 go run 输出一致）。
//
// 注意：缺口检测段（"检测到缺口: N 个"及各缺口行）在迁移前即非确定性——
// create.extractGapKey 以 map 迭代匹配错误模式，同一错误可命中多个 pattern
// （如 "parse error: invalid CSV format" 同时含 "parse error"/"invalid format"），
// 故本测试只固化确定性标记，不断言缺口数量/键名/SHA。
var wantMarkers = []string{
	"=== AgentPrimordia v7.1 统一工具智能系统演示 ===",
	"✓ 所有组件构造完成",
	"  - Profiler: InMemoryProfiler",
	"  - Creator: LifecycleCreator",
	"  - Catalog: reuse.ToolCatalog",
	"  已注册工具: 3 个",
	"  任务: \"解析 CSV 数据并提取关键字段\"",
	"  最佳匹配: csv_parse",
	"  file_read: 调用 5 次 | 成功率 80% | 平均延迟 92ms | P95 200ms",
	"  slow_api:  调用 3 次 | 成功率 100% | 平均延迟 4.333s | P95 6s",
	"  file_read: 表现良好，无需调优",
	"  slow_api:  表现良好，无需调优",
	"  候选: [bash_exec shell_cmd python_exec]",
	"  选中: python_exec（历史成功率最高）",
	"--- 缺口检测与工具生成 ---",
	"  自动工具生成:",
	"--- IntelligenceHook 桥接 ---",
	"  当前轨迹长度: 3",
	"  轮次结束 → 触发缺口检测...",
	"✓ Hook 已完成轮次收尾（缺口已检测并尝试自动创建工具）",
	"  轮次后轨迹长度: 0（已清空，等待下一轮）",
	"--- INV-0 合规注册（RegisteringCreator）---",
	"✓ 未签名工件被拒绝注册（fail-closed，符合 INV-0）",
	"✓ 已签名工件注册成功（验签门放行）",
	"✓ 执行通道：未注入 wasm executor 时拒绝宿主执行",
	"    （生产装配：cmd/ap/tool_forge.go 绑定 WASMToolAdapter）",
	"  Profiler 追踪工具数: 3",
	"  Selector 统计工具数: 3 (bash_exec, shell_cmd, python_exec)",
	"  Catalog 注册工具数: 3",
	"=== 演示完成 ===",
}

// TestDemoOutputSmoke 运行 main() 并校验输出包含全部关键标记、✓ 数恰为 5。
func TestDemoOutputSmoke(t *testing.T) {
	out := captureMain(t)

	for _, marker := range wantMarkers {
		if !strings.Contains(out, marker) {
			t.Errorf("演示输出缺少关键标记 %q", marker)
		}
	}
	if got := strings.Count(out, "✓"); got != 5 {
		t.Errorf("✓ 标记数 = %d，want 5（迁移后演示行为必须与迁移前一致）", got)
	}
}

// TestExampleNoInternalImports 固化生态边界：示例仅可经 pkg 公共 API 交互。
func TestExampleNoInternalImports(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读取 main.go 失败: %v", err)
	}
	src := string(data)
	if strings.Contains(src, "AgentPrimordia/internal/") {
		t.Error("main.go 仍直依赖 agentprimordia/internal/*，违反 AGENTS.md §4.2（ecosystem 应仅经 pkg 公共 API 交互）")
	}
	if !strings.Contains(src, `ap "github.com/Gleamseekers/AgentPrimordia/pkg"`) {
		t.Error("main.go 未以 ap 别名导入 pkg 公共 API")
	}
}

// captureMain 重定向 stdout 运行 main()，返回其完整输出。
func captureMain(t *testing.T) string {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建 stdout 管道失败: %v", err)
	}
	os.Stdout = w

	// 读取 goroutine：并发排空管道，避免 main() 写满管道缓冲阻塞
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
		done <- sb.String()
	}()

	main() // 演示入口（无 os.Exit，可在测试内运行）

	_ = w.Close()
	os.Stdout = origStdout
	return <-done
}
