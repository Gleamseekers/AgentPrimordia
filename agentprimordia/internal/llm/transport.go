// transport.go — 共享 HTTP Transport 配置工厂（perf-v5 Task 6）
// 11 个 Provider 之前各自使用默认 http.Transport，高并发下连接池瓶颈；
// 提取共享工厂函数，统一配置 MaxIdleConns/IdleConnTimeout/ForceAttemptHTTP2。
//
// v6.x 评估 §4.2 P1-3：原实现每次调用 NewDefaultLLMTransport() 都 new 全新
// *http.Transport，"共享 transport"名不副实——12 个 Provider 各自持有独立
// 连接池。修复：包级单例（sync.OnceValues），所有 Provider 复用同一实例。
//
// v6.x 评估 §4.2 P1-4：原 NewDefaultLLMClient 的 Client.Timeout（默认 120s）
// 覆盖响应体读取全过程，长流式响应会被整体掐断。修复：流式路径改用
// NewDefaultLLMStreamClient()——不设置 Client.Timeout，由专用 stream
// transport 的 ResponseHeaderTimeout 限制响应头耗时，流式总时长由调用方
// ctx deadline 控制。
package llm

import (
	"net/http"
	"sync"
	"time"
)

// defaultResponseHeaderTimeout 流式请求响应头超时（v6.x 评估 §4.2 P1-4）。
// 仅约束"建连到收到响应头"的耗时；响应体（流式 chunk 序列）不受此限。
const defaultResponseHeaderTimeout = 30 * time.Second

// sharedLLMTransport 包级共享的 HTTP Transport 单例（sync.OnceValue）。
// 非流式请求（Complete/CallTools/Embeddings）复用此连接池。
var sharedLLMTransport = sync.OnceValue(func() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       0, // 0 表示不限制
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false,
		DisableCompression:    false,
		ForceAttemptHTTP2:     true,
	}
})

// sharedLLMStreamTransport 包级共享的流式专用 Transport 单例。
// 与非流式共享 transport 分离的原因：ResponseHeaderTimeout 若加到非流式
// transport 上，会连 Complete 一起受限（推理型模型首字节可能远超 30s，
// 属正常业务耗时），因此流式独立一份、仅流式路径启用响应头超时。
var sharedLLMStreamTransport = sync.OnceValue(func() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       0, // 0 表示不限制
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableKeepAlives:     false,
		DisableCompression:    false,
		ForceAttemptHTTP2:     true,
		// v6.x 评估 §4.2 P1-4：流式路径的响应头超时（替代 Client.Timeout）
		ResponseHeaderTimeout: defaultResponseHeaderTimeout,
	}
})

// NewDefaultLLMTransport 返回 LLM Provider 共享的 HTTP Transport 单例（perf-v5 Task 6 + v6.x P1-3 修复）
//
// 设计要点：
//   - MaxIdleConns=100：全局空闲连接池上限，避免高并发请求耗尽连接
//   - MaxIdleConnsPerHost=10：每个 host 的空闲连接数（Go 默认仅 2）
//   - IdleConnTimeout=90s：空闲连接超时，避免服务端提前关闭导致 EOF
//   - ForceAttemptHTTP2=true：启用 HTTP/2 多路复用
//   - TLSHandshakeTimeout=10s：限制 TLS 握手时间
//   - ExpectContinueTimeout=1s：限制 100-continue 等待时间
//
// 包级单例（sync.OnceValues）：所有 Provider 复用同一连接池实例。
func NewDefaultLLMTransport() *http.Transport {
	return sharedLLMTransport()
}

// NewDefaultLLMClient 创建带共享 transport 的 *http.Client（perf-v5 Task 6）
// timeout 为客户端整体超时（覆盖建连+响应头+响应体读取全过程），
// 适用于非流式请求；流式请求请使用 NewDefaultLLMStreamClient。
func NewDefaultLLMClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: NewDefaultLLMTransport(),
	}
}

// NewDefaultLLMStreamClient 创建流式请求专用的 *http.Client（v6.x 评估 §4.2 P1-4）
//
// 设计要点：
//   - 不设置 Client.Timeout：长流式响应（数分钟持续吐 chunk）不得被整体超时掐断；
//   - Transport.ResponseHeaderTimeout=30s：限制"建连到收到响应头"的耗时，
//     防止服务端挂起不响应；
//   - 流式总时长由调用方通过 ctx deadline 控制（各 Provider 的 Stream 均监听 ctx.Done）。
func NewDefaultLLMStreamClient() *http.Client {
	return &http.Client{
		Transport: sharedLLMStreamTransport(),
	}
}

// CloseTransport 关闭 transport 的空闲连接（perf-v6 round 5 Task 4）
// 用于优雅关闭：释放文件描述符，避免连接泄漏。
// 注意：transport 为包级共享单例，关闭会影响复用同一 transport 的所有 client，
// 仅在进程优雅关闭阶段调用。
func CloseTransport(client *http.Client) {
	if t, ok := client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}
