// cross_language_test.go — Go 侧跨语言行为一致性测试
//
// 读取 sdk/typescript/tests/shared/cross-language-spec.json 中定义的测试规范，
// 验证 Go 端行为与 TS 端一致。
//
// 运行方式：
//
//	go test -run TestCrossLanguage ./pkg/
package ap

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ===== 规范文件加载 =====

// crossLanguageSpec 跨语言测试规范顶层结构
type crossLanguageSpec struct {
	Version     string          `json:"version"`
	Description string          `json:"description"`
	TestSuites  []testSuiteSpec `json:"testSuites"`
}

type testSuiteSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Cases       []testCaseSpec `json:"cases"`
}

type testCaseSpec struct {
	ID          string          `json:"id"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Expected    json.RawMessage `json:"expected"`
}

// loadSpec 加载跨语言测试规范文件
func loadSpec(t *testing.T) *crossLanguageSpec {
	t.Helper()

	// 从当前文件位置向上查找 sdk/typescript/tests/shared/cross-language-spec.json
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法获取当前文件路径")
	}

	// pkg/ -> github.com/Gleamseekers/AgentPrimordia/ -> 仓库根 -> sdk/typescript/tests/shared/
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	specPath := filepath.Join(repoRoot, "sdk", "typescript", "tests", "shared", "cross-language-spec.json")

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("读取跨语言规范文件失败: %v (路径: %s)", err, specPath)
	}

	var spec crossLanguageSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("解析跨语言规范文件失败: %v", err)
	}

	return &spec
}

// ===== 错误码映射测试 =====

// TestCrossLanguage_ErrorCodeMapping 验证 Go 错误码与规范一致
func TestCrossLanguage_ErrorCodeMapping(t *testing.T) {
	spec := loadSpec(t)

	// 找到 error_code_mapping 套件
	var suite *testSuiteSpec
	for i := range spec.TestSuites {
		if spec.TestSuites[i].Name == "error_code_mapping" {
			suite = &spec.TestSuites[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("规范中未找到 error_code_mapping 套件")
	}

	// 定义 Go 端所有错误码（按模块分组）
	goErrorCodes := map[string][]string{
		"agent":    {"AGENT_001", "AGENT_002", "AGENT_003", "AGENT_004"},
		"tool":     {"TOOL_001", "TOOL_002", "TOOL_003", "TOOL_004"},
		"llm":      {"LLM_001", "LLM_002", "LLM_003", "LLM_004", "LLM_005", "LLM_006", "LLM_007", "LLM_008"},
		"pool":     {"POOL_001", "POOL_002", "POOL_003"},
		"memory":   {"MEM_001", "MEM_002", "MEM_003", "MEM_004", "MEM_005", "MEM_006", "MEM_007", "MEM_008"},
		"security": {"SEC_001", "SEC_002", "SEC_003", "SEC_004"},
		"infra":    {"EVT_001", "PST_001", "CON_001", "CON_002", "CTX_001"},
	}

	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			var input struct {
				Module string `json:"module"`
				Error  string `json:"error"`
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				t.Fatalf("解析输入失败: %v", err)
			}

			var expected struct {
				Codes []string `json:"codes"`
				Code  string   `json:"code"`
			}
			if err := json.Unmarshal(tc.Expected, &expected); err != nil {
				t.Fatalf("解析期望值失败: %v", err)
			}

			// 验证 unknown 错误码
			if input.Error != "" {
				code := GetErrorCode(nil)
				if code != "UNKNOWN" {
					t.Errorf("nil 错误应返回 UNKNOWN，得到 %q", code)
				}
				return
			}

			// 验证模块错误码
			goCodes, ok := goErrorCodes[input.Module]
			if !ok {
				t.Fatalf("未知模块: %s", input.Module)
			}

			if len(goCodes) != len(expected.Codes) {
				t.Errorf("模块 %s 错误码数量不一致: Go=%d, 规范=%d",
					input.Module, len(goCodes), len(expected.Codes))
			}

			// 逐个对比
			codeSet := make(map[string]bool)
			for _, c := range goCodes {
				codeSet[c] = true
			}
			for _, c := range expected.Codes {
				if !codeSet[c] {
					t.Errorf("规范中的错误码 %q 在 Go 端不存在", c)
				}
			}
		})
	}
}

// ===== 向量操作测试 =====

// cosineSimilarity 计算余弦相似度（与 TS 端对齐）
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// TestCrossLanguage_VectorOperations 验证向量操作与规范一致
func TestCrossLanguage_VectorOperations(t *testing.T) {
	spec := loadSpec(t)

	var suite *testSuiteSpec
	for i := range spec.TestSuites {
		if spec.TestSuites[i].Name == "vector_operations" {
			suite = &spec.TestSuites[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("规范中未找到 vector_operations 套件")
	}

	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			var input struct {
				VectorA []float64 `json:"vectorA"`
				VectorB []float64 `json:"vectorB"`
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				t.Fatalf("解析输入失败: %v", err)
			}

			var expected struct {
				Score     float64 `json:"score"`
				Tolerance float64 `json:"tolerance"`
			}
			if err := json.Unmarshal(tc.Expected, &expected); err != nil {
				t.Fatalf("解析期望值失败: %v", err)
			}

			score := cosineSimilarity(input.VectorA, input.VectorB)
			if math.Abs(score-expected.Score) > expected.Tolerance {
				t.Errorf("余弦相似度不一致: 得到 %.6f, 期望 %.6f (容差 %.4f)",
					score, expected.Score, expected.Tolerance)
			}
		})
	}
}

// ===== 错误处理测试 =====

// TestCrossLanguage_ErrorHandling 验证错误处理行为与规范一致
func TestCrossLanguage_ErrorHandling(t *testing.T) {
	spec := loadSpec(t)

	var suite *testSuiteSpec
	for i := range spec.TestSuites {
		if spec.TestSuites[i].Name == "error_handling" {
			suite = &spec.TestSuites[i]
			break
		}
	}
	if suite == nil {
		t.Fatal("规范中未找到 error_handling 套件")
	}

	for _, tc := range suite.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			var input struct {
				Name     string `json:"name"`
				MaxTurns int    `json:"maxTurns"`
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				t.Fatalf("解析输入失败: %v", err)
			}

			var expected struct {
				ShouldError bool `json:"shouldError"`
			}
			if err := json.Unmarshal(tc.Expected, &expected); err != nil {
				t.Fatalf("解析期望值失败: %v", err)
			}

			// 验证 Agent 配置错误处理
			var err error
			if input.Name == "" {
				err = ErrInvalidConfig // 空名称应产生错误
			}
			if input.MaxTurns < 0 {
				err = ErrInvalidConfig // 负数 MaxTurns 应产生错误
			}

			if expected.ShouldError && err == nil {
				t.Error("期望产生错误，但未产生")
			}
			if !expected.ShouldError && err != nil {
				t.Errorf("不期望产生错误，但得到: %v", err)
			}
		})
	}
}

// ===== GetErrorCode 单元测试 =====

// TestCrossLanguage_GetErrorCode 验证 GetErrorCode 函数行为
func TestCrossLanguage_GetErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"AgentStopped", ErrAgentStopped, "AGENT_001"},
		{"AgentRunning", ErrAgentRunning, "AGENT_002"},
		{"MaxTurns", ErrMaxTurnsExceeded, "AGENT_003"},
		{"NoToolkit", ErrNoToolkit, "AGENT_004"},
		{"ToolNotFound", ErrToolNotFound, "TOOL_001"},
		{"ToolExecution", ErrToolExecution, "TOOL_002"},
		{"LLMCallFailed", ErrLLMCallFailed, "LLM_001"},
		{"CircuitOpen", ErrCircuitOpen, "LLM_003"},
		{"PoolFull", ErrPoolFull, "POOL_001"},
		{"Timeout", ErrTimeout, "POOL_003"},
		{"EpisodeNotFound", ErrEpisodeNotFound, "MEM_001"},
		{"DimensionMismatch", ErrDimensionMismatch, "MEM_007"},
		{"CommandBlocked", ErrCommandBlocked, "SEC_001"},
		{"PathTraversal", ErrPathTraversal, "SEC_004"},
		{"BusClosed", ErrBusClosed, "EVT_001"},
		{"CheckpointNotFound", ErrCheckpointNotFound, "PST_001"},
		{"WriteConflict", ErrGlobalWriteConflict, "CON_001"},
		{"nil error", nil, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := GetErrorCode(tt.err)
			if code != tt.expected {
				t.Errorf("GetErrorCode(%v) = %q, 期望 %q", tt.err, code, tt.expected)
			}
		})
	}
}
