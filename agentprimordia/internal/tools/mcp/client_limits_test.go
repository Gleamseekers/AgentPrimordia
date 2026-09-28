package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// newPipedClient 用内存管道构造一个不依赖真实子进程的 Client，
// 用于直接测试 readLoop / sendRequest 的行处理逻辑。
// childIn：伪造子进程读取客户端写入的请求；childOut：伪造子进程写响应给客户端。
func newPipedClient(t *testing.T) (client *Client, childIn *io.PipeReader, childOut *io.PipeWriter) {
	t.Helper()
	inR, inW := io.Pipe()   // 客户端写 inW → 伪造子进程读 inR
	outR, outW := io.Pipe() // 伪造子进程写 outW → 客户端读 outR

	c := &Client{
		stdin:    inW,
		stdout:   bufio.NewReader(outR),
		pending:  make(map[int64]chan *jsonRPCResponse),
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout:  2 * time.Second,
		done:     make(chan struct{}),
		reapDone: make(chan struct{}),
	}
	go c.readLoop()
	t.Cleanup(func() {
		close(c.done)
		_ = inW.Close()
		_ = outW.Close()
	})
	return c, inR, outW
}

// TestClientReadLoop_超长行响应_不静默退出 验证 >64KB 的响应行可被完整读取，
// 且 readLoop 不退出（后续请求仍能收到响应）。
func TestClientReadLoop_超长行响应_不静默退出(t *testing.T) {
	c, childIn, childOut := newPipedClient(t)

	go func() {
		br := bufio.NewReader(childIn)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			var req jsonRPCRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			pad := strings.Repeat("a", 100*1024) // 100KB > 64KB 旧 Scanner 上限
			resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"echo":%q}}`, req.ID, pad)
			if _, err := childOut.Write([]byte(resp + "\n")); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.sendRequest(ctx, "ping", nil); err != nil {
		t.Fatalf("超长行（100KB）响应应被完整读取，实际错误: %v", err)
	}
	// readLoop 未退出：第二个请求同样成功
	if _, err := c.sendRequest(ctx, "ping", nil); err != nil {
		t.Fatalf("readLoop 不应退出，第二个请求应成功: %v", err)
	}
}

// TestClientReadLoop_超限行被跳过_readLoop存活 验证超过单行上限（16MB）的行
// 被跳过而非导致 readLoop 退出。
func TestClientReadLoop_超限行被跳过_readLoop存活(t *testing.T) {
	c, childIn, childOut := newPipedClient(t)

	go func() {
		br := bufio.NewReader(childIn)
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var req1 jsonRPCRequest
		_ = json.Unmarshal([]byte(line), &req1)
		// 超限行：超过单行上限，无中间换行
		huge := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"pad":%q}}`,
			req1.ID, strings.Repeat("b", 17*1024*1024)) // 17MB
		if _, err := childOut.Write([]byte(huge + "\n")); err != nil {
			return
		}
		line2, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var req2 jsonRPCRequest
		_ = json.Unmarshal([]byte(line2), &req2)
		resp2 := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, req2.ID)
		_, _ = childOut.Write([]byte(resp2 + "\n"))
	}()

	// 请求 1 的响应行超限被丢弃 → 失败（小超时防止测试卡死）
	ctx1, cancel1 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel1()
	if _, err := c.sendRequest(ctx1, "ping", nil); err == nil {
		t.Fatal("超过单行上限的响应行应被丢弃，请求 1 应失败")
	}

	// readLoop 应仍存活：请求 2 成功
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := c.sendRequest(ctx2, "ping", nil); err != nil {
		t.Fatalf("readLoop 应在超限行之后继续存活，请求 2 应成功: %v", err)
	}
}

// TestClientSendRequest_写超时_子进程不读不永久阻塞 验证 stdin 写入带 deadline：
// 子进程不读且管道写满时，写入快速失败而非永久挂起。
func TestClientSendRequest_写超时_子进程不读不永久阻塞(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	defer pipeR.Close()
	defer pipeW.Close()

	// 占满内核管道缓冲区
	stopFill := make(chan struct{})
	go func() {
		block := make([]byte, 64*1024)
		for {
			select {
			case <-stopFill:
				return
			default:
			}
			if _, err := pipeW.Write(block); err != nil {
				return
			}
		}
	}()
	defer close(stopFill)
	time.Sleep(200 * time.Millisecond)

	c := &Client{
		stdin:    pipeW,
		stdout:   bufio.NewReader(strings.NewReader("")), // 立即 EOF，readLoop 退出（本测试只关心写入）
		pending:  make(map[int64]chan *jsonRPCResponse),
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout:  2 * time.Second,
		done:     make(chan struct{}),
		reapDone: make(chan struct{}),
	}
	close(c.done) // readLoop 无需运行

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = c.sendRequest(ctx, "ping", nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("管道写满后写入应超时失败，而非永久阻塞")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("写入应受 deadline 约束快速失败，实际耗时 %v", elapsed)
	}
}

// TestBuildChildEnv_最小白名单 验证子进程环境为最小白名单 + 用户显式配置：
// 宿主敏感变量不泄露，白名单必要项保留，用户配置可覆盖。
func TestBuildChildEnv_最小白名单(t *testing.T) {
	t.Setenv("AP_MCP_TEST_SECRET", "leak-me")
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")

	env := buildChildEnv(map[string]string{
		"MCP_CUSTOM": "v1",
		"PATH":       "/custom/bin", // 用户配置覆盖白名单项
	})

	got := make(map[string]string, len(env))
	pathCount := 0
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		got[k] = v
		if k == "PATH" {
			pathCount++
		}
	}

	if _, ok := got["AP_MCP_TEST_SECRET"]; ok {
		t.Error("宿主敏感变量不应传递给 MCP 子进程")
	}
	if got["MCP_CUSTOM"] != "v1" {
		t.Errorf("用户显式配置的环境变量应传递，got %q", got["MCP_CUSTOM"])
	}
	if got["PATH"] != "/custom/bin" {
		t.Errorf("用户配置应覆盖白名单变量，PATH = %q", got["PATH"])
	}
	if pathCount != 1 {
		t.Errorf("PATH 应唯一（去重），实际出现 %d 次", pathCount)
	}
	if home := os.Getenv("HOME"); home != "" && got["HOME"] != home {
		t.Errorf("HOME 应在白名单中保留，got %q, want %q", got["HOME"], home)
	}
}

// TestBuildChildEnv_空配置也过滤宿主环境 验证即使用户未配置任何环境变量，
// 子进程也不会继承完整宿主环境（旧实现 cmd.Env 为 nil 时全量继承）。
func TestBuildChildEnv_空配置也过滤宿主环境(t *testing.T) {
	t.Setenv("AP_MCP_TEST_SECRET", "leak-me")

	env := buildChildEnv(nil)
	for _, kv := range env {
		if strings.HasPrefix(kv, "AP_MCP_TEST_SECRET=") {
			t.Fatal("空配置时也不应继承宿主敏感变量")
		}
	}
}
