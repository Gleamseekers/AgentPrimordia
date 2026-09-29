// main.go — 跨平台 Eval runner（替代 bash 脚本，纯 Go 实现）
// 用法：
//
//	go run ./bench/eval-ci --threshold 0.8 --json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// EvalResult JSON-serializable 结构
type EvalResult struct {
	Total          int     `json:"total"`
	Passed         int     `json:"passed"`
	Failed         int     `json:"failed"`
	Skipped        int     `json:"skipped"`
	PassRate       float64 `json:"pass_rate"`
	Threshold      float64 `json:"threshold"`
	MeetsThreshold bool    `json:"meets_threshold"`
	GoTestExit     int     `json:"go_test_exit"`
	TestFilter     string  `json:"test_filter"`
	TestPackage    string  `json:"test_package"`
	Toolchain      string  `json:"toolchain"`
}

func main() {
	var (
		threshold  = flag.Float64("threshold", 0.8, "最小通过率 [0,1]")
		jsonOutput = flag.Bool("json", false, "输出 JSON 格式结果")
		filter     = flag.String("filter", "TestExactMatch|TestEvalSuite|TestEvaluator", "go test -run 过滤正则")
		pkg        = flag.String("pkg", "github.com/Gleamseekers/AgentPrimordia/internal/agent/eval/...", "go test 包路径")
		toolchain  = flag.String("toolchain", "go1.26.4", "Go toolchain")
	)
	flag.Parse()

	if *threshold < 0 || *threshold > 1 {
		fmt.Fprintf(os.Stderr, "ERROR: --threshold must be in [0, 1], got %f\n", *threshold)
		os.Exit(2)
	}

	// 运行 go test
	goBin, err := lookupGo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(2)
	}

	args := []string{"test", "-count=1", "-v", "-run", *filter, *pkg}
	cmd := exec.Command(goBin, args...)
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOTOOLCHAIN="+*toolchain,
	)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: go test failed: %v\n", err)
			os.Exit(2)
		}
	}
	testOutput := string(out)

	// 解析结果
	passed := countLines(testOutput, "^--- PASS:")
	failed := countLines(testOutput, "^--- FAIL:")
	skipped := countLines(testOutput, "^--- SKIP:")
	total := passed + failed
	if total == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: no tests matched filter %q\n", *filter)
		os.Exit(2)
	}
	passRate := float64(passed) / float64(total)
	meets := passRate >= *threshold

	result := EvalResult{
		Total:          total,
		Passed:         passed,
		Failed:         failed,
		Skipped:        skipped,
		PassRate:       roundFloat(passRate, 4),
		Threshold:      *threshold,
		MeetsThreshold: meets,
		GoTestExit:     exitCode,
		TestFilter:     *filter,
		TestPackage:    *pkg,
		Toolchain:      *toolchain,
	}

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: JSON encode failed: %v\n", err)
			os.Exit(2)
		}
	} else {
		fmt.Println("==> AgentPrimordia Eval Runner (Go)")
		fmt.Printf("    Threshold: %f\n", *threshold)
		fmt.Printf("    Filter:    %s\n", *filter)
		fmt.Printf("    Package:   %s\n", *pkg)
		fmt.Printf("    Toolchain: %s\n\n", *toolchain)

		fmt.Println("==> Results")
		fmt.Printf("    Total:   %d\n", total)
		fmt.Printf("    Passed:  %d\n", passed)
		fmt.Printf("    Failed:  %d\n", failed)
		if skipped > 0 {
			fmt.Printf("    Skipped: %d\n", skipped)
		}
		fmt.Printf("    Rate:    %f (threshold: %f)\n\n", result.PassRate, *threshold)

		if failed > 0 {
			fmt.Println("==> Failed tests:")
			for _, line := range extractFailed(testOutput) {
				fmt.Println("    " + line)
			}
			fmt.Println()
		}

		if meets {
			fmt.Printf("✅ Eval PASSED (rate %f >= threshold %f)\n", result.PassRate, *threshold)
			os.Exit(0)
		} else {
			fmt.Printf("❌ Eval FAILED (rate %f < threshold %f)\n", result.PassRate, *threshold)
			os.Exit(1)
		}
	}

	// JSON 模式下用 exit code 表示是否通过（便于 CI）
	if !meets {
		os.Exit(1)
	}
}

// lookupGo 寻找可用的 go 二进制
func lookupGo() (string, error) {
	for _, name := range []string{"go"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("go binary not found in PATH")
}

// countLines 统计匹配 regex 的行数
func countLines(s, pattern string) int {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if re.MatchString(line) {
			count++
		}
	}
	return count
}

// extractFailed 提取失败的测试名
func extractFailed(s string) []string {
	var failed []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "--- FAIL:") {
			// 去掉 PASS/FAIL 前缀和 timing
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				failed = append(failed, strings.Join(parts[2:len(parts)-1], " "))
			}
		}
	}
	return failed
}

func roundFloat(f float64, places int) float64 {
	shift := 1.0
	for i := 0; i < places; i++ {
		shift *= 10
	}
	return float64(int(f*shift+0.5)) / shift
}

// init 确保 main 不被识别为 unused（让 strconv import 有意义）
var _ = strconv.Itoa
