package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// stdioMaxLineBytes stdio 传输单行最大字节数（1MB）。
	// 旧实现使用 bufio.Scanner 默认 64KB 上限：超长响应行触发 ErrTooLong 后
	// readLoop 静默退出，后续请求全部挂死。现改为有上限的行读取，
	// 超限行跳过（保持行边界同步）而非退出读取循环。
	stdioMaxLineBytes = 1 << 20

	// stdioWriteTimeout stdio 写入超时：子进程不读 stdin 时，避免持锁写入
	// 永久阻塞（exec 管道为 *os.File，支持 SetWriteDeadline）。
	stdioWriteTimeout = 30 * time.Second
)

// errLineTooLong 单行超过 stdioMaxLineBytes 上限
var errLineTooLong = errors.New("stdio line exceeds max length")

// deadlineWriter 支持写超时的写入器（exec 管道 *os.File 实现该方法）
type deadlineWriter interface {
	SetWriteDeadline(t time.Time) error
}

// withWriteDeadline 在写入期间为 w 设置写超时，返回恢复函数（清除 deadline）。
// 超时值取 stdioWriteTimeout 与 ctx deadline 中较短的——请求方取消/超时时
// 写入同步失败，不会继续阻塞。w 不支持 deadline（如测试用 io.Pipe）时退化为直写。
func withWriteDeadline(w io.Writer, ctx context.Context) func() {
	dw, ok := w.(deadlineWriter)
	if !ok {
		return func() {}
	}
	timeout := stdioWriteTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	_ = dw.SetWriteDeadline(time.Now().Add(timeout))
	return func() { _ = dw.SetWriteDeadline(time.Time{}) }
}

// readLineLimited 从 r 读取一行（含结尾 '\n'）。
// 单行超过 limit 字节时，丢弃该行剩余内容（直到换行符）并返回 errLineTooLong，
// 使调用方可以跳过该行继续读取；其他读取错误（含 io.EOF）原样返回，
// 已读到的部分内容随 data 一并返回。
func readLineLimited(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		slice, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			buf = append(buf, slice...)
			if len(buf) > limit {
				// 丢弃该行剩余部分直到换行符，保持后续行边界同步
				for {
					_, err2 := r.ReadSlice('\n')
					if err2 == nil {
						break
					}
					if !errors.Is(err2, bufio.ErrBufferFull) {
						// EOF 等错误：已到流末尾，无需继续丢弃
						return nil, errLineTooLong
					}
				}
				return nil, errLineTooLong
			}
			continue
		}
		if err != nil {
			// io.EOF 或其他错误：连同已读数据返回，由调用方决定如何处理
			buf = append(buf, slice...)
			return buf, err
		}
		buf = append(buf, slice...)
		if len(buf) > limit {
			return nil, errLineTooLong
		}
		return buf, nil
	}
}

// mcpTransport 抽象 MCP 传输层，支持 HTTP 和 stdio 两种模式
type mcpTransport interface {
	// SendRequest 发送 JSON-RPC 请求并等待响应
	SendRequest(ctx context.Context, req *MCPRequest) (*MCPResponse, error)
	// SendNotification 发送 JSON-RPC 通知（无需响应）
	SendNotification(ctx context.Context, req *MCPRequest) error
	// Close 关闭传输层
	Close() error
}

// ===== HTTP Transport =====

type mcpHTTPTransport struct {
	baseURL string
	client  *http.Client
}

func newHTTPTransport(baseURL string) *mcpHTTPTransport {
	// perf-v5 Task 4：添加 timeout 避免 goroutine 永久挂起
	return &mcpHTTPTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *mcpHTTPTransport) SendRequest(ctx context.Context, req *MCPRequest) (*MCPResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.baseURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, mcpMaxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var mcpResp MCPResponse
	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &mcpResp, nil
}

func (t *mcpHTTPTransport) SendNotification(ctx context.Context, req *MCPRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.baseURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (t *mcpHTTPTransport) Close() error { return nil }

// ===== stdio Transport =====

type mcpStdioTransport struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader // 带单行上限的行读取器（替代旧 bufio.Scanner 64KB 默认上限）
	mu     sync.Mutex    // 保护 stdin 写入顺序
	respCh map[int]chan *MCPResponse
	respMu sync.Mutex
}

func newStdioTransport(stdin io.WriteCloser, stdout io.ReadCloser) *mcpStdioTransport {
	t := &mcpStdioTransport{
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
		respCh: make(map[int]chan *MCPResponse),
	}
	// 启动后台 goroutine 持续读取响应
	go t.readLoop()
	return t
}

func (t *mcpStdioTransport) readLoop() {
	for {
		line, err := readLineLimited(t.reader, stdioMaxLineBytes)
		if err != nil {
			if errors.Is(err, errLineTooLong) {
				// 超长行：跳过该行继续读取，避免 readLoop 静默退出导致后续请求挂死
				continue
			}
			// 管道关闭或读取错误，退出循环
			return
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			continue
		}
		var resp MCPResponse
		if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
			continue
		}
		t.respMu.Lock()
		if ch, ok := t.respCh[resp.ID]; ok {
			delete(t.respCh, resp.ID)
			ch <- &resp
		}
		t.respMu.Unlock()
	}
}

func (t *mcpStdioTransport) SendRequest(ctx context.Context, req *MCPRequest) (*MCPResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respCh := make(chan *MCPResponse, 1)
	t.respMu.Lock()
	t.respCh[req.ID] = respCh
	t.respMu.Unlock()

	// 写入 stdin（JSON-RPC over stdio：每行一个 JSON）。
	// 持锁写入并设置写超时：子进程不读 stdin 时写入在 deadline 后失败，
	// 不会永久占用写锁导致后续请求全部挂死。
	t.mu.Lock()
	restore := withWriteDeadline(t.stdin, ctx)
	_, err = fmt.Fprintf(t.stdin, "%s\n", string(reqBody))
	restore()
	t.mu.Unlock()
	if err != nil {
		t.respMu.Lock()
		delete(t.respCh, req.ID)
		t.respMu.Unlock()
		return nil, fmt.Errorf("stdio write: %w", err)
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		t.respMu.Lock()
		delete(t.respCh, req.ID)
		t.respMu.Unlock()
		return nil, ctx.Err()
	}
}

func (t *mcpStdioTransport) SendNotification(ctx context.Context, req *MCPRequest) error {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	// 持锁写入并设置写超时，避免子进程不读 stdin 时永久阻塞
	t.mu.Lock()
	defer t.mu.Unlock()
	restore := withWriteDeadline(t.stdin, ctx)
	_, err = fmt.Fprintf(t.stdin, "%s\n", string(reqBody))
	restore()
	return err
}

func (t *mcpStdioTransport) Close() error {
	var errs []error
	if t.stdin != nil {
		if err := t.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if t.stdout != nil {
		if err := t.stdout.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
