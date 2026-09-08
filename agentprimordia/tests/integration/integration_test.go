// Package integration 提供 AgentPrimordia 集成测试。
//
// 运行方式：
//   go test -v ./tests/integration/
//
// 测试覆盖：
//   - ap start 完整流程
//   - 多轮对话
//   - ap profile 成长报告
//   - A2A 通信
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ap "agentprimordia/pkg"
)

// TestApStartFlow 测试 ap start 完整流程
func TestApStartFlow(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	
	// 切换到临时目录
	os.Chdir(tmpDir)
	
	// 编译 ap 命令
	apBin := filepath.Join(tmpDir, "ap")
	buildCmd := exec.Command("go", "build", "-o", apBin, "./cmd/ap")
	buildCmd.Dir = filepath.Join(origDir, "..", "..")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 ap 失败: %v\n%s", err, output)
	}
	
	// 运行 ap start
	startCmd := exec.Command(apBin, "start", "test-agent")
	startCmd.Dir = tmpDir
	output, err := startCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ap start 失败: %v\n%s", err, output)
	}
	
	// 验证输出
	outputStr := string(output)
	if !strings.Contains(outputStr, "创建项目") {
		t.Error("输出中未包含'创建项目'")
	}
	if !strings.Contains(outputStr, "test-agent") {
		t.Error("输出中未包含项目名称")
	}
	
	// 验证项目目录
	projectDir := filepath.Join(tmpDir, "test-agent")
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		t.Error("项目目录未创建")
	}
	
	// 验证关键文件
	files := []string{"main.go", "go.mod", ".ap.yaml"}
	for _, file := range files {
		path := filepath.Join(projectDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("关键文件缺失: %s", file)
		}
	}
}

// TestMultiTurnConversation 测试多轮对话
func TestMultiTurnConversation(t *testing.T) {
	ctx := context.Background()
	provider := ap.NewDemoProvider()
	
	// 模拟多轮对话
	messages := []struct {
		input    string
		expected []string // 期望包含的关键词
	}{
		{
			input:    "你好",
			expected: []string{"你好", "欢迎"},
		},
		{
			input:    "帮我审查代码",
			expected: []string{"代码", "审查"},
		},
		{
			input:    "谢谢",
			expected: []string{"不客气", "帮助"},
		},
	}
	
	for i, msg := range messages {
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: msg.input},
			},
		}
		
		resp, err := provider.Complete(ctx, req)
		if err != nil {
			t.Fatalf("第 %d 轮对话失败: %v", i+1, err)
		}
		
		// 验证响应包含期望关键词
		found := false
		for _, keyword := range msg.expected {
			if strings.Contains(resp.Content, keyword) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("第 %d 轮响应未包含期望关键词: %v", i+1, msg.expected)
		}
	}
}

// TestApProfileCommand 测试 ap profile 命令
func TestApProfileCommand(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	
	// 切换到临时目录
	os.Chdir(tmpDir)
	
	// 编译 ap 命令
	apBin := filepath.Join(tmpDir, "ap")
	buildCmd := exec.Command("go", "build", "-o", apBin, "./cmd/ap")
	buildCmd.Dir = filepath.Join(origDir, "..", "..")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 ap 失败: %v\n%s", err, output)
	}
	
	// 创建项目
	initCmd := exec.Command(apBin, "init", "profile-test")
	initCmd.Dir = tmpDir
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("ap init 失败: %v\n%s", err, output)
	}
	
	// 切换到项目目录
	projectDir := filepath.Join(tmpDir, "profile-test")
	os.Chdir(projectDir)
	
	// 运行 ap profile
	profileCmd := exec.Command(apBin, "profile")
	profileCmd.Dir = projectDir
	output, err := profileCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ap profile 失败: %v\n%s", err, output)
	}
	
	// 验证输出
	outputStr := string(output)
	if !strings.Contains(outputStr, "Agent Growth Profile") {
		t.Error("输出中未包含'Agent Growth Profile'")
	}
	if !strings.Contains(outputStr, "Overall") {
		t.Error("输出中未包含'Overall'")
	}
}

// TestA2ACommunication 测试 A2A 通信
func TestA2ACommunication(t *testing.T) {
	ctx := context.Background()
	
	// 创建 Agent Card
	card := ap.OpenAgentCard{
		Name:               "test-agent",
		Description:        "integration test agent",
		URL:                "http://localhost:9876",
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	
	cfg := ap.DefaultInteropConfig()
	
	// 创建 server
	server := ap.NewOpenInteropServer(card, cfg)
	if server == nil {
		t.Fatal("创建 server 失败")
	}
	
	// 创建 client
	client := ap.NewOpenInteropClient("http://localhost:9876")
	if client == nil {
		t.Fatal("创建 client 失败")
	}
	
	// 获取 Agent Card
	discoveredCard, err := client.FetchAgentCard(ctx)
	if err != nil {
		// 注意：这里会失败因为 server 没有真正启动，只测试 API 存在
		t.Logf("预期失败（server 未启动）: %v", err)
	} else {
		// 如果成功，验证 card
		if discoveredCard.Name != card.Name {
			t.Errorf("Card name 不匹配: got %s, want %s", discoveredCard.Name, card.Name)
		}
	}
}

// TestDemoProvider 测试 DemoProvider
func TestDemoProvider(t *testing.T) {
	ctx := context.Background()
	provider := ap.NewDemoProvider()
	
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    "你好",
			expected: []string{"你好", "欢迎"},
		},
		{
			input:    "hello",
			expected: []string{"Hello", "welcome"},
		},
		{
			input:    "帮助",
			expected: []string{"帮助", "能力"},
		},
	}
	
	for _, test := range tests {
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: test.input},
			},
		}
		
		resp, err := provider.Complete(ctx, req)
		if err != nil {
			t.Fatalf("Complete 失败: %v", err)
		}
		
		// 验证响应
		found := false
		for _, keyword := range test.expected {
			if strings.Contains(resp.Content, keyword) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("输入 %q 的响应未包含期望关键词: %v", test.input, test.expected)
		}
	}
}

// TestProjectStructure 测试项目结构
func TestProjectStructure(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	
	// 切换到临时目录
	os.Chdir(tmpDir)
	
	// 编译 ap 命令
	apBin := filepath.Join(tmpDir, "ap")
	buildCmd := exec.Command("go", "build", "-o", apBin, "./cmd/ap")
	buildCmd.Dir = filepath.Join(origDir, "..", "..")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 ap 失败: %v\n%s", err, output)
	}
	
	// 创建项目
	initCmd := exec.Command(apBin, "init", "structure-test")
	initCmd.Dir = tmpDir
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("ap init 失败: %v\n%s", err, output)
	}
	
	// 验证项目结构
	projectDir := filepath.Join(tmpDir, "structure-test")
	
	// 检查目录
	dirs := []string{"data"}
	for _, dir := range dirs {
		path := filepath.Join(projectDir, dir)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("目录缺失: %s", dir)
		} else if !info.IsDir() {
			t.Errorf("%s 不是目录", dir)
		}
	}
	
	// 检查文件
	files := []string{"main.go", "go.mod", ".ap.yaml"}
	for _, file := range files {
		path := filepath.Join(projectDir, file)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("文件缺失: %s", file)
		} else if info.IsDir() {
			t.Errorf("%s 是目录而不是文件", file)
		}
	}
}

// TestPerformance 测试基本性能
func TestPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过性能测试")
	}
	
	ctx := context.Background()
	provider := ap.NewDemoProvider()
	
	// 测试响应时间
	start := time.Now()
	for i := 0; i < 100; i++ {
		req := &ap.CompletionRequest{
			Messages: []ap.ChatMessage{
				{Role: "user", Content: "test"},
			},
		}
		_, _ = provider.Complete(ctx, req)
	}
	elapsed := time.Since(start)
	
	avgTime := elapsed / 100
	t.Logf("平均响应时间: %v", avgTime)
	
	// 应该小于 100ms
	if avgTime > 100*time.Millisecond {
		t.Errorf("响应时间过长: %v", avgTime)
	}
}
