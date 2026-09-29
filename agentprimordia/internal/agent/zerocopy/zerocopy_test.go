package zerocopy

import (
	"testing"
	"unsafe"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/core"
)

func TestZeroCopyMessage_Create(t *testing.T) {
	content := "这是一条测试消息，包含中文和 English"
	msg := NewZeroCopyMessage(core.RoleUser, content)

	if msg.Role != core.RoleUser {
		t.Errorf("Role = %v, 期望 RoleUser", msg.Role)
	}
	if msg.Content() != content {
		t.Errorf("Content = %q, 期望 %q", msg.Content(), content)
	}
}

func TestZeroCopyMessage_NoAllocation(t *testing.T) {
	// 验证零拷贝不会复制底层字节数组
	original := "hello world"
	msg := NewZeroCopyMessage(core.RoleUser, original)

	// 通过 unsafe 获取底层指针，验证指向同一内存
	originalPtr := unsafe.StringData(original)
	contentPtr := unsafe.StringData(msg.Content())

	if originalPtr != contentPtr {
		t.Error("零拷贝消息应指向原始字符串的内存地址")
	}
}

func TestZeroCopyMessage_BatchConvert(t *testing.T) {
	messages := []string{
		"消息 1",
		"消息 2",
		"消息 3",
	}

	zeroMsgs := BatchConvertToZeroCopy(core.RoleUser, messages)

	if len(zeroMsgs) != 3 {
		t.Errorf("转换后数量 = %d, 期望 3", len(zeroMsgs))
	}

	for i, msg := range zeroMsgs {
		if msg.Content() != messages[i] {
			t.Errorf("消息 %d 内容不匹配", i)
		}
	}
}

func TestZeroCopyMessage_Immutable(t *testing.T) {
	content := "immutable content"
	msg := NewZeroCopyMessage(core.RoleUser, content)

	// 零拷贝消息应该是只读的，Content() 返回的内容应与原始一致
	if msg.Content() != content {
		t.Error("零拷贝消息内容被意外修改")
	}
}

func TestZeroCopyMessage_ToMessage(t *testing.T) {
	content := "convert test"
	msg := NewZeroCopyMessage(core.RoleAssistant, content)

	converted := msg.ToMessage()
	if converted.Role != core.RoleAssistant {
		t.Errorf("Role = %v, 期望 RoleAssistant", converted.Role)
	}
	if converted.Content != content {
		t.Errorf("Content = %q, 期望 %q", converted.Content, content)
	}
}

func TestZeroCopyPool(t *testing.T) {
	pool := NewZeroCopyPool()

	msg := pool.Get(core.RoleUser, "pooled message")
	if msg.Role != core.RoleUser {
		t.Errorf("Role = %v, 期望 RoleUser", msg.Role)
	}
	if msg.Content() != "pooled message" {
		t.Errorf("Content = %q, 期望 %q", msg.Content(), "pooled message")
	}

	pool.Put(msg)

	// 从池中再次获取
	msg2 := pool.Get(core.RoleAssistant, "reused")
	if msg2.Role != core.RoleAssistant {
		t.Errorf("Role = %v, 期望 RoleAssistant", msg2.Role)
	}
	if msg2.Content() != "reused" {
		t.Errorf("Content = %q, 期望 %q", msg2.Content(), "reused")
	}
	pool.Put(msg2)
}

func TestZeroCopyFromBytes(t *testing.T) {
	data := []byte("hello bytes")
	s := ZeroCopyFromBytes(data)
	if s != string(data) {
		t.Errorf("ZeroCopyFromBytes = %q, 期望 %q", s, string(data))
	}
}

func TestZeroCopyFromBytes_Empty(t *testing.T) {
	s := ZeroCopyFromBytes(nil)
	if s != "" {
		t.Errorf("ZeroCopyFromBytes(nil) = %q, 期望空字符串", s)
	}
}

func TestBytesFromZeroCopy(t *testing.T) {
	s := "hello string"
	b := BytesFromZeroCopy(s)
	if string(b) != s {
		t.Errorf("BytesFromZeroCopy = %q, 期望 %q", string(b), s)
	}
}

func TestBytesFromZeroCopy_Empty(t *testing.T) {
	b := BytesFromZeroCopy("")
	if b != nil {
		t.Errorf("BytesFromZeroCopy(\"\") = %v, 期望 nil", b)
	}
}

func BenchmarkZeroCopy_vs_Copy(b *testing.B) {
	content := ""
	for i := 0; i < 100; i++ {
		content += "这是一条较长的测试消息，用于基准测试零拷贝和普通拷贝的性能差异。"
	}

	b.Run("ZeroCopy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = NewZeroCopyMessage(core.RoleUser, content)
		}
	})

	b.Run("Copy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = &core.Message{Role: core.RoleUser, Content: string([]byte(content))}
		}
	})
}
