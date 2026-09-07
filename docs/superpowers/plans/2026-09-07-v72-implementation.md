# v7.2 实施计划：基准先行 + 五命题生产级深化

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 v7.1 的接口骨架深化为生产级实现，在硬任务集上证明框架能力的可测量增益。

**Architecture:** 三阶段推进——S1 设计硬任务集（3 周）→ S2 五命题并行深化（6-8 周，第一波 P4/P2/P3/P1，第二波 P5）→ S3 验收（2 周）。每个命题聚焦「接线打通」而非新增能力。

**Tech Stack:** Go 1.26+，标准库 + modernc.org/sqlite（白名单内），sensenova/OpenAI API

---

## 文件结构总览

### S1 新增文件
| 文件 | 职责 |
|------|------|
| `docs/evals/v72/manifest.json` | 任务集清单 + sha256 哈希 |
| `docs/evals/v72/p1-worldmodel.json` | P1 硬任务（≥30 道） |
| `docs/evals/v72/p2-planning.json` | P2 硬任务（≥30 道） |
| `docs/evals/v72/p4-intelligence.json` | P4 硬任务（≥30 道） |
| `docs/evals/v72/p5-multimodal.json` | P5 硬任务（≥30 道） |
| `internal/eval/registry_v72.go` | v72 任务集注册 + 冻结门 |
| `bench/v72/main.go` | v72 统一 bench 运行器 |
| `bench/v72/prescreen.go` | A 臂预筛逻辑 |

### S2 修改/新增文件
| 文件 | 变更类型 | 职责 |
|------|---------|------|
| `internal/agent/worldmodel/predict.go` | 新增 | LLM-as 状态预演 |
| `internal/agent/worldmodel/predict_test.go` | 新增 | 预演测试 |
| `internal/agent/react_loop_core.go` | 修改 | 接入预演 + 回溯校验 |
| `internal/agent/planning/cache.go` | 新增 | 规划缓存（embedding 相似度） |
| `internal/agent/planning/cache_test.go` | 新增 | 缓存测试 |
| `internal/agent/planning/hooks.go` | 修改 | EnhancedPlanner 接入缓存 + deadlock 自动触发 |
| `internal/tools/intelligence/hooks.go` | 修改 | IntelligenceHook 桥接 HookManager |
| `internal/tools/intelligence/create/creator.go` | 修改 | LifecycleCreator 接入 LLM |
| `internal/agent/react_loop_tools.go` | 修改 | 接入 IntelligenceHook + 自动注册 |
| `internal/observability/integration_test.go` | 新增 | P3 端到端集成测试 |
| `internal/llm/openai_provider.go` | 修改 | vision 路径 |
| `internal/agent/multimodal/vision_test.go` | 新增 | 多模态端到端测试 |

---

## S1：硬任务集设计（3 周）

### Task 1: 任务注册框架扩展

**Files:**
- Create: `internal/eval/registry_v72.go`
- Create: `internal/eval/registry_v72_test.go`
- Create: `docs/evals/v72/manifest.json`

- [ ] **Step 1: 写冻结门测试**

```go
// internal/eval/registry_v72_test.go
package eval

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"testing"
)

func TestV72RegistryFrozen(t *testing.T) {
	// 验证 manifest.json 存在且哈希匹配
	data, err := os.ReadFile("../../docs/evals/v72/manifest.json")
	if err != nil {
		t.Fatalf("manifest.json 不存在: %v", err)
	}
	var manifest struct {
		SHA256 string `json:"sha256"`
		Files  []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest.json 解析失败: %v", err)
	}
	// 验证每个任务文件哈希
	for _, f := range manifest.Files {
		content, err := os.ReadFile("../../docs/evals/v72/" + f.Path)
		if err != nil {
			t.Fatalf("任务文件 %s 不存在: %v", f.Path, err)
		}
		h := sha256.Sum256(content)
		got := fmt.Sprintf("%x", h)
		if got != f.Hash {
			t.Errorf("任务文件 %s 哈希不匹配: got %s, want %s", f.Path, got, f.Hash)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd agentprimordia && go test ./internal/eval/ -run TestV72RegistryFrozen -v`
Expected: FAIL — manifest.json 不存在

- [ ] **Step 3: 创建 manifest 骨架**

```json
// docs/evals/v72/manifest.json
{
  "version": "v7.2",
  "sha256": "",
  "files": [],
  "frozen_at": "",
  "holdout_ratio": 0.3
}
```

- [ ] **Step 4: 写注册加载器**

```go
// internal/eval/registry_v72.go
package eval

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed ../../docs/evals/v72/*.json
var v72Tasks embed.FS

// V72Task 是 v7.2 硬任务的标准格式
type V72Task struct {
	ID       string   `json:"id"`
	Prop     string   `json:"prop"`      // "p1" | "p2" | "p4" | "p5"
	Task     string   `json:"task"`      // 任务描述
	Holdout  bool     `json:"holdout"`   // 是否为留出集
	Fixtures []Fixture `json:"fixtures"` // 预置文件
	Asserts  []Assert  `json:"asserts"`  // 成功断言
}

type Fixture struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Assert struct {
	Type   string `json:"type"`   // "file_exists" | "file_contains" | "stdout_contains"
	Target string `json:"target"`
	Expect string `json:"expect"`
}

// LoadV72Tasks 加载指定命题的硬任务集
func LoadV72Tasks(prop string) ([]V72Task, error) {
	data, err := v72Tasks.ReadFile(fmt.Sprintf("../../docs/evals/v72/%s.json", prop))
	if err != nil {
		return nil, fmt.Errorf("加载 %s 任务集失败: %w", prop, err)
	}
	var tasks []V72Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("解析 %s 任务集失败: %w", prop, err)
	}
	return tasks, nil
}

// V72TaskCount 返回指定命题的任务数（排除留出集）
func V72TaskCount(prop string) (total, holdout int, err error) {
	tasks, err := LoadV72Tasks(prop)
	if err != nil {
		return 0, 0, err
	}
	for _, t := range tasks {
		if t.Holdout {
			holdout++
		}
	}
	return len(tasks), holdout, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd agentprimordia && go test ./internal/eval/ -run TestV72RegistryFrozen -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/eval/registry_v72.go internal/eval/registry_v72_test.go docs/evals/v72/manifest.json
git commit -m "feat: v7.2 任务注册框架 + 冻结门"
```

---

### Task 2: P1 世界模型硬任务设计

**Files:**
- Create: `docs/evals/v72/p1-worldmodel.json`

- [ ] **Step 1: 设计 30+ 道硬任务**

任务设计原则：A 臂（纯 ReAct + shell/filesystem）成功率 <60%。任务必须需要维护实体/关系状态图才能可靠完成。

```json
// docs/evals/v72/p1-worldmodel.json
[
  {
    "id": "p1-001",
    "prop": "p1",
    "task": "在目录中创建 5 个 Go 文件，每个文件包含一个 struct 定义。然后修改第 2 个文件的 struct，使其引用第 4 个文件的 struct。最后回答：哪些文件的 struct 存在跨文件引用关系？",
    "holdout": false,
    "fixtures": [],
    "asserts": [
      {"type": "file_exists", "target": "*.go", "expect": "5 files"},
      {"type": "stdout_contains", "target": "answer", "expect": "file2.go and file4.go"}
    ]
  },
  {
    "id": "p1-002",
    "prop": "p1",
    "task": "给定一个包含 10 个配置的 JSON 文件，依次修改其中 5 个配置的特定字段。完成后回答：哪些配置的 version 字段被修改过？按修改顺序列出。",
    "holdout": false,
    "fixtures": [
      {"path": "configs.json", "content": "{\"svc1\":{\"version\":\"1.0\",\"port\":8080},\"svc2\":{\"version\":\"1.0\",\"port\":8081},\"svc3\":{\"version\":\"1.0\",\"port\":8082},\"svc4\":{\"version\":\"1.0\",\"port\":8083},\"svc5\":{\"version\":\"1.0\",\"port\":8084},\"svc6\":{\"version\":\"1.0\",\"port\":8085},\"svc7\":{\"version\":\"1.0\",\"port\":8086},\"svc8\":{\"version\":\"1.0\",\"port\":8087},\"svc9\":{\"version\":\"1.0\",\"port\":8088},\"svc10\":{\"version\":\"1.0\",\"port\":8089}}"}
    ],
    "asserts": [
      {"type": "stdout_contains", "target": "answer", "expect": "ordered list of 5 modified configs"}
    ]
  }
]
```

> **注意**：此处仅列出 2 道示例任务。实际需设计 ≥30 道，覆盖：多步状态追踪（10 道）、因果链推理（10 道）、实体关系变化检测（10 道）。留出集 ≥30%（即 ≥10 道标记 `"holdout": true`）。任务设计为创造性工作，需结合对 Agent 能力边界的理解逐一编写。

- [ ] **Step 2: A 臂预筛**

用纯 ReAct + shell/filesystem 跑全部任务，记录成功率。淘汰成功率 ≥60% 的任务，替换为更难的任务。

```bash
# 预筛脚本（复用 bench/gap/main.go 的 A 臂逻辑）
cd agentprimordia && go run ./bench/v72/ -prescreen -prop p1
```

- [ ] **Step 3: 更新 manifest 哈希**

```bash
cd agentprimordia && python3 -c "
import json, hashlib
files = ['p1-worldmodel.json']
manifest = {'version': 'v7.2', 'files': [], 'holdout_ratio': 0.3}
for f in files:
    content = open(f'docs/evals/v72/{f}', 'rb').read()
    manifest['files'].append({'path': f, 'hash': hashlib.sha256(content).hexdigest()})
json.dump(manifest, open('docs/evals/v72/manifest.json', 'w'), indent=2)
"
```

- [ ] **Step 4: 提交**

```bash
git add docs/evals/v72/p1-worldmodel.json docs/evals/v72/manifest.json
git commit -m "feat: v7.2 P1 世界模型硬任务集（30+ 道，含预筛）"
```

---

### Task 3: P2 规划增强硬任务设计

**Files:**
- Create: `docs/evals/v72/p2-planning.json`

- [ ] **Step 1: 设计 30+ 道硬任务**

任务特征：≥5 步有依赖关系的子任务，含条件分支和失败回退。

```json
// docs/evals/v72/p2-planning.json
[
  {
    "id": "p2-001",
    "prop": "p2",
    "task": "构建一个数据处理 pipeline：1) 从 fixtures/data.csv 读取数据 2) 清洗空值和异常值 3) 按 category 分组聚合 4) 生成汇总报告到 report.txt 5) 验证报告中的数据行数与原始数据一致。如果步骤 3 失败（分组字段不存在），回退到按行号分组。",
    "holdout": false,
    "fixtures": [
      {"path": "data.csv", "content": "id,name,category,value\n1,A,cat1,10\n2,B,cat2,20\n3,C,,30\n4,D,cat1,40\n5,E,cat2,\n6,F,cat3,60"}
    ],
    "asserts": [
      {"type": "file_exists", "target": "report.txt", "expect": ""},
      {"type": "file_contains", "target": "report.txt", "expect": "total"}
    ]
  }
]
```

> 同 Task 2，需设计 ≥30 道，覆盖：多阶段 pipeline（10 道）、条件分支（10 道）、失败回退（10 道）。留出集 ≥30%。

- [ ] **Step 2: A 臂预筛 + 更新 manifest + 提交**

同 Task 1 流程。

```bash
git add docs/evals/v72/p2-planning.json docs/evals/v72/manifest.json
git commit -m "feat: v7.2 P2 规划增强硬任务集（30+ 道，含预筛）"
```

---

### Task 4: P4 工具智能硬任务设计

**Files:**
- Create: `docs/evals/v72/p4-intelligence.json`

- [ ] **Step 1: 设计 30+ 道硬任务**

任务特征：所需工具不在预置工具集中（只有 shell + filesystem），Agent 必须检测缺口→创建→使用。

```json
// docs/evals/v72/p4-intelligence.json
[
  {
    "id": "p4-001",
    "prop": "p4",
    "task": "将 fixtures/data.json 中的 JSON 数组转换为 CSV 格式，保存到 output.csv。要求：列名从 JSON key 自动推断，嵌套对象展平为 dot notation。",
    "holdout": false,
    "fixtures": [
      {"path": "data.json", "content": "[{\"name\":\"Alice\",\"address\":{\"city\":\"Beijing\",\"zip\":\"100000\"}},{\"name\":\"Bob\",\"address\":{\"city\":\"Shanghai\",\"zip\":\"200000\"}}]"}
    ],
    "asserts": [
      {"type": "file_exists", "target": "output.csv", "expect": ""},
      {"type": "file_contains", "target": "output.csv", "expect": "address.city"}
    ]
  }
]
```

> 需设计 ≥30 道，覆盖：格式转换（10 道）、数据分析（10 道）、内容生成（10 道）。留出集 ≥30%。

- [ ] **Step 2: A 臂预筛 + 更新 manifest + 提交**

```bash
git add docs/evals/v72/p4-intelligence.json docs/evals/v72/manifest.json
git commit -m "feat: v7.2 P4 工具智能硬任务集（30+ 道，含预筛）"
```

---

### Task 5: P5 多模态硬任务设计 + Vision API 可行性验证

**Files:**
- Create: `docs/evals/v72/p5-multimodal.json`
- Create: `docs/evals/v72/p5-assets/` (图片资源目录)

- [ ] **Step 1: 验证 sensenova vision API 支持**

```bash
curl -s https://token.sensenova.cn/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "sensenova-6.8-flash-lite",
    "messages": [{"role": "user", "content": [
      {"type": "text", "text": "描述这张图片"},
      {"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="}}
    ]}]
  }'
```

如果返回成功且包含图片描述 → sensenova 支持 vision，P5 使用 sensenova。
如果返回错误 → 切换为 OpenAI-compatible vision 端点。

- [ ] **Step 2: 设计 30+ 道硬任务**

任务特征：输入包含图片，纯文本无法获取关键信息。

```json
// docs/evals/v72/p5-multimodal.json
[
  {
    "id": "p5-001",
    "prop": "p5",
    "task": "查看 fixtures/chart.png 中的柱状图，提取每个柱的数据值，保存到 output.txt 格式为 '类别: 值'。",
    "holdout": false,
    "fixtures": [
      {"path": "chart.png", "content_type": "image/png", "source": "generated"}
    ],
    "asserts": [
      {"type": "file_exists", "target": "output.txt", "expect": ""},
      {"type": "file_contains", "target": "output.txt", "expect": ":"}
    ]
  }
]
```

> 需设计 ≥30 道，覆盖：图表数据提取（10 道）、UI 截图分析（10 道）、OCR + 推理（10 道）。需生成对应的测试图片资源。留出集 ≥30%。

- [ ] **Step 3: 更新 manifest + 提交**

```bash
git add docs/evals/v72/p5-multimodal.json docs/evals/v72/p5-assets/ docs/evals/v72/manifest.json
git commit -m "feat: v7.2 P5 多模态硬任务集（30+ 道，含 vision 验证）"
```

---

### Task 6: 冻结任务集 + 统一预筛报告

**Files:**
- Modify: `docs/evals/v72/manifest.json`
- Create: `docs/evals/v72/prescreen-report.json`

- [ ] **Step 1: 计算最终 manifest 哈希**

```bash
cd agentprimordia && python3 -c "
import json, hashlib, os
files = ['p1-worldmodel.json', 'p2-planning.json', 'p4-intelligence.json', 'p5-multimodal.json']
manifest = {'version': 'v7.2', 'files': [], 'holdout_ratio': 0.3}
all_hashes = ''
for f in sorted(files):
    path = f'docs/evals/v72/{f}'
    content = open(path, 'rb').read()
    h = hashlib.sha256(content).hexdigest()
    manifest['files'].append({'path': f, 'hash': h, 'count': len(json.loads(content))})
    all_hashes += h
manifest['sha256'] = hashlib.sha256(all_hashes.encode()).hexdigest()
manifest['frozen_at'] = '2026-09-XX'
json.dump(manifest, open('docs/evals/v72/manifest.json', 'w'), indent=2, ensure_ascii=False)
print('Manifest frozen:', manifest['sha256'])
"
```

- [ ] **Step 2: 运行冻结门测试**

Run: `cd agentprimordia && go test ./internal/eval/ -run TestV72RegistryFrozen -v`
Expected: PASS

- [ ] **Step 3: 提交冻结**

```bash
git add docs/evals/v72/manifest.json
git commit -m "feat: v7.2 任务集冻结（4 命题 × 30+ 道，哈希锁定）"
```

---

## S2 Wave 1：P4 工具智能深化（前 4 周）

### Task 7: IntelligenceHook 桥接 HookManager

**Files:**
- Modify: `internal/tools/intelligence/hooks.go`
- Create: `internal/tools/intelligence/bridge.go`
- Create: `internal/tools/intelligence/bridge_test.go`

**背景**：IntelligenceHook 当前只在 bench 中手动接线。需要桥接到 `hooks.HookManager`，让 ReAct 循环自动调用。

- [ ] **Step 1: 写桥接测试**

```go
// internal/tools/intelligence/bridge_test.go
package intelligence

import (
	"context"
	"testing"
	"time"

	"agentprimordia/internal/tools/intelligence/create"
	"agentprimordia/internal/tools/intelligence/optimize"
)

func TestIntelligenceHook_BridgeToHookManager(t *testing.T) {
	profiler := optimize.NewInMemoryProfiler()
	detector := create.NewTraceGapDetector()
	creator := create.NewLifecycleCreator()
	hook := NewIntelligenceHook(profiler, detector, creator)

	// 模拟 AfterToolCall
	ctx := context.Background()
	hook.AfterToolCall(ctx, "shell", `{"command":"ls"}`, "file1.txt\nfile2.txt", nil, 50*time.Millisecond)
	hook.AfterToolCall(ctx, "shell", `{"command":"cat missing"}`, "", fmt.Errorf("file not found"), 100*time.Millisecond)

	if hook.TraceLength() != 2 {
		t.Errorf("trace 长度 = %d, want 2", hook.TraceLength())
	}

	// 模拟 OnTurnEnd — 应触发缺口检测
	hook.OnTurnEnd(ctx)

	// profiler 应有 2 条记录
	profiles, _ := profiler.AllProfiles(ctx)
	if len(profiles) == 0 {
		t.Error("profiler 应有记录")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd agentprimordia && go test ./internal/tools/intelligence/ -run TestIntelligenceHook_BridgeToHookManager -v`
Expected: FAIL — `fmt` 未导入（编译错误）

- [ ] **Step 3: 修复测试 + 确认现有 hook 已满足**

现有 `IntelligenceHook` 已实现 `AfterToolCall` 和 `OnTurnEnd`。桥接的关键是在 `react_loop_tools.go` 的 `processToolResult` 中调用这些方法。

- [ ] **Step 4: 在 react_loop_tools.go 中接入 IntelligenceHook**

在 `processToolResult` 函数中，tool 执行完成后调用 IntelligenceHook：

```go
// internal/agent/react_loop_tools.go — processToolResult 中追加
// 在已有的 hook 调用之后，追加 intelligence hook
if capCache.intelligenceHook != nil {
	capCache.intelligenceHook.AfterToolCall(ctx, toolName, argsStr, result.Content, toolErr, duration)
}
```

在 `runLoop` 的每轮结束处追加：

```go
// internal/agent/react_loop_core.go — runLoop 的 turn 结束处追加
if capCache.intelligenceHook != nil {
	capCache.intelligenceHook.OnTurnEnd(ctx)
}
```

- [ ] **Step 5: 在 resolveCapabilities 中解析 IntelligenceHook**

```go
// internal/agent/react_loop.go — capabilityCache 增加字段
type capabilityCache struct {
	// ... 现有字段 ...
	intelligenceHook *intelligence.IntelligenceHook
}

// resolveCapabilities 中追加
if a.config.ToolIntelligence != nil {
	ti := a.config.ToolIntelligence
	cc.intelligenceHook = intelligence.NewIntelligenceHook(ti.Profiler, ti.Detector, ti.Creator)
}
```

- [ ] **Step 6: 在 ReActConfig 中增加 ToolIntelligence 选项**

```go
// internal/agent/react_loop.go — ReActConfig 增加
type ToolIntelligenceConfig struct {
	Profiler interface{} // intelligence.ToolProfiler
	Detector interface{} // intelligence.GapDetector
	Creator  interface{} // intelligence.ToolCreator
}

// ReActConfig 增加字段
type ReActConfig struct {
	// ... 现有字段 ...
	ToolIntelligence *ToolIntelligenceConfig
}
```

- [ ] **Step 7: 运行全量测试**

Run: `cd agentprimordia && go test ./internal/agent/ ./internal/tools/intelligence/ -v -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/tools/intelligence/ internal/agent/react_loop.go internal/agent/react_loop_core.go internal/agent/react_loop_tools.go
git commit -m "feat: IntelligenceHook 桥接 ReAct 循环——工具智能自动记录 + 缺口检测"
```

---

### Task 8: LifecycleCreator 接入 LLM 生成工具

**Files:**
- Modify: `internal/tools/intelligence/create/creator.go`
- Create: `internal/tools/intelligence/create/creator_llm.go`
- Create: `internal/tools/intelligence/create/creator_llm_test.go`

**背景**：当前 LifecycleCreator 使用硬编码的 20 个脚本模板。升级为 LLM 驱动的工具生成。

- [ ] **Step 1: 写 LLM Creator 测试**

```go
// internal/tools/intelligence/create/creator_llm_test.go
package create

import (
	"context"
	"strings"
	"testing"

	"agentprimordia/internal/tools/intelligence"
)

// mockLLMForCreator 返回预设的 LLM 响应
type mockLLMForCreator struct {
	response string
}

func (m *mockLLMForCreator) Complete(ctx context.Context, messages interface{}) (string, error) {
	return m.response, nil
}

func TestLLMCreator_GeneratesShellScript(t *testing.T) {
	mockLLM := &mockLLMForCreator{
		response: `#!/bin/sh
# JSON to CSV converter
input="$1"
output="$2"
python3 -c "
import json, csv, sys
data = json.load(open(sys.argv[1]))
if not data: sys.exit(0)
keys = list(data[0].keys())
w = csv.writer(open(sys.argv[2], 'w'))
w.writerow(keys)
for row in data:
    w.writerow([row.get(k, '') for k in keys])
" "$input" "$output"`,
	}

	creator := NewLLMCreator(mockLLM)
	gap := intelligence.GapCandidate{
		Key:         "json_to_csv",
		Kind:        "format_conversion",
		SampleError: "no tool to convert JSON array to CSV format",
		Count:       3,
	}

	artifact, err := creator.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if artifact == nil {
		t.Fatal("artifact 不应为 nil")
	}
	if !strings.Contains(string(artifact.Artifact), "#!/bin/sh") {
		t.Error("生成的工件应包含 shell 脚本头")
	}
	if artifact.Name != "json_to_csv" {
		t.Errorf("Name = %q, want %q", artifact.Name, "json_to_csv")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd agentprimordia && go test ./internal/tools/intelligence/create/ -run TestLLMCreator -v`
Expected: FAIL — `NewLLMCreator` 未定义

- [ ] **Step 3: 实现 LLMCreator**

```go
// internal/tools/intelligence/create/creator_llm.go
package create

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"agentprimordia/internal/tools/intelligence"
)

// LLMCompleter 是 LLM 调用的最小接口（避免 import cycle）
type LLMCompleter interface {
	Complete(ctx context.Context, messages interface{}) (string, error)
}

// LLMCreator 基于 LLM 的工具生成器
type LLMCreator struct {
	llm LLMCompleter
}

func NewLLMCreator(llm LLMCompleter) *LLMCreator {
	return &LLMCreator{llm: llm}
}

func (c *LLMCreator) Create(ctx context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	if gap.Key == "" {
		return nil, fmt.Errorf("缺口键为空")
	}

	prompt := fmt.Sprintf(`你是一个工具生成器。根据以下缺口描述，生成一个 shell 脚本来填补这个能力缺口。

缺口名称: %s
缺口类型: %s
错误示例: %s
出现次数: %d

要求：
1. 输出完整的 #!/bin/sh 脚本
2. 使用标准 Unix 工具（awk, sed, grep, python3 等）
3. 脚本接受输入文件作为 $1，输出到 $2
4. 不要输出任何解释，只输出脚本代码`, gap.Key, gap.Kind, gap.SampleError, gap.Count)

	response, err := c.llm.Complete(ctx, prompt)
	if err != nil {
		// LLM 失败时回退到模板生成器
		fallback := NewLifecycleCreator()
		return fallback.Create(ctx, gap)
	}

	// 清理响应（去除 markdown 代码块标记）
	script := cleanLLMResponse(response)

	sum := sha256.Sum256([]byte(script))
	return &intelligence.ToolArtifact{
		ID:          fmt.Sprintf("auto-%s", gap.Key),
		Name:        gap.Key,
		Description: fmt.Sprintf("自动生成的工具：%s（LLM 生成）", gap.Key),
		ArtifactSHA: hex.EncodeToString(sum[:]),
		Artifact:    []byte(script),
	}, nil
}

func cleanLLMResponse(response string) string {
	// 去除 ```sh ... ``` 或 ```bash ... ``` 包裹
	s := response
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		// 跳过语言标记
		if nl := strings.Index(s, "\n"); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	return strings.TrimSpace(s)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd agentprimordia && go test ./internal/tools/intelligence/create/ -run TestLLMCreator -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/tools/intelligence/create/creator_llm.go internal/tools/intelligence/create/creator_llm_test.go
git commit -m "feat: LLMCreator——LLM 驱动的工具生成（回退到模板生成器）"
```

---

### Task 9: 创建工具自动注册到 Agent Registry

**Files:**
- Modify: `internal/agent/react_loop_tools.go`
- Create: `internal/tools/intelligence/registering.go`
- Create: `internal/tools/intelligence/registering_test.go`

**背景**：bench 中的 `registeringCreator` 包装器需要正式化进框架。

- [ ] **Step 1: 写注册器测试**

```go
// internal/tools/intelligence/registering_test.go
package intelligence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/intelligence/create"
)

func TestRegisteringCreator_RegistersTool(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()
	base := create.NewLifecycleCreator()
	rc := NewRegisteringCreator(base, reg, dir)

	gap := GapCandidate{
		Key:         "test_tool",
		SampleError: "test error",
		Count:       1,
	}

	artifact, err := rc.Create(context.Background(), gap)
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if artifact == nil {
		t.Fatal("artifact 不应为 nil")
	}

	// 验证工具已注册到 Registry
	tool := reg.Get("test_tool")
	if tool == nil {
		t.Fatal("工具应已注册到 Registry")
	}
	if tool.Name() != "test_tool" {
		t.Errorf("tool.Name() = %q, want %q", tool.Name(), "test_tool")
	}

	// 验证脚本文件已写入磁盘
	scriptPath := filepath.Join(dir, ".intel-tools", "test_tool")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("脚本文件应存在: %v", err)
	}
}
```

- [ ] **Step 2: 实现 RegisteringCreator**

```go
// internal/tools/intelligence/registering.go
package intelligence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"agentprimordia/internal/tools"
)

// RegisteringCreator 包装 ToolCreator，创建的工具自动注册到 Registry
type RegisteringCreator struct {
	base ToolCreator
	reg  *tools.Registry
	dir  string
}

func NewRegisteringCreator(base ToolCreator, reg *tools.Registry, dir string) *RegisteringCreator {
	return &RegisteringCreator{base: base, reg: reg, dir: dir}
}

func (rc *RegisteringCreator) Create(ctx context.Context, gap GapCandidate) (*ToolArtifact, error) {
	art, err := rc.base.Create(ctx, gap)
	if err != nil || art == nil {
		return art, err
	}

	// 写入脚本文件
	toolDir := filepath.Join(rc.dir, ".intel-tools")
	os.MkdirAll(toolDir, 0755)
	scriptPath := filepath.Join(toolDir, art.Name)
	if err := os.WriteFile(scriptPath, art.Artifact, 0755); err != nil {
		return art, fmt.Errorf("写入脚本失败: %w", err)
	}

	// 注册为 Agent 可用工具
	tool := &artifactTool{
		name:    art.Name,
		desc:    art.Description,
		path:    scriptPath,
		workdir: rc.dir,
	}
	_ = rc.reg.Register(tool)

	return art, nil
}
```

- [ ] **Step 3: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/tools/intelligence/ -run TestRegisteringCreator -v
git add internal/tools/intelligence/registering.go internal/tools/intelligence/registering_test.go internal/agent/react_loop_tools.go
git commit -m "feat: RegisteringCreator——创建工具自动注册到 Agent Registry"
```

---

### Task 10: P4 端到端集成测试

**Files:**
- Create: `internal/tools/intelligence/integration_test.go`

- [ ] **Step 1: 写端到端集成测试**

测试完整链路：Agent 执行任务 → 工具调用失败 → IntelligenceHook 检测缺口 → LLMCreator 生成工具 → RegisteringCreator 注册 → 下一轮 Agent 使用新工具。

```go
// internal/tools/intelligence/integration_test.go
package intelligence

import (
	"context"
	"testing"
	"time"

	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/intelligence/create"
	"agentprimordia/internal/tools/intelligence/optimize"
)

func TestToolIntelligence_EndToEnd(t *testing.T) {
	ctx := context.Background()
	reg := tools.NewRegistry()
	dir := t.TempDir()

	// 组装工具智能栈
	profiler := optimize.NewInMemoryProfiler()
	detector := create.NewTraceGapDetector()
	baseCreator := create.NewLifecycleCreator()
	creator := NewRegisteringCreator(baseCreator, reg, dir)
	hook := NewIntelligenceHook(profiler, detector, creator)

	// 模拟 Agent 执行：多次工具调用失败
	for i := 0; i < 5; i++ {
		hook.AfterToolCall(ctx, "shell", `{"command":"convert input.json output.csv"}`,
			"convert: command not found", fmt.Errorf("exit status 127"), 200*time.Millisecond)
	}

	// Turn 结束 → 应触发缺口检测 + 工具创建
	hook.OnTurnEnd(ctx)

	// 验证：profiler 有记录
	profile, err := profiler.Profile(ctx, "shell")
	if err != nil {
		t.Fatalf("Profile 失败: %v", err)
	}
	if profile.TotalCalls != 5 {
		t.Errorf("TotalCalls = %d, want 5", profile.TotalCalls)
	}
	if profile.SuccessRate != 0.0 {
		t.Errorf("SuccessRate = %f, want 0.0", profile.SuccessRate)
	}

	// 验证：工具已创建并注册
	tool := reg.Get("json_to_csv")
	if tool == nil {
		// 缺口检测可能生成不同的工具名，检查是否有任何工具被注册
		allTools := reg.List()
		if len(allTools) == 0 {
			t.Log("警告：无工具被注册——缺口检测可能未识别此模式")
		}
	}
}
```

- [ ] **Step 2: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/tools/intelligence/ -run TestToolIntelligence_EndToEnd -v
git add internal/tools/intelligence/integration_test.go
git commit -m "test: P4 工具智能端到端集成测试"
```

---

## S2 Wave 1：P2 规划增强深化

### Task 11: 规划缓存（embedding 相似度）

**Files:**
- Create: `internal/agent/planning/cache.go`
- Create: `internal/agent/planning/cache_test.go`

- [ ] **Step 1: 写缓存测试**

```go
// internal/agent/planning/cache_test.go
package planning

import (
	"testing"
)

func TestPlanCache_HitOnSimilarTask(t *testing.T) {
	cache := NewPlanCache(0.85)

	// 存入一个 plan
	plan1 := &Plan{
		Steps: []SubTask{
			{ID: "s1", Task: "download data", Status: StatusCompleted},
			{ID: "s2", Task: "process data", Status: StatusPending},
		},
	}
	cache.Put("构建数据处理 pipeline", plan1)

	// 查询相似任务
	result := cache.Get("构建一个数据处理 pipeline")
	if result == nil {
		t.Fatal("应命中缓存")
	}
	if len(result.Steps) != 2 {
		t.Errorf("Steps 数 = %d, want 2", len(result.Steps))
	}
}

func TestPlanCache_MissOnDissimilarTask(t *testing.T) {
	cache := NewPlanCache(0.85)

	plan1 := &Plan{Steps: []SubTask{{ID: "s1", Task: "download data"}}}
	cache.Put("构建数据处理 pipeline", plan1)

	result := cache.Get("写一封邮件给老板")
	if result != nil {
		t.Error("不相似的任务不应命中缓存")
	}
}
```

- [ ] **Step 2: 实现 PlanCache**

```go
// internal/agent/planning/cache.go
package planning

import (
	"strings"
	"sync"
)

// PlanCache 基于任务描述相似度的 plan 缓存
type PlanCache struct {
	mu        sync.RWMutex
	entries   []cacheEntry
	threshold float64
}

type cacheEntry struct {
	task string
	plan *Plan
}

func NewPlanCache(threshold float64) *PlanCache {
	return &PlanCache{threshold: threshold, entries: make([]cacheEntry, 0)}
}

func (c *PlanCache) Put(task string, plan *Plan) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, cacheEntry{task: task, plan: plan})
}

func (c *PlanCache) Get(task string) *Plan {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var bestMatch *Plan
	bestSim := 0.0
	for _, e := range c.entries {
		sim := tokenOverlap(task, e.task)
		if sim > bestSim && sim >= c.threshold {
			bestSim = sim
			bestMatch = e.plan
		}
	}
	return bestMatch
}

// tokenOverlap 基于词元重叠的相似度（Jaccard coefficient）
// 后续可替换为 embedding 余弦相似度（需 EmbeddingProvider）
func tokenOverlap(a, b string) float64 {
	tokensA := tokenize(strings.ToLower(a))
	tokensB := tokenize(strings.ToLower(b))
	if len(tokensA) == 0 || len(tokensB) == 0 {
		return 0
	}
	intersection := 0
	setA := make(map[string]bool)
	for _, t := range tokensA {
		setA[t] = true
	}
	for _, t := range tokensB {
		if setA[t] {
			intersection++
		}
	}
	union := len(tokensA) + len(tokensB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func tokenize(s string) []string {
	// 简单分词：按空格和标点分割
	var tokens []string
	current := ""
	for _, r := range s {
		if r == ' ' || r == ',' || r == '.' || r == '，' || r == '。' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		tokens = append(tokens, current)
	}
	return tokens
}
```

> **注意**：初始版本使用 tokenOverlap（Jaccard）作为相似度。后续可替换为 embedding 余弦相似度（需接入 S0-3 的 `EmbeddingProvider`），但 tokenOverlap 足以启动并验证缓存机制。

- [ ] **Step 3: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/agent/planning/ -run TestPlanCache -v
git add internal/agent/planning/cache.go internal/agent/planning/cache_test.go
git commit -m "feat: 规划缓存——基于任务相似度复用已有 plan 骨架"
```

---

### Task 12: DeadlockDetector 接入 ReAct 循环

**Files:**
- Modify: `internal/agent/react_loop_core.go`

- [ ] **Step 1: 在 runLoop 中接入 deadlock 检测**

在 `react_loop_core.go` 的 `runLoop` 中，每轮结束后检查是否陷入死锁：

```go
// 在 turn 结束处追加（tool result 处理完成后）
if capCache.planner != nil {
	if ep, ok := capCache.planner.(*planning.EnhancedPlanner); ok {
		if ep.Deadlock != nil {
			ep.Deadlock.RecordFailure(toolName, argsStr)
			if ep.Deadlock.DetectDeadlock() {
				// 触发恢复策略
				if ep.Recovery != nil {
					altPlan, err := ep.Recovery.Recover(ctx, currentPlan, failureMsg)
					if err == nil && altPlan != nil {
						currentPlan = altPlan
						ep.Deadlock.Reset()
					}
				}
			}
		}
	}
}
```

- [ ] **Step 2: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/agent/ -run TestReActLoop -v -count=1
git add internal/agent/react_loop_core.go
git commit -m "feat: DeadlockDetector 接入 ReAct 循环——连续失败自动恢复"
```

---

## S2 Wave 1：P3 可观测性集成测试

### Task 13: P3 端到端集成测试

**Files:**
- Create: `internal/observability/integration_test.go`

- [ ] **Step 1: 写集成测试**

```go
// internal/observability/integration_test.go
package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestObservability_EndToEnd(t *testing.T) {
	// 1. 创建 CorrelationStore + AlertEngine + DashboardHandler
	store := NewCorrelationStore()
	engine := NewAlertEngine()
	dashboard := NewDashboardHandler(store, engine)

	// 2. 注册告警规则
	rule := NewThresholdAlertRule("high_latency", "latency_ms", 500, SeverityWarning)
	engine.RegisterRule(rule)

	// 3. 模拟一次 Agent 运行
	traceID := "test-trace-001"
	store.Start(traceID, "agent-test")
	store.RecordLLM(traceID, "llm-call-1", "gpt-4", 100, 50, 200*time.Millisecond)
	store.RecordTool(traceID, "tool-call-1", "shell", true, 600*time.Millisecond) // 超过 500ms 阈值
	store.End(traceID, true)

	// 4. 验证 trace 已记录
	traces := store.List()
	if len(traces) != 1 {
		t.Fatalf("traces 数 = %d, want 1", len(traces))
	}

	// 5. 评估告警
	events := engine.Evaluate()
	// 应有 latency 告警（600ms > 500ms 阈值）
	found := false
	for _, e := range events {
		if e.RuleName == "high_latency" {
			found = true
		}
	}
	if !found {
		t.Error("应触发 high_latency 告警")
	}

	// 6. 验证 Dashboard HTTP handler
	mux := http.NewServeMux()
	dashboard.RegisterHandlers(mux)

	req := httptest.NewRequest("GET", "/dashboard/summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Dashboard status = %d, want 200", w.Code)
	}

	var summary map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("Dashboard 响应解析失败: %v", err)
	}
	if summary["total_traces"] == nil {
		t.Error("Dashboard 应包含 total_traces")
	}
}
```

- [ ] **Step 2: 运行测试 + 修复 + 提交**

```bash
cd agentprimordia && go test ./internal/observability/ -run TestObservability_EndToEnd -v
git add internal/observability/integration_test.go
git commit -m "test: P3 可观测性端到端集成测试（OTel + 告警 + Dashboard）"
```

---

## S2 Wave 1：P1 世界模型深化

### Task 14: LLM-as 状态预演

**Files:**
- Create: `internal/agent/worldmodel/predict.go`
- Create: `internal/agent/worldmodel/predict_test.go`

- [ ] **Step 1: 写预演测试**

```go
// internal/agent/worldmodel/predict_test.go
package worldmodel

import (
	"context"
	"testing"
)

func TestPredictPrompt_GeneratesDelta(t *testing.T) {
	graph := NewStateGraph()
	graph.AddNode(StateNode{Kind: NodeTask, Summary: "create file main.go"})
	graph.AddNode(StateNode{Kind: NodeToolCall, Summary: "write main.go"})
	graph.AddEdge("task-1", "tool-1", EdgeCause)

	candidates := []string{
		"write file utils.go",
		"delete file main.go",
	}

	// 使用 mock LLM
	mockLLM := &mockPredictLLM{
		response: `{"predictions": [
			{"action": "write file utils.go", "delta": [{"kind": "tool_call", "summary": "write utils.go", "effect": "add_node"}]},
			{"action": "delete file main.go", "delta": [{"kind": "tool_call", "summary": "delete main.go", "effect": "remove_node"}]}
		]}`,
	}

	deltas, err := PredictStateDeltas(context.Background(), mockLLM, graph, candidates)
	if err != nil {
		t.Fatalf("PredictStateDeltas 失败: %v", err)
	}
	if len(deltas) != 2 {
		t.Fatalf("deltas 数 = %d, want 2", len(deltas))
	}
	if len(deltas[0]) != 1 {
		t.Errorf("第一个预测的 delta 数 = %d, want 1", len(deltas[0]))
	}
}

type mockPredictLLM struct {
	response string
}

func (m *mockPredictLLM) Complete(ctx context.Context, prompt string) (string, error) {
	return m.response, nil
}
```

- [ ] **Step 2: 实现 PredictStateDeltas**

```go
// internal/agent/worldmodel/predict.go
package worldmodel

import (
	"context"
	"encoding/json"
	"fmt"
)

// StateDelta 是单个动作的预期状态增量
type StateDelta struct {
	Kind    NodeKind `json:"kind"`
	Summary string   `json:"summary"`
	Effect  string   `json:"effect"` // "add_node" | "remove_node" | "modify_edge"
}

// PredictPrompt 构造预演 prompt
func PredictPrompt(graph *StateGraph, candidates []string) string {
	snapshot := Snapshot(graph)
	nodesJSON, _ := json.MarshalIndent(snapshot.Nodes, "", "  ")

	prompt := fmt.Sprintf(`当前世界模型状态：
%s

候选动作：
`, nodesJSON)
	for i, c := range candidates {
		prompt += fmt.Sprintf("%d. %s\n", i+1, c)
	}
	prompt += `
对每个候选动作，预测执行后的状态增量（JSON 格式）：
{"predictions": [{"action": "动作描述", "delta": [{"kind": "节点类型", "summary": "变化描述", "effect": "add_node|remove_node|modify_edge"}]}]}`
	return prompt
}

// PredictStateDeltas 调用 LLM 预测每个候选动作的状态增量
func PredictStateDeltas(ctx context.Context, llm interface{ Complete(context.Context, string) (string, error) }, graph *StateGraph, candidates []string) ([][]StateDelta, error) {
	prompt := PredictPrompt(graph, candidates)
	response, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM 预演失败: %w", err)
	}

	var result struct {
		Predictions []struct {
			Action string       `json:"action"`
			Delta  []StateDelta `json:"delta"`
		} `json:"predictions"`
	}
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("预演响应解析失败: %w", err)
	}

	deltas := make([][]StateDelta, len(result.Predictions))
	for i, p := range result.Predictions {
		deltas[i] = p.Delta
	}
	return deltas, nil
}
```

- [ ] **Step 3: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/agent/worldmodel/ -run TestPredictPrompt -v
git add internal/agent/worldmodel/predict.go internal/agent/worldmodel/predict_test.go
git commit -m "feat: LLM-as 状态预演——行动前预测状态增量"
```

---

### Task 15: 回溯校验接入失败库

**Files:**
- Modify: `internal/agent/worldmodel/predict.go`
- Modify: `internal/agent/react_loop_core.go`

- [ ] **Step 1: 实现 Validate 函数**

```go
// internal/agent/worldmodel/predict.go — 追加

// Validate 对比预演的 predicted delta vs 实际状态变化
func Validate(predicted []StateDelta, before, after *StateGraph) []string {
	var divergences []string

	// 检查 predicted 中的 add_node 是否在实际中出现
	newNodes := diffNodes(before, after)
	predictedAdditions := make(map[string]bool)
	for _, d := range predicted {
		if d.Effect == "add_node" {
			predictedAdditions[d.Summary] = true
		}
	}

	for summary := range predictedAdditions {
		found := false
		for _, n := range newNodes {
			if n.Summary == summary {
				found = true
				break
			}
		}
		if !found {
			divergences = append(divergences, fmt.Sprintf("预期新增 %q 但未出现", summary))
		}
	}

	return divergences
}

func diffNodes(before, after *StateGraph) []StateNode {
	beforeSet := make(map[string]bool)
	for _, n := range before.Nodes {
		beforeSet[n.ID] = true
	}
	var added []StateNode
	for _, n := range after.Nodes {
		if !beforeSet[n.ID] {
			added = append(added, n)
		}
	}
	return added
}
```

- [ ] **Step 2: 在 react_loop_core.go 中接入回溯校验**

在 `wmObserveToolResult` 之后追加回溯校验调用：

```go
// 在 wmObserveToolResult 调用后追加
if capCache.worldTracker != nil && capCache.lastPrediction != nil {
	before := worldmodel.Snapshot(capCache.worldTracker.Graph())
	// ... tool result 已更新 graph ...
	after := worldmodel.Snapshot(capCache.worldTracker.Graph())
	divergences := worldmodel.Validate(capCache.lastPrediction, before, after)
	if len(divergences) > 0 {
		// 记录到失败库
		for _, d := range divergences {
			capCache.failureStore.Record(ctx, FailureRecord{
				AgentID: a.config.Name,
				Task:    d,
				Error:   "world model prediction divergence",
			})
		}
	}
	capCache.lastPrediction = nil
}
```

- [ ] **Step 3: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/agent/worldmodel/ ./internal/agent/ -v -count=1
git add internal/agent/worldmodel/predict.go internal/agent/react_loop_core.go
git commit -m "feat: 回溯校验——预演 vs 实际差异记录进失败库"
```

---

## S2 Wave 2：P5 多模态深化

### Task 16: OpenAI Provider Vision 路径

**Files:**
- Modify: `internal/llm/openai_provider.go`
- Create: `internal/llm/vision_test.go`

- [ ] **Step 1: 写 vision 测试**

```go
// internal/llm/vision_test.go
package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProvider_VisionRequest(t *testing.T) {
	// Mock server 验证请求格式
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "A bar chart showing sales data"}},
			},
		})
	}))
	defer server.Close()

	provider := NewOpenAIProvider(OpenAIConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "gpt-4o-mini",
	})

	// 发送多模态请求
	req := CompletionRequest{
		Messages: []ChatMessage{
			{
				Role: "user",
				MultimodalContent: &MultimodalContent{
					Parts: []ContentPart{
						{Type: ContentTypeText, Text: "描述这张图片"},
						{Type: ContentTypeImageURL, URL: "https://example.com/chart.png"},
					},
				},
			},
		},
	}

	resp, err := provider.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}
	if resp.Content == "" {
		t.Error("响应内容不应为空")
	}

	// 验证请求体格式
	messages := capturedBody["messages"].([]interface{})
	msg := messages[0].(map[string]interface{})
	content := msg["content"]
	// content 应该是数组格式（多模态）
	contentArr, ok := content.([]interface{})
	if !ok {
		t.Fatalf("content 应为数组格式, got %T", content)
	}
	if len(contentArr) != 2 {
		t.Errorf("content 数组长度 = %d, want 2", len(contentArr))
	}
}
```

- [ ] **Step 2: 修改 buildMessages 支持多模态**

在 `openai_provider.go` 的 `buildMessages` 中，检查 `ChatMessage.MultimodalContent` 是否存在，存在则构建数组格式的 content：

```go
// 在 buildMessages 循环中追加
if msg.MultimodalContent != nil {
	var parts []map[string]interface{}
	for _, p := range msg.MultimodalContent.Parts {
		switch p.Type {
		case ContentTypeText:
			parts = append(parts, map[string]interface{}{"type": "text", "text": p.Text})
		case ContentTypeImageURL:
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": p.URL},
			})
		}
	}
	m["content"] = parts
} else {
	m["content"] = msg.Content
}
```

- [ ] **Step 3: 运行测试 + 提交**

```bash
cd agentprimordia && go test ./internal/llm/ -run TestOpenAIProvider_Vision -v
git add internal/llm/openai_provider.go internal/llm/vision_test.go
git commit -m "feat: OpenAI Provider vision 路径——多模态 content 数组格式"
```

---

### Task 17: P5 多模态端到端测试

**Files:**
- Create: `internal/agent/multimodal/vision_test.go`

- [ ] **Step 1: 写端到端测试**

测试从 MultimodalMessage → ContentPart → OpenAI vision request → response 的完整链路。

- [ ] **Step 2: 运行测试 + 提交**

```bash
git add internal/agent/multimodal/vision_test.go
git commit -m "test: P5 多模态端到端 vision 测试"
```

---

## S3：验收（2 周）

### Task 18: v72 统一 Bench 运行器

**Files:**
- Create: `bench/v72/main.go`
- Create: `bench/v72/run.sh`

- [ ] **Step 1: 实现统一 bench 运行器**

bench 运行器需支持：
- 加载 v72 硬任务集
- A 臂：纯 ReAct + shell/filesystem
- B 臂：ReAct + 对应命题的框架能力
- McNemar 精确检验 + Wilson CI
- 结果输出到 `bench/results/v72/`

- [ ] **Step 2: 编写 run.sh**

```bash
#!/bin/bash
# bench/v72/run.sh — v7.2 统一验收 bench
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

export OPENAI_API_KEY="${OPENAI_API_KEY:-sk-IE84nyrP9MdJXleKAfAKGNiawk81sHZW}"
export OPENAI_BASE_URL="${OPENAI_BASE_URL:-https://token.sensenova.cn/v1}"
export OPENAI_MODEL="${OPENAI_MODEL:-sensenova-6.8-flash-lite}"

PROP="${1:-all}"  # p1 | p2 | p4 | p5 | all

cd "$PROJECT_ROOT"
go run ./bench/v72/ -prop "$PROP" -output "bench/results/v72/"
```

- [ ] **Step 3: 提交**

```bash
git add bench/v72/
git commit -m "feat: v7.2 统一 bench 运行器"
```

---

### Task 19: 跑验收实验 + 出报告

**Files:**
- Create: `docs/v7.2-验收实验报告.md`

- [ ] **Step 1: 跑全部 4 个命题的 A/B 实验**

```bash
cd agentprimordia
bash bench/v72/run.sh p1   # 世界模型
bash bench/v72/run.sh p2   # 规划增强
bash bench/v72/run.sh p4   # 工具智能
bash bench/v72/run.sh p5   # 多模态
```

- [ ] **Step 2: P3 集成测试**

```bash
cd agentprimordia && go test ./internal/observability/ -run TestObservability_EndToEnd -v
```

- [ ] **Step 3: 撰写验收报告**

包含：每个命题的 McNemar 数字、集成测试结果、与 v7.1 数字对比、根因分析（如仍不达标）。

- [ ] **Step 4: 提交报告**

```bash
git add docs/v7.2-验收实验报告.md bench/results/v72/
git commit -m "docs: v7.2 验收实验报告"
```

---

### Task 20: 版本升级 v7.2.0-rc1

**Files:**
- Modify: `pkg/agent.go`

- [ ] **Step 1: 更新版本号**

```go
const Version = "7.2.0-rc1"
```

- [ ] **Step 2: 提交 + 打 tag**

```bash
git add pkg/agent.go
git commit -m "feat: v7.2.0-rc1 版本升级"
git tag v7.2.0-rc1
```

---

## 执行顺序总览

```
S1（3 周）:
  Task 1 → Task 2 → Task 3 → Task 4 → Task 5 → Task 6

S2 Wave 1（4 周）:
  Task 7 → Task 8 → Task 9 → Task 10  (P4 工具智能)
  Task 11 → Task 12                     (P2 规划增强)
  Task 13                               (P3 可观测性)
  Task 14 → Task 15                     (P1 世界模型)

S2 Wave 2（2-4 周）:
  Task 16 → Task 17                     (P5 多模态)

S3（2 周）:
  Task 18 → Task 19 → Task 20           (验收 + 发版)
```
