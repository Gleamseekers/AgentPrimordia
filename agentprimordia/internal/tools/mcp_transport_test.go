package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeStdioChild 在测试中伪造 MCP 子进程：
// 从 reqR 读取客户端写入的请求行，按 responder 逻辑向 respW 写响应行。
// 返回的 stop 用于结束伪造进程。
func fakeStdioChild(reqR *io.PipeReader, respW *io.PipeWriter, responder func(req MCPRequest) []byte) (stop func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		br := bufio.NewReader(reqR)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			var req MCPRequest
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				continue
			}
			out := responder(req)
			if len(out) > 0 {
				if _, err := respW.Write(append(out, '\n')); err != nil {
					return
				}
			}
		}
	}()
	return func() {
		_ = reqR.Close()
		_ = respW.Close()
		<-done
	}
}

// TestStdioTransport_超长行响应_不静默退出 验证 >64KB（旧 bufio.Scanner 默认上限）
// 的响应行能被完整读取，且 readLoop 不会退出——后续请求仍能收到响应。
func TestStdioTransport_超长行响应_不静默退出(t *testing.T) {
	reqR, reqW := io.Pipe()   // 客户端写 reqW → 伪造子进程读 reqR
	respR, respW := io.Pipe() // 伪造子进程写 respW → 客户端读 respR

	tr := newStdioTransport(reqW, respR)
	defer tr.Close()

	// 伪造子进程：对每个请求回一条约 100KB 的合法 JSON 响应行
	stop := fakeStdioChild(reqR, respW, func(req MCPRequest) []byte {
		pad := strings.Repeat("a", 100*1024) // 100KB > 64KB 旧上限
		resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"echo":%q}}`, req.ID, pad)
		return []byte(resp)
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := tr.SendRequest(ctx, &MCPRequest{JSONRPC: "2.0", ID: 1, Method: "ping"})
	if err != nil {
		t.Fatalf("超长行（100KB）响应应被完整读取，实际错误: %v", err)
	}
	if resp.ID != 1 {
		t.Fatalf("响应 ID = %d, 期望 1", resp.ID)
	}

	// readLoop 未静默退出：第二个请求同样应成功
	resp2, err := tr.SendRequest(ctx, &MCPRequest{JSONRPC: "2.0", ID: 2, Method: "ping"})
	if err != nil {
		t.Fatalf("readLoop 不应退出，第二个请求应成功: %v", err)
	}
	if resp2.ID != 2 {
		t.Fatalf("第二个响应 ID = %d, 期望 2", resp2.ID)
	}
}

// TestStdioTransport_超限行被跳过_readLoop存活 验证超过单行上限（1MB）的行被跳过
// 而非导致 readLoop 退出：对应请求超时失败，但连接保持可用。
func TestStdioTransport_超限行被跳过_readLoop存活(t *testing.T) {
	reqR, reqW := io.Pipe()
	respR, respW := io.Pipe()

	tr := newStdioTransport(reqW, respR)
	defer tr.Close()

	// 伪造子进程：先回一条 >1MB 的超限行（无中间换行），再回正常响应
	go func() {
		br := bufio.NewReader(reqR)
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var req1 MCPRequest
		_ = json.Unmarshal([]byte(line), &req1)
		huge := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"pad":%q}}`,
			req1.ID, strings.Repeat("b", 2*1024*1024)) // 2MB，超过单行上限
		if _, err := respW.Write([]byte(huge + "\n")); err != nil {
			return
		}
		line2, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var req2 MCPRequest
		_ = json.Unmarshal([]byte(line2), &req2)
		resp2 := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, req2.ID)
		_, _ = respW.Write([]byte(resp2 + "\n"))
	}()

	// 请求 1 的响应行超限被丢弃 → 请求失败（用小超时防止测试卡死）
	ctx1, cancel1 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel1()
	if _, err := tr.SendRequest(ctx1, &MCPRequest{JSONRPC: "2.0", ID: 1, Method: "ping"}); err == nil {
		t.Fatal("超过单行上限的响应行应被丢弃，请求 1 应失败")
	}

	// readLoop 应仍然存活：请求 2 能收到正常响应
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	resp2, err := tr.SendRequest(ctx2, &MCPRequest{JSONRPC: "2.0", ID: 2, Method: "ping"})
	if err != nil {
		t.Fatalf("readLoop 应在超限行之后继续存活，请求 2 应成功: %v", err)
	}
	if resp2.ID != 2 {
		t.Fatalf("请求 2 响应 ID = %d, 期望 2", resp2.ID)
	}
}

// TestStdioTransport_写超时_子进程不读不永久阻塞 验证 stdin 写入带 deadline：
// 子进程不读 stdin 且管道写满时，写入应在 deadline 后失败，而不是永久挂起。
func TestStdioTransport_写超时_子进程不读不永久阻塞(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	defer pipeR.Close()
	defer pipeW.Close()

	// 后台持续写入占满内核管道缓冲区，之后任何写入都会阻塞
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
	time.Sleep(200 * time.Millisecond) // 等待管道被填满

	respR, _ := io.Pipe()
	defer respR.Close()
	tr := newStdioTransport(pipeW, respR)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = tr.SendRequest(ctx, &MCPRequest{JSONRPC: "2.0", ID: 1, Method: "ping"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("管道写满后写入应超时失败，而非永久阻塞")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("写入应受 deadline 约束快速失败，实际耗时 %v", elapsed)
	}
}

// TestStdioTransport_通知写入_同样受deadline约束 验证 SendNotification 的写入
// 也带超时，不会因持锁写入无限阻塞。
func TestStdioTransport_通知写入_同样受deadline约束(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}
	defer pipeR.Close()
	defer pipeW.Close()

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

	respR, _ := io.Pipe()
	defer respR.Close()
	tr := newStdioTransport(pipeW, respR)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = tr.SendNotification(ctx, &MCPRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("管道写满时通知写入应超时失败")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("通知写入应受 deadline 约束快速失败，实际耗时 %v", elapsed)
	}
}
