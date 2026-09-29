// bufferpool.go — bufferpool 子包的函数包装，保持向后兼容
package agent

import (
	"bytes"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/bufferpool"
)

// AcquireBuffer 从池中获取一个空 bytes.Buffer。
// 委托到 bufferpool 子包，保持向后兼容
func AcquireBuffer() *bytes.Buffer {
	return bufferpool.AcquireBuffer()
}

// ReleaseBuffer 归还 buffer 到池中。
// 委托到 bufferpool 子包，保持向后兼容
func ReleaseBuffer(buf *bytes.Buffer) {
	bufferpool.ReleaseBuffer(buf)
}

// AcquireBufferWithSize 获取一个预分配指定大小的 buffer。
// 委托到 bufferpool 子包，保持向后兼容
func AcquireBufferWithSize(size int) *bytes.Buffer {
	return bufferpool.AcquireBufferWithSize(size)
}
