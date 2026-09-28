// marshal_concurrent_test.go — Marshal 并发安全回归测试（P0 修复固防）
//
// 背景：Marshal 曾直接返回 pooled buffer 的底层切片，buffer 归还后会被
// 其他 goroutine 的 Reset+Encode 覆写——实测 2000 次并发下 99.5% 的
// 请求体被整体替换（正确性缺陷 + 跨会话数据泄漏）。修复为归还前拷贝，
// 本测试以"并发全部结束后统一比对"（真实使用模式：调用方序列化后交由
// HTTP 传输异步读取）固防，任何时候失败即回归。
package jsonutil

import (
	"fmt"
	"sync"
	"testing"
)

type concurrentMarshalReq struct {
	Model   string `json:"model"`
	Content string `json:"content"`
}

// TestMarshalConcurrentNoAliasing 并发 Marshal 的返回值必须是各自独立的
// 数据，不得因 buffer 池复用而互相覆写。
func TestMarshalConcurrentNoAliasing(t *testing.T) {
	const N = 2000
	results := make([][]byte, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := Marshal(concurrentMarshalReq{
				Model:   fmt.Sprintf("m%d", i),
				Content: fmt.Sprintf("c%d", i),
			})
			if err != nil {
				t.Errorf("Marshal error: %v", err)
				return
			}
			results[i] = got
		}(i)
	}
	wg.Wait()
	// 延迟到全部并发结束后统一比对：若返回值与 pool buffer 别名，
	// 此前的覆写此时必然已发生。
	for i := 0; i < N; i++ {
		want := fmt.Sprintf(`{"model":"m%d","content":"c%d"}`, i, i)
		if results[i] == nil {
			t.Fatalf("结果 %d 为 nil", i)
		}
		if string(results[i]) != want {
			t.Fatalf("结果 %d 被覆写: got %q, want %q（共 %d 项）", i, results[i], want, N)
		}
	}
}

// TestMarshalAppendSafe 修复后返回值不再是 pool buffer 别名，
// 调用方对其 append 不得影响后续 Marshal 的结果。
func TestMarshalAppendSafe(t *testing.T) {
	a, err := Marshal(map[string]string{"k": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	a = append(a, 'X', 'Y') // 触发可能的底层数组扩容/覆写
	b, err := Marshal(map[string]string{"k": "v2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"k":"v2"}` {
		t.Fatalf("后续 Marshal 被前次 append 影响: got %q", b)
	}
}
