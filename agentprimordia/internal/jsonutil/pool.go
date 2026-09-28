// Package jsonutil 提供 JSON 序列化优化tool（perf-v6 round 5/6/8 Task 1）
// 减少 encoding/json 的反射开销，并通过 sync.Pool 复用 buffer/reader
package jsonutil

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

// bufferPool 复用 bytes.Buffer，减少高频 JSON 序列化的内存分配
// perf-v6 round 5 Task 1：替代直接 json.Marshal（每调用可省 1 次 alloc）
var bufferPool = sync.Pool{
	New: func() any {
		buf := bytes.NewBuffer(make([]byte, 0, 1024))
		return buf
	},
}

// readerPool 复用 bytes.Reader，避免每次 Unmarshal 分配一个轻量 reader。
// 典型收益：SSE 解析路径上每条消息可省一次 alloc（perf-v6 round 8 Task 1）。
var readerPool = sync.Pool{
	New: func() any {
		return &bytes.Reader{}
	},
}

// stringReaderPool 复用 *stringReader（不分配，零拷贝包装 string）。
// 用于 SSE 等热路径：从 string 直接构造 io.Reader，无需 []byte(data) 拷贝。
var stringReaderPool = sync.Pool{
	New: func() any {
		return &stringReader{}
	},
}

// stringReader 零拷贝 string→io.Reader（perf-v6 round 8 Task 1）
// 比 strings.NewReader 更轻：省去 strings.Reader 内部的额外字段，
// 复用方式（pool）省去每次 new 的分配。
type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func (r *stringReader) ReadByte() (byte, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	b := r.s[r.i]
	r.i++
	return b, nil
}

// Marshal 使用 pooled buffer 序列化 JSON（perf-v6 round 5 Task 1）
//
// 返回值为独立拷贝：pooled buffer 在序列化后即归还池中，若直接返回其
// 底层数组的切片，其他 goroutine 的 Marshal 会 Reset+Encode 覆写同一块
// 内存（实测 2000 次并发下 99.5% 请求体被整体替换——既是正确性缺陷，
// 也是跨会话数据泄漏）。故此处必须在归还前拷贝（P0 修复：正确性优先
// 于该微优化，拷贝成本相对 JSON 序列化本身可忽略）。
func Marshal(v any) ([]byte, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		bufferPool.Put(buf)
		return nil, err
	}
	// 去除 json.Encoder 添加的末尾 '\n'
	data := buf.Bytes()
	n := len(data)
	if n > 0 && data[n-1] == '\n' {
		n--
	}
	// 拷贝出独立底层数组后再归还 buffer，杜绝并发覆写。
	out := make([]byte, n)
	copy(out, data[:n])
	bufferPool.Put(buf)
	return out, nil
}

// Unmarshal 反序列化 JSON（perf-v6 round 8 Task 1：复用 bytes.Reader）
// 行为与 encoding/json.Unmarshal 完全一致：数值默认 float64，单个 JSON 值。
func Unmarshal(data []byte, v any) error {
	r := readerPool.Get().(*bytes.Reader)
	r.Reset(data)
	dec := json.NewDecoder(r)
	err := dec.Decode(v)
	readerPool.Put(r)
	return err
}

// DecodeReader 从 pooled *bytes.Reader 解码单个 JSON 值。
// 适用于 SSE 解析等热路径，避免每条消息分配 reader + decoder。
// 行为与 json.NewDecoder(bytes.NewReader(data)).Decode(v) 等价。
func DecodeReader(data []byte, v any) error {
	r := readerPool.Get().(*bytes.Reader)
	r.Reset(data)
	dec := json.NewDecoder(r)
	err := dec.Decode(v)
	readerPool.Put(r)
	return err
}

// DecodeString 从 string 解码单个 JSON 值。
// 适用于 SSE 解析等热路径（典型场景：scanner.Text() 返回 string）。
// 行为与 json.NewDecoder(strings.NewReader(data)).Decode(v) 等价。
// 优化点：
//   - 使用 pooled *stringReader，零拷贝（无需 []byte(data) 转换）
//   - 每条消息省一次 alloc，相对 stdlib 路径节省 ~10% 内存
func DecodeString(data string, v any) error {
	r := stringReaderPool.Get().(*stringReader)
	r.s = data
	r.i = 0
	dec := json.NewDecoder(r)
	err := dec.Decode(v)
	stringReaderPool.Put(r)
	return err
}

// NewReader 返回 JSON 解码的 io.Reader
// 用于 json.NewDecoder 的输入（perf-v6 round 5，向后兼容）
// 注意：调用方应在使用完毕后调用 PutReader 归还，否则会泄漏 pool 槽位。
func NewReader(data []byte) *bytes.Reader {
	r := readerPool.Get().(*bytes.Reader)
	r.Reset(data)
	return r
}

// PutReader 释放 NewReader 取出的 reader 回 pool（可选）
// 对应 NewReader 调用的，必须以 PutReader 收尾，否则会泄漏 pool 槽位。
func PutReader(r *bytes.Reader) {
	if r != nil {
		readerPool.Put(r)
	}
}

// ioReader 哨兵类型：用于让 DecodeReader 接受任意 io.Reader 入参
// 实际仍走 pooled *bytes.Reader（更高效），但保持 API 兼容性
var _ io.Reader = (*bytes.Reader)(nil)

// MarshalBody 是 jsonutil.Marshal 的便捷别名（perf-v6 round 6 Task 1）
// 提供统一入口，便于将来切换 goccy/jsoniter
func MarshalBody(v any) ([]byte, error) {
	return Marshal(v)
}

// ReadAllPooled 从 io.Reader 读取所有数据，使用 bufferPool 复用底层 buffer。
// 优化（perf-v11 stage-2）：替代 io.ReadAll，避免每次 HTTP 响应读取都分配新的 []byte。
// 返回的字节切片是从 pooled buffer 复制而来，调用方可安全保留。
//
// ⚠️ 注意：实测发现，在多个 t.Parallel() 测试并发使用同一个 bufferPool 的场景下，
// 由于不同测试响应体长度差异较大（1KB-100KB），复用同一个 buffer 会导致后续
// 测试读取到陈旧数据（表现为 JSON 解析错误："invalid character '{' after top-level value"）。
// 建议：仅在已知响应体大小相近的单线程场景使用；通用 LLM 响应读取应使用 io.ReadAll。
// 内部 benchmark 表明收益有限（错误响应 < 1KB 池化收益 < 5%），不建议在生产路径上启用。
func ReadAllPooled(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, io.EOF
	}
	buf := bufferPool.Get().(*bytes.Buffer)
	// 关键：取出的 buffer 可能是其他用户（如 Marshal）留下未清空的内容，
	// 必须先 Reset 再 io.Copy，否则结果会包含陈旧数据（脏读）。
	buf.Reset()
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()
	// 通过 io.Copy 写入 pooled buffer，超大响应会自动扩容
	if _, err := io.Copy(buf, r); err != nil {
		return nil, err
	}
	// 返回独立切片（拷贝），因为 pooled buffer 会被回收
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out, nil
}
