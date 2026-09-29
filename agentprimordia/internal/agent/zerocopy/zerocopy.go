// Package zerocopy 提供零拷贝消息传递，减少内存分配
package zerocopy

import (
	"sync"
	"unsafe"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

// ZeroCopyMessage 零拷贝消息，直接引用原始字符串内存
type ZeroCopyMessage struct {
	Role    core.Role
	content string
}

// NewZeroCopyMessage 创建零拷贝消息（不复制底层字节数组）
func NewZeroCopyMessage(role core.Role, content string) *ZeroCopyMessage {
	return &ZeroCopyMessage{
		Role:    role,
		content: content,
	}
}

// Content 返回消息内容（零拷贝，直接返回原始字符串引用）
func (m *ZeroCopyMessage) Content() string {
	return m.content
}

// ToMessage 转换为标准 Message（此时才复制）
func (m *ZeroCopyMessage) ToMessage() core.Message {
	return core.Message{
		Role:    m.Role,
		Content: m.content,
	}
}

// BatchConvertToZeroCopy 批量转换为零拷贝消息
func BatchConvertToZeroCopy(role core.Role, contents []string) []*ZeroCopyMessage {
	msgs := make([]*ZeroCopyMessage, len(contents))
	for i, c := range contents {
		msgs[i] = NewZeroCopyMessage(role, c)
	}
	return msgs
}

// ZeroCopyPool 零拷贝消息对象池，减少 GC 压力
type ZeroCopyPool struct {
	pool sync.Pool
}

// NewZeroCopyPool 创建消息对象池
func NewZeroCopyPool() *ZeroCopyPool {
	return &ZeroCopyPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &ZeroCopyMessage{}
			},
		},
	}
}

// Get 从池中获取消息
func (p *ZeroCopyPool) Get(role core.Role, content string) *ZeroCopyMessage {
	msg := p.pool.Get().(*ZeroCopyMessage)
	msg.Role = role
	msg.content = content
	return msg
}

// Put 归还消息到池
func (p *ZeroCopyPool) Put(msg *ZeroCopyMessage) {
	msg.Role = ""
	msg.content = ""
	p.pool.Put(msg)
}

// ZeroCopyFromBytes 从字节数组创建零拷贝字符串（不复制）
func ZeroCopyFromBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}

// BytesFromZeroCopy 从零拷贝字符串获取字节数组（不复制）
func BytesFromZeroCopy(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
