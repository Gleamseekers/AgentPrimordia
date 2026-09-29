// flagset_test.go — FlagSet 注入能力测试（config New() 硬绑 flag.CommandLine 锐边修复）
//
// 背景：New() 原硬绑全局 flag.CommandLine 且 LoadFlags 裸解析 os.Args[1:]，
// 嵌入方/测试二进制携带外来 flag（-test.* 等）时会被误当业务 flag 解析失败。
// 本文件固化修复后的契约：
//   - WithFlagSet Option 注入自定义 FlagSet；New() 无选项时行为不变（仍绑 flag.CommandLine）；
//   - LoadFlagsFrom(args) 显式 args 解析入口：外来 flag 与业务 flag 同集并存不互相干扰；
//   - LoadFlags 对"注入且未解析"的 FlagSet 返回指引错误，而非裸解析 os.Args；
//   - 业务 flag 注册去重由 Visit 改 VisitAll，修复"预注册但未设置"的重复注册 panic。
package config

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newDiscardFlagSet 构造静默的 ContinueOnError FlagSet（测试辅助）。
func newDiscardFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// TestNew_BindsCommandLineByDefault New() 无选项时保持既有行为：绑定 flag.CommandLine。
func TestNew_BindsCommandLineByDefault(t *testing.T) {
	t.Parallel()
	ldr, err := New(&testConfig{}, "AP")
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if ldr.flagSet != flag.CommandLine {
		t.Error("New() 应默认绑定 flag.CommandLine（向后兼容）")
	}
}

// TestNew_WithFlagSet_InjectsFlagSet WithFlagSet 注入自定义 FlagSet。
func TestNew_WithFlagSet_InjectsFlagSet(t *testing.T) {
	t.Parallel()
	fs := newDiscardFlagSet("embedder")
	ldr, err := New(&testConfig{}, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if ldr.flagSet != fs {
		t.Fatal("WithFlagSet 应替换默认 FlagSet")
	}
}

// TestNew_WithFlagSet_NilFallsBackToCommandLine nil FlagSet 防御性回退默认。
func TestNew_WithFlagSet_NilFallsBackToCommandLine(t *testing.T) {
	t.Parallel()
	ldr, err := New(&testConfig{}, "AP", WithFlagSet(nil))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if ldr.flagSet != flag.CommandLine {
		t.Error("WithFlagSet(nil) 应回退到 flag.CommandLine")
	}
}

// TestNew_WithFlagSet_ValidatesCfgFirst Option 不跳过 cfg 校验（nil / 类型化 nil 均拒绝）。
func TestNew_WithFlagSet_ValidatesCfgFirst(t *testing.T) {
	t.Parallel()
	fs := newDiscardFlagSet("embedder")
	if _, err := New(nil, "AP", WithFlagSet(fs)); err == nil {
		t.Error("nil cfg 即使带 WithFlagSet 也应报错")
	}
	var typedNil *testConfig
	if _, err := New(typedNil, "AP", WithFlagSet(fs)); err == nil {
		t.Error("类型化 nil 指针即使带 WithFlagSet 也应报错")
	}
}

// TestLoadFlagsFrom_ForeignFlagsCoexist 外来 flag 与业务 flag 同一 FlagSet 并存不互相干扰。
func TestLoadFlagsFrom_ForeignFlagsCoexist(t *testing.T) {
	fs := newDiscardFlagSet("embedder")
	// 嵌入方预注册自有 flag（用法/默认值由嵌入方掌控）
	var logLevel string
	fs.StringVar(&logLevel, "log-level", "info", "嵌入方自有 flag")

	cfg := &testConfig{Name: "default", Port: 80}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	// 混合传参：外来 flag + 未预注册的业务 flag（由 LoadFlagsFrom 注册）
	if err := ldr.LoadFlagsFrom([]string{"-log-level=debug", "-name=from-flag", "-port=9090"}); err != nil {
		t.Fatalf("LoadFlagsFrom error: %v", err)
	}
	if logLevel != "debug" {
		t.Errorf("外来 flag 应正常解析: log-level = %q, want debug", logLevel)
	}
	if cfg.Name != "from-flag" {
		t.Errorf("Name = %q, want from-flag", cfg.Name)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
}

// TestLoadFlagsFrom_RegistersAndWritesBack 未预注册的业务 flag 自动注册、解析并回写。
func TestLoadFlagsFrom_RegistersAndWritesBack(t *testing.T) {
	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{Port: 80}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if err := ldr.LoadFlagsFrom([]string{"-name=abc", "-port=7", "-verbose=true"}); err != nil {
		t.Fatalf("LoadFlagsFrom error: %v", err)
	}
	if cfg.Name != "abc" || cfg.Port != 7 || !cfg.Verbose {
		t.Fatalf("业务 flag 回写不符: %+v", cfg)
	}
	// 注册后可从 FlagSet 查询到业务 flag
	if got := fs.Lookup("name"); got == nil || got.Value.String() != "abc" {
		t.Errorf("业务 flag 应已注册并可读: %+v", got)
	}
}

// TestLoadFlagsFrom_ParseError 未定义 flag → 解析错误。
func TestLoadFlagsFrom_ParseError(t *testing.T) {
	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if err := ldr.LoadFlagsFrom([]string{"-undefined-flag=x"}); err == nil {
		t.Fatal("未定义 flag 应返回解析错误")
	}
}

// TestLoadFlagsFrom_EmptyArgs 空 args → 无操作，字段保持默认值。
func TestLoadFlagsFrom_EmptyArgs(t *testing.T) {
	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{Name: "keep", Port: 80}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if err := ldr.LoadFlagsFrom(nil); err != nil {
		t.Fatalf("LoadFlagsFrom(nil) error: %v", err)
	}
	if cfg.Name != "keep" || cfg.Port != 80 {
		t.Fatalf("空 args 不应改动配置: %+v", cfg)
	}
}

// TestLoadFlags_InjectedUnparsed_NoOsArgsBareParse 注入且未解析的 FlagSet 上
// LoadFlags 不得裸解析 os.Args（测试二进制外来 flag 误解析锐边）→ 返回指引错误。
func TestLoadFlags_InjectedUnparsed_NoOsArgsBareParse(t *testing.T) {
	// 模拟测试二进制：os.Args 含外来 flag（本测试不并行，立即恢复）
	oldArgs := os.Args
	os.Args = []string{"prog", "-test.timeout=10m", "-undefined-foreign=x"}
	defer func() { os.Args = oldArgs }()

	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	err = ldr.LoadFlags()
	if err == nil {
		t.Fatal("注入且未解析的 FlagSet 上 LoadFlags 应返回指引错误而非裸解析 os.Args")
	}
	if !strings.Contains(err.Error(), "LoadFlagsFrom") {
		t.Errorf("错误信息应指引使用 LoadFlagsFrom: %v", err)
	}
}

// TestLoadFlags_InjectedPreParsed_IgnoresOsArgs 注入且已自解析的 FlagSet 上
// LoadFlags 直接回写，不触碰 os.Args（即使 os.Args 含外来未定义 flag）。
func TestLoadFlags_InjectedPreParsed_IgnoresOsArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"prog", "-foreign-undefined=x"}
	defer func() { os.Args = oldArgs }()

	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{Name: "default", Port: 80}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	// 嵌入方自行注册并解析业务 flag
	fs.String("name", "default", "")
	fs.Int("port", 80, "")
	if err := fs.Parse([]string{"-name=pre-parsed", "-port=1234"}); err != nil {
		t.Fatal(err)
	}
	if err := ldr.LoadFlags(); err != nil {
		t.Fatalf("LoadFlags error: %v", err)
	}
	if cfg.Name != "pre-parsed" || cfg.Port != 1234 {
		t.Fatalf("回写不符: %+v", cfg)
	}
}

// TestLoadFlags_PreRegisteredNotSet_NoRedefinePanic 预注册但未设置的业务 flag
// 不得重复注册（Visit→VisitAll 修复；修复前 fs.String 二次注册 panic）。
func TestLoadFlags_PreRegisteredNotSet_NoRedefinePanic(t *testing.T) {
	fs := newDiscardFlagSet("embedder")
	fs.String("name", "preset", "") // 预注册但不在 args 中设置
	cfg := &testConfig{Name: "field-default"}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	// 修复前：defined 用 Visit 收集（只见已设置 flag）→ name 漏检 → registerFlag 重复注册 panic
	if err := ldr.LoadFlagsFrom(nil); err != nil {
		t.Fatalf("LoadFlagsFrom error: %v", err)
	}
	if cfg.Name != "field-default" {
		t.Errorf("未设置的 flag 不应回写覆盖字段值: %q", cfg.Name)
	}
	if got := fs.Lookup("name").Value.String(); got != "preset" {
		t.Errorf("预注册 flag 值应保留: %q", got)
	}
}

// TestMustLoad_WithInjectedFlagSet 注入 FlagSet 的全链路（YAML < ENV < flags），
// 且全程不触碰 os.Args（测试二进制外来 flag 无干扰）。
func TestMustLoad_WithInjectedFlagSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("name: yaml-name\nport: 1111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newDiscardFlagSet("embedder")
	cfg := &testConfig{}
	ldr, err := New(cfg, "AP", WithFlagSet(fs))
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	fs.String("name", "", "")
	if err := fs.Parse([]string{"-name=from-flag"}); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("AP_NAME")
	os.Unsetenv("AP_PORT")
	os.Unsetenv("AP_VERBOSE")

	if err := ldr.MustLoad(path); err != nil {
		t.Fatalf("MustLoad error: %v", err)
	}
	// flags > YAML：name 被 flag 覆盖；port 的 flag 未设置 → 保持 YAML 值
	if cfg.Name != "from-flag" {
		t.Errorf("Name = %q, want from-flag（flags 优先级最高）", cfg.Name)
	}
	if cfg.Port != 1111 {
		t.Errorf("Port = %d, want 1111（YAML 值，flag 未设置）", cfg.Port)
	}
}

// TestLoadFlags_DefaultCommandLine_LoadFromOSArgs 默认（未注入）场景保持既有行为：
// 未解析的 flag.CommandLine 上解析 os.Args[1:] 并回写。
// 测试二进制中 testing 框架已预解析 flag.CommandLine，LoadFlags 走不到该分支，
// 故直接单测拆出的 loadFromOSArgs——生产二进制中 MustLoad→LoadFlags 正是此路径。
func TestLoadFlags_DefaultCommandLine_LoadFromOSArgs(t *testing.T) {
	// 操作进程全局 flag.CommandLine 与 os.Args，本测试不并行；
	// 使用唯一 flag 名（osargs-probe-*）避免与其他测试/框架 flag 冲突
	type osArgsProbe struct {
		ProbeName string `flag:"osargs-probe-name"`
		ProbePort int    `flag:"osargs-probe-port"`
	}
	cfg := &osArgsProbe{ProbePort: 80}
	ldr := NewOrFatal(cfg) // 默认绑定 flag.CommandLine

	oldArgs := os.Args
	os.Args = []string{"prog", "-osargs-probe-name=from-os-args", "-osargs-probe-port=4321"}
	defer func() { os.Args = oldArgs }()

	ldr.registerConfigFlags() // 注册到 flag.CommandLine（全局，唯一名）
	if err := ldr.loadFromOSArgs(); err != nil {
		t.Fatalf("loadFromOSArgs error: %v", err)
	}
	if cfg.ProbeName != "from-os-args" || cfg.ProbePort != 4321 {
		t.Fatalf("os.Args 解析回写不符: %+v", cfg)
	}
}

// TestMustLoad_LoadEnvError MustLoad 链路在 LoadEnv 失败时中止并返回错误。
func TestMustLoad_LoadEnvError(t *testing.T) {
	// env 标签的不支持类型（map）+ 环境变量已设置 → setField 报错
	cfg := &struct {
		M map[string]string `env:"M"`
	}{}
	ldr, _ := flagLoader(cfg)
	t.Setenv("AP_M", "x")
	if err := ldr.MustLoad(filepath.Join(t.TempDir(), "nonexist.yaml")); err == nil {
		t.Fatal("LoadEnv 失败应使 MustLoad 返回错误")
	}
}

// TestMustLoad_LoadFlagsError MustLoad 链路在 LoadFlags 失败时中止并返回错误，
// 且不继续执行 Validate（注入 FlagSet 未解析 → 指引错误）。
func TestMustLoad_LoadFlagsError(t *testing.T) {
	cfg := &testConfig{}
	ldr, _ := flagLoader(cfg) // 注入的 FlagSet 未解析
	validated := false
	ldr.AddValidator(func() error { validated = true; return nil })
	if err := ldr.MustLoad(filepath.Join(t.TempDir(), "nonexist.yaml")); err == nil {
		t.Fatal("LoadFlags 失败应使 MustLoad 返回错误")
	}
	if validated {
		t.Error("LoadFlags 失败后不应执行 Validate")
	}
}
