// artifact.go — 手工字节组装「最小有效 WASM 工具模块」。
//
// 范式出处：agentprimordia/wasm/runtime_e2e_test.go 的 validTestWASM()
// （memory/alloc/tool_execute/free 四导出 + type/function/memory/export/code
// 段）。本文件在示例侧重新实现一份，原因有两条且都不可绕过：
//
//  1. validTestWASM 是 *_test.go 内的未导出函数，示例包无法引用；
//  2. AGENTS.md §4.2 要求 ecosystem/examples/* 只经 pkg/ 公共 API 与核心交互，
//     不直接 import internal/*（Go internal 规则同样禁止跨模块引用）。
//
// 与范式出处的差异只有一处，且是为「可对账」服务的：本模块多一个 data 段，
// tool_execute 会把宿主写入线性内存的参数逐字节求和（mod 256）后以两位小写
// 十六进制写回载荷占位符，宿主再把结果读回。这样沙箱内的计算可以由宿主侧
// 独立复算对账——证明参数真的经过了 WASM 线性内存往返，而不是常量返回。
//
// 组装口径：全部 opcode / 段 id / magic 均以字面量给出，仅长度与索引走
// LEB128 编码；不依赖任何外部 WASM 工具链，输出字节级确定（同输入必得同字节）。
package main

import "encoding/json"

// ===== WASM 二进制格式常量（MVP 核心子集）=====

const (
	wasmMagic0 = 0x00 // '\0'
	wasmMagic1 = 0x61 // 'a'
	wasmMagic2 = 0x73 // 's'
	wasmMagic3 = 0x6d // 'm'
	wasmVer0   = 0x01
	wasmVer1   = 0x00
	wasmVer2   = 0x00
	wasmVer3   = 0x00
)

// 段 id（WASM 规范；本模块只用到 1/3/5/7/10/11）。
const (
	sectionType     = 0x01
	sectionFunction = 0x03
	sectionMemory   = 0x05
	sectionExport   = 0x07
	sectionCode     = 0x0a
	sectionData     = 0x0b
)

// 值类型 / 导出种类 / 本模块用到的 opcode。
const (
	valI32      = 0x7f
	exportFunc  = 0x00
	exportMem   = 0x02
	opBlock     = 0x02
	opLoop      = 0x03
	opEnd       = 0x0b
	opBr        = 0x0c
	opBrIf      = 0x0d
	opLocalGet  = 0x20
	opLocalSet  = 0x21
	opI32Const  = 0x41
	opI32Add    = 0x6a
	opI32And    = 0x71
	opI32ShrU   = 0x76
	opI32GeU    = 0x4f
	opI32Load8U = 0x2d
	opI32Store8 = 0x3a
	voidBlock   = 0x40
)

// 导出名（ABI 契约，见 wasm/sandbox.go ExecuteWithMemory 的调用协议注释）。
const (
	exportMemoryName = "memory"
	exportAllocName  = "alloc"
	exportExecute    = "tool_execute"
	exportFreeName   = "free"
)

// ===== 模块载荷与内存布局 =====

const (
	// payloadTemplate 是 data 段里的 JSON 载荷模板，两位 "__" 是运行时占位符
	// （被 tool_execute 写回十六进制校验和）。字段名与 wasm.WASMToolResult
	// （content/is_error）对齐，宿主侧可直接解出结果。
	payloadTemplate = `{"content":"wasm-tool-exec ok; input-checksum=__","is_error":false}`

	// hexTable 是 16 字节十六进制数字表，tool_execute 用它做 nibble→字符的
	// 查表转换（省掉分支，字节码更短更易审）。
	hexTable = "0123456789abcdef"

	// allocBumpOffset 是 alloc 返回的固定分配基点。本 demo 每次执行只分配
	// 一次，常量桩足够；真实工件需自行实现分配器（此处如实标注为桩）。
	allocBumpOffset = 256
)

// hexPlaceholder 载荷模板中的两位占位符（用于定位写回偏移）。
const hexPlaceholder = "__"

// buildWASMToolModule 组装最小有效 WASM 工具模块并返回其字节。
//
// 模块结构（对照 runtime_e2e_test.go validTestWASM 的同款段落）：
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "alloc") (param i32) (result i32) i32.const 256)
//	  (func (export "tool_execute") (param i32 i32) (result i32 i32) ...)
//	  (func (export "free") (param i32 i32))
//	  (data (i32.const 0) "<payload><hex table>"))
func buildWASMToolModule() []byte {
	b := &wasmBuilder{buf: make([]byte, 0, 256)}

	// magic + version（与 validTestWASM 完全同款字面量）
	b.buf = append(b.buf,
		wasmMagic0, wasmMagic1, wasmMagic2, wasmMagic3,
		wasmVer0, wasmVer1, wasmVer2, wasmVer3,
	)

	// ① type section (id=1)：三个函数型
	//   type0: (i32,i32) -> (i32,i32)   ← tool_execute
	//   type1: (i32)     -> (i32)       ← alloc
	//   type2: (i32,i32) -> ()          ← free
	b.section(sectionType, []byte{
		0x03, // 3 个类型
		0x60, 0x02, valI32, valI32, 0x02, valI32, valI32,
		0x60, 0x01, valI32, 0x01, valI32,
		0x60, 0x02, valI32, valI32, 0x00,
	})

	// ② function section (id=3)：func0=alloc→type1，func1=tool_execute→type0，func2=free→type2
	b.section(sectionFunction, []byte{0x03, 0x01, 0x00, 0x02})

	// ③ memory section (id=5)：1 个内存，limits flags=0（仅 min），min 1 页
	b.section(sectionMemory, []byte{0x01, 0x00, 0x01})

	// ④ export section (id=7)：四个导出，函数索引须与 function section 对齐
	b.section(sectionExport, concat([]byte{0x04},
		exportEntry(exportMemoryName, exportMem, 0),
		exportEntry(exportAllocName, exportFunc, 0),
		exportEntry(exportExecute, exportFunc, 1),
		exportEntry(exportFreeName, exportFunc, 2),
	))

	// ⑤ code section (id=10)：三个函数体
	b.section(sectionCode, concat([]byte{0x03},
		codeEntry(allocBody()),
		codeEntry(toolExecuteBody()),
		codeEntry(freeBody()),
	))

	// ⑥ data section (id=11)：单个 active 段（memory 0, offset 0），
	//    内容 = 载荷模板 + 十六进制表（tool_execute 查表用）
	b.section(sectionData, dataSectionBytes())

	return b.buf
}

// wasmBuilder 以字节级精度累积 WASM 模块。
type wasmBuilder struct {
	buf []byte
}

// section 追加一个段：id + uleb(payload 长度) + payload。
func (b *wasmBuilder) section(id byte, payload []byte) {
	b.buf = append(b.buf, id)
	b.buf = appendULEB128(b.buf, uint32(len(payload)))
	b.buf = append(b.buf, payload...)
}

// exportEntry 组装一个 export 段条目：name 长度 + name + kind + index。
func exportEntry(name string, kind byte, index uint32) []byte {
	e := appendULEB128(nil, uint32(len(name)))
	e = append(e, name...)
	e = append(e, kind)
	return appendULEB128(e, index)
}

// codeEntry 组装一个 code 段条目：body 长度前缀 + body。
func codeEntry(body []byte) []byte {
	e := appendULEB128(nil, uint32(len(body)))
	return append(e, body...)
}

// dataSectionBytes 组装 data 段 payload：1 个 active 段（memory 0, offset 0）。
// offset 表达式即 (i32.const 0)(end)。
func dataSectionBytes() []byte {
	data := append([]byte(nil), payloadTemplate...)
	data = append(data, hexTable...)
	p := []byte{0x01, 0x00} // 1 个段 + flags=0（active, memory 0）
	p = append(p, opI32Const, 0x00, opEnd)
	p = appendULEB128(p, uint32(len(data)))
	return append(p, data...)
}

// allocBody 函数体：返回固定分配基点 allocBumpOffset（常量桩）。
func allocBody() []byte {
	c := &codeBuf{}
	c.raw(0x00) // 0 个 local group
	c.i32Const(allocBumpOffset)
	c.raw(opEnd)
	return c.b
}

// toolExecuteBody 函数体：把宿主写入线性内存的参数逐字节求和（mod 256），
// 以两位小写十六进制写回载荷占位符，返回 (0, len(payload))。
//
// 局部变量布局：param0=ptr（宿主写入的参数地址），param1=len（参数长度），
// local2=i（循环下标），local3=sum（累加和）。
func toolExecuteBody() []byte {
	c := &codeBuf{}
	// 1 个 local group：2 × i32（local2=i, local3=sum）
	c.raw(0x01, 0x02, valI32)
	// i = 0 ; sum = 0
	c.i32Const(0).localSet(2)
	c.i32Const(0).localSet(3)
	// block { loop { if (i >=u len) break block; sum += mem[ptr+i]; i++; continue loop } }
	c.raw(opBlock, voidBlock)
	c.raw(opLoop, voidBlock)
	c.localGet(2).localGet(1).raw(opI32GeU).raw(opBrIf, 0x01)
	c.localGet(3) // sum
	c.localGet(0).localGet(2).raw(opI32Add)
	c.raw(opI32Load8U, 0x00, 0x00) // align=0 offset=0
	c.raw(opI32Add).localSet(3)    // sum += (u8) mem[ptr+i]
	c.localGet(2).i32Const(1).raw(opI32Add).localSet(2)
	c.raw(opBr, 0x00)
	c.raw(opEnd) // loop
	c.raw(opEnd) // block
	// 注意 WASM 栈序：i32.store8 先从栈顶弹出「值」、再弹出「地址」，
	// 因此必须先压地址、后压值（本模块的 memarg 均取 align=0/offset=0）。
	// mem[hi 占位符] = hexTable[(sum >>u 4) & 0x0f]
	c.i32Const(hiPlaceholderOffset())
	c.localGet(3).i32Const(4).raw(opI32ShrU).i32Const(0x0f).raw(opI32And)
	c.i32Const(hexTableOffset()).raw(opI32Add)
	c.raw(opI32Load8U, 0x00, 0x00)
	c.raw(opI32Store8, 0x00, 0x00)
	// mem[lo 占位符] = hexTable[sum & 0x0f]
	c.i32Const(loPlaceholderOffset())
	c.localGet(3).i32Const(0x0f).raw(opI32And)
	c.i32Const(hexTableOffset()).raw(opI32Add)
	c.raw(opI32Load8U, 0x00, 0x00)
	c.raw(opI32Store8, 0x00, 0x00)
	// 返回 (0, len(payload))
	c.i32Const(0).i32Const(int32(len(payloadTemplate)))
	c.raw(opEnd)
	return c.b
}

// freeBody 函数体：空操作（宿主对返回内存的 free 调用对本桩无意义）。
func freeBody() []byte {
	c := &codeBuf{}
	c.raw(0x00) // 0 个 local group
	c.raw(opEnd)
	return c.b
}

// codeBuf 函数体字节累积器（带常用指令的语义化助手）。
type codeBuf struct{ b []byte }

// raw 原样追加字节（opcode 与无立即数指令）。
func (c *codeBuf) raw(bytes ...byte) *codeBuf {
	c.b = append(c.b, bytes...)
	return c
}

// i32Const 追加 i32.const（有符号 LEB128 立即数）。
func (c *codeBuf) i32Const(v int32) *codeBuf {
	c.b = append(c.b, opI32Const)
	c.b = appendSLEB128(c.b, v)
	return c
}

// localGet 追加 local.get <idx>。
func (c *codeBuf) localGet(idx uint32) *codeBuf {
	c.b = append(c.b, opLocalGet)
	c.b = appendULEB128(c.b, idx)
	return c
}

// localSet 追加 local.set <idx>。
func (c *codeBuf) localSet(idx uint32) *codeBuf {
	c.b = append(c.b, opLocalSet)
	c.b = appendULEB128(c.b, idx)
	return c
}

// ===== 载荷布局辅助 =====

// indexPlaceholder 返回载荷模板中两位占位符的起始下标。
func indexPlaceholder() int {
	for i := 0; i+len(hexPlaceholder) <= len(payloadTemplate); i++ {
		if payloadTemplate[i:i+len(hexPlaceholder)] == hexPlaceholder {
			return i
		}
	}
	return -1
}

// hiPlaceholderOffset 高位十六进制占位符的字节偏移。
func hiPlaceholderOffset() int32 { return int32(indexPlaceholder()) }

// loPlaceholderOffset 低位十六进制占位符的字节偏移。
func loPlaceholderOffset() int32 { return int32(indexPlaceholder() + 1) }

// hexTableOffset 十六进制表在 data 段（即线性内存 offset 0 起）中的偏移。
func hexTableOffset() int32 { return int32(len(payloadTemplate)) }

// ===== LEB128 与字节拼接辅助 =====

// appendULEB128 追加无符号 LEB128（段长度、向量计数、索引）。
func appendULEB128(dst []byte, v uint32) []byte {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

// appendSLEB128 追加有符号 LEB128（i32.const 立即数）。
func appendSLEB128(dst []byte, v int32) []byte {
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			more = false
		} else {
			b |= 0x80
		}
		dst = append(dst, b)
	}
	return dst
}

// concat 顺序拼接多个字节切片。
func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// inputChecksumHex 复算宿主传入参数的字节和（mod 256）的两位小写十六进制，
// 与 tool_execute 在沙箱内的计算对账使用。
func inputChecksumHex(input []byte) string {
	var sum byte
	for _, c := range input {
		sum += c
	}
	const hexDigits = "0123456789abcdef"
	return string([]byte{hexDigits[sum>>4], hexDigits[sum&0x0f]})
}

// toolInputJSON 复刻 wasm.WASMToolAdapter.ExecuteTool 构造宿主输入的方式
// （map 序列化，键序由 encoding/json 排序保证确定），供独立复算对账。
func toolInputJSON(toolName string, args json.RawMessage) ([]byte, error) {
	return json.Marshal(map[string]any{
		"tool_name": toolName,
		"args":      args,
	})
}
