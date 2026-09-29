package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runTest(args []string) error {
	var verbose, e2e bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verbose", "-v":
			verbose = true
		case "--e2e":
			e2e = true
		case "--help", "-h":
			fmt.Print(`ap test — run eval/e2e test suite

Usage:
  ap test [--verbose] [--e2e]

Options:
  --verbose, -v   show detailed output
  --e2e           run end-to-end integration tests (test/e2e/)

Description:
  Run the eval test suite for the current project, evaluating
  tool call accuracy, output quality, and response relevance.
  With --e2e, runs the framework end-to-end integration tests.

Examples:
  ap test
  ap test --verbose
  ap test --e2e
`)
			return nil
		}
	}

	// E2E 模式：直接运行 test/e2e/ 下的集成测试
	if e2e {
		infof("running end-to-end integration tests...")
		fmt.Println()
		goTestArgs := []string{"test", "-v", "-count=1", "-timeout", "60s", "./test/e2e/..."}
		result, err := runCommand(".", "go", goTestArgs...)
		if err != nil {
			return fmt.Errorf("e2e tests failed:\n%s", result)
		}
		fmt.Println(result)
		successf("all e2e tests passed!")
		return nil
	}

	dir, err := findProjectDir()
	if err != nil {
		return err
	}

	// 检查是否有 eval_test.go 文件（递归扫描子目录）
	hasEval := false
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "eval_") && strings.HasSuffix(name, "_test.go") {
			hasEval = true
			return filepath.SkipDir // 找到即可退出
		}
		return nil
	}); err != nil {
		return fmt.Errorf("扫描 eval 测试文件失败: %w", err)
	}

	if !hasEval {
		infof("eval test file not found, generating template...")
		if err := generateEvalTemplate(dir); err != nil {
			return fmt.Errorf("generate eval template failed: %w", err)
		}
		successf("generated eval_agent_test.go, edit it then re-run ap test")
		return nil
	}

	// 运行 eval 测试
	infof("running eval test suite...")
	fmt.Println()

	goTestArgs := []string{"test", "-v", "-run", "Eval", "./..."}
	if !verbose {
		goTestArgs = []string{"test", "-run", "Eval", "./..."}
	}

	//nolint:gosec // go test 命令参数受控
	result, err := runCommand(dir, "go", goTestArgs...)
	if err != nil {
		return fmt.Errorf("%s", result)
	}

	fmt.Println(result)
	return nil
}

func generateEvalTemplate(dir string) error {
	template := `package main

import (
	"context"
	"log"
	"testing"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// EvalTestSuite defines the agent eval test suite.
// Modify the test cases below to match your agent behavior.
func EvalTestSuite(t *testing.T) {
	// TODO: replace with your actual LLM Provider
	mockLLM := &testMockLLM{}

	agent, err := ap.NewAgent("TestAgent", "you are a test assistant", mockLLM,
		ap.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatal(err)
	}

	t.Run("basic reply", func(t *testing.T) {
		resp, err := agent.Run(context.Background(), ap.UserMessage("hello"))
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
		if resp.Content == "" {
			t.Error("response content is empty")
		}
	})
}

// testMockLLM is a mock LLM for testing.
type testMockLLM struct{}

func (m *testMockLLM) Complete(ctx context.Context, req *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		ID:      "eval-mock-1",
		Content: "this is a test reply",
		Role:    "assistant",
		Usage:   ap.Usage{PromptTokens: 5, CompletionTokens: 10},
	}, nil
}

func (m *testMockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() {
		defer close(ch)
		ch <- ap.Chunk{Content: "this is a test reply", Done: true}
	}()
	return ch, nil
}

func (m *testMockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}

func (m *testMockLLM) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return make([][]float32, len(texts)), nil
}

func (m *testMockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "eval-mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}
`
	return os.WriteFile(dir+"/eval_agent_test.go", []byte(template), 0o644)
}
