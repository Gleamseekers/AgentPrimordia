// sse.go — sse 子包的类型别名与构造转发，保持向后兼容
package agent

import (
	"io"
	"net/http"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/sse"
)

// SSEWriter 服务端推送事件写入器（sse 子包别名）。
// 实现 SSE 协议的 data/event/id/retry 字段，并发安全。
type SSEWriter = sse.SSEWriter

// NewSSEWriter 创建 SSE 写入器（转发到 sse 子包）。
func NewSSEWriter(w io.Writer, flusher http.Flusher) *SSEWriter {
	return sse.NewSSEWriter(w, flusher)
}
