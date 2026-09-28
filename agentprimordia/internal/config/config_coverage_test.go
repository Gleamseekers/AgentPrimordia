// config_coverage_test.go — 配置包覆盖率补强（热加载 / flags / 解析边界）
//
// 目标（只新增测试，不改生产代码）：
//   - loader.go：LoadYAML 坏 YAML 与读错误、LoadEnv 嵌入结构体/跳过标签/
//     类型错误、LoadFlags 全类型注册与回写、MustLoad 链路、ToJSON 失败路径；
//   - hot_reload.go：默认轮询间隔、空路径/读失败、check() 的 stat 失败/
//     内容未变/onChange 失败路径、WatchConfigFile 错误路径；
//   - feature.go：LoadFromFile 边界（不存在/坏 JSON/合并语义）。
package config

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ===== loader.go =====

// TestNew_NilTypedPointer 类型化 nil 指针也应被拒绝。
func TestNew_NilTypedPointer(t *testing.T) {
	t.Parallel()
	var p *testConfig
	if _, err := New(p, "AP"); err == nil {
		t.Error("类型化 nil 指针应返回错误")
	}
}

// TestLoadYAML_BadYAMLAndReadError 坏 YAML 与读错误（目录路径）两条错误路径。
func TestLoadYAML_BadYAMLAndReadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// 坏 YAML：语法错误 → 解析错误
	badPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(badPath, []byte("name: [unclosed\n\t- x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &testConfig{}
	ldr := NewOrFatal(cfg)
	if err := ldr.LoadYAML(badPath); err == nil {
		t.Fatal("坏 YAML 应返回解析错误")
	}

	// 读错误：路径是目录 → os.ReadFile 报错（非 NotExist）→ 包装错误
	subDir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg2 := &testConfig{}
	ldr2 := NewOrFatal(cfg2)
	if err := ldr2.LoadYAML(subDir); err == nil {
		t.Fatal("目录路径应返回读错误")
	}
}

// TestLoadEnv_EmbeddedAndSkip 嵌入结构体递归、env:"-" 跳过、未设置变量保持原值。
func TestLoadEnv_EmbeddedAndSkip(t *testing.T) {
	type Inner struct {
		Host string `env:"INNER_HOST"`
	}
	type Outer struct {
		Inner
		Name    string            `env:"OUTER_NAME"`
		Skipped string            `env:"-"`
		Extra   map[string]string `env:"OUTER_MAP"` // 不支持的类型 → 错误
	}
	cfg := &Outer{Name: "default", Skipped: "keep"}
	ldr := NewOrFatal(cfg)

	t.Setenv("AP_OUTER_NAME", "from-env")
	t.Setenv("AP_INNER_HOST", "inner-host")
	t.Setenv("AP_OUTER_SKIPPED", "should-not-apply")

	// map 字段带 env 标签但未设置对应变量 → 不会触达 setField，应成功
	if err := ldr.LoadEnv(); err != nil {
		t.Fatalf("LoadEnv error: %v", err)
	}
	if cfg.Name != "from-env" {
		t.Errorf("Name = %q, want from-env", cfg.Name)
	}
	if cfg.Host != "inner-host" {
		t.Errorf("嵌入结构体 Host = %q, want inner-host", cfg.Host)
	}
	if cfg.Skipped != "keep" {
		t.Errorf("env:\"-\" 字段不应被改写: %q", cfg.Skipped)
	}
}

// TestLoadEnv_TypeErrors env 值类型不符时的四条错误路径（int/uint/bool/float/不支持类型）。
func TestLoadEnv_TypeErrors(t *testing.T) {
	tests := []struct {
		name   string
		cfg    any
		envKey string
		envVal string
	}{
		{
			name:   "非法整数",
			cfg:    &struct{ N int `env:"N"` }{},
			envKey: "AP_N", envVal: "not-a-number",
		},
		{
			name:   "非法无符号整数",
			cfg:    &struct{ U uint `env:"U"` }{},
			envKey: "AP_U", envVal: "-5",
		},
		{
			name:   "非法布尔",
			cfg:    &struct{ B bool `env:"B"` }{},
			envKey: "AP_B", envVal: "maybe",
		},
		{
			name:   "非法浮点",
			cfg:    &struct{ F float64 `env:"F"` }{},
			envKey: "AP_F", envVal: "abc",
		},
		{
			name:   "不支持的字段类型",
			cfg:    &struct{ M map[string]string `env:"M"` }{},
			envKey: "AP_M", envVal: "x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envVal)
			ldr := NewOrFatal(tt.cfg)
			err := ldr.LoadEnv()
			if err == nil {
				t.Fatal("期望类型错误")
			}
			if !strings.Contains(err.Error(), tt.envKey) {
				t.Errorf("错误信息应含环境变量名 %q: %v", tt.envKey, err)
			}
		})
	}
}

// TestLoadEnv_AllKinds setField 全类型成功路径（string/int/uint/bool/float/slice）。
func TestLoadEnv_AllKinds(t *testing.T) {
	cfg := &struct {
		S string    `env:"S"`
		I int64     `env:"I"`
		U uint16    `env:"U"`
		B bool      `env:"B"`
		F float32   `env:"F"`
		T []string  `env:"T"`
	}{}
	ldr := NewOrFatal(cfg)
	t.Setenv("AP_S", "hello")
	t.Setenv("AP_I", "-42")
	t.Setenv("AP_U", "7")
	t.Setenv("AP_B", "1")
	t.Setenv("AP_F", "2.5")
	t.Setenv("AP_T", "a,b")

	if err := ldr.LoadEnv(); err != nil {
		t.Fatalf("LoadEnv error: %v", err)
	}
	if cfg.S != "hello" || cfg.I != -42 || cfg.U != 7 || !cfg.B || cfg.F != 2.5 {
		t.Fatalf("标量解析不符: %+v", cfg)
	}
	if len(cfg.T) != 2 || cfg.T[0] != "a" || cfg.T[1] != "b" {
		t.Fatalf("切片解析不符: %v", cfg.T)
	}
}

// flagLoader 构造带自定义 FlagSet 的 Loader（包内测试直构，避免污染 flag.CommandLine）。
func flagLoader(cfg any) (*Loader, *flag.FlagSet) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 静默解析错误输出
	return &Loader{cfg: cfg, envPrefix: "AP", flagSet: fs}, fs
}

// TestLoadFlags_AllKinds LoadFlags 全类型注册（registerFlag 的 float64/uint
// 分支）、预解析跳过、值回写结构体（setByFlag）。
func TestLoadFlags_AllKinds(t *testing.T) {
	type Embedded struct {
		Depth int `flag:"depth"`
	}
	cfg := &struct {
		Embedded
		Name    string  `flag:"name"`
		Port    int     `flag:"port"`
		Ratio   float64 `flag:"ratio"`
		Workers uint    `flag:"workers"`
		Verbose bool    `flag:"verbose"`
		NoTag   string  // 无 flag 标签 → 不注册
	}{Port: 80, Ratio: 1.0, Workers: 4}
	ldr, fs := flagLoader(cfg)

	// 预注册并解析 string/int/bool（defined 命中 → LoadFlags 跳过重复注册）；
	// float64/uint 不预注册 → 走 registerFlag 的 Float64/Uint 分支
	fs.String("name", "flag-name", "")
	fs.Int("port", 80, "")
	fs.Bool("verbose", false, "")
	if err := fs.Parse([]string{"-name=from-flag", "-port=9090", "-verbose=true"}); err != nil {
		t.Fatal(err)
	}

	if err := ldr.LoadFlags(); err != nil {
		t.Fatalf("LoadFlags error: %v", err)
	}
	// 已设置的 flag 回写（覆盖 YAML/env 层）
	if cfg.Name != "from-flag" {
		t.Errorf("Name = %q, want from-flag", cfg.Name)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if !cfg.Verbose {
		t.Error("Verbose 应为 true")
	}
	// registerFlag 以字段当前值注册默认值，未设置的 flag 保持默认
	if cfg.Ratio != 1.0 || cfg.Workers != 4 {
		t.Errorf("未设置 flag 应保持默认值: %+v", cfg)
	}
	// Depth 无 flag 标签……（嵌入字段有标签但未预注册且未设置 → 保持 0）
	if cfg.Depth != 0 {
		t.Errorf("Depth 应保持 0, got %d", cfg.Depth)
	}
}

// TestLoadFlags_DefinedSkip 已定义的 flag 不重复注册（Visit 去重路径）。
func TestLoadFlags_DefinedSkip(t *testing.T) {
	cfg := &struct {
		Name string `flag:"name"`
	}{}
	ldr, fs := flagLoader(cfg)
	fs.String("name", "preset", "")
	if err := fs.Parse([]string{"-name=preset"}); err != nil {
		t.Fatal(err)
	}
	// defined 含 name → LoadFlags 跳过注册，直接回写
	if err := ldr.LoadFlags(); err != nil {
		t.Fatalf("LoadFlags error: %v", err)
	}
	if cfg.Name != "preset" {
		t.Errorf("Name = %q, want preset", cfg.Name)
	}
}

// TestLoadFlags_ParseError flag 解析失败路径（os.Args 注入未定义 flag）。
func TestLoadFlags_ParseError(t *testing.T) {
	cfg := &struct {
		Name string `flag:"name"`
	}{}
	ldr, fs := flagLoader(cfg)

	// 临时替换 os.Args（本测试不并行；立即恢复，避免影响其他测试）
	oldArgs := os.Args
	os.Args = []string{"prog", "-undefined-flag=x"}
	defer func() { os.Args = oldArgs }()

	if err := ldr.LoadFlags(); err == nil {
		t.Fatal("未定义 flag 应返回解析错误")
	}
	_ = fs
}

// TestMustLoad 全链路：YAML < ENV < flags 优先级 + Validate 执行。
func TestMustLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("name: yaml-name\nport: 1111\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &testConfig{}
	ldr, fs := flagLoader(cfg)
	// 只预注册并解析 name：LoadFlags 时 name 命中 defined 跳过重复注册，
	// port 走 registerFlag 注册（默认值取 YAML 已写入的 1111）
	fs.String("name", "", "")
	if err := fs.Parse([]string{"-name=from-flag"}); err != nil {
		t.Fatal(err)
	}

	// 不设 AP_* 环境变量：YAML 值生效，flags 最高优先级
	os.Unsetenv("AP_NAME")
	os.Unsetenv("AP_PORT")
	os.Unsetenv("AP_VERBOSE")

	validated := false
	ldr.AddValidator(func() error {
		validated = true
		return nil
	})
	if err := ldr.MustLoad(path); err != nil {
		t.Fatalf("MustLoad error: %v", err)
	}
	if !validated {
		t.Error("MustLoad 应执行 Validate")
	}
	// flags > YAML：name 被 flag 覆盖；port 的 flag 未设置 → 保持 YAML 值
	if cfg.Name != "from-flag" {
		t.Errorf("Name = %q, want from-flag（flags 优先级最高）", cfg.Name)
	}
	if cfg.Port != 1111 {
		t.Errorf("Port = %d, want 1111（YAML 值，flag 未设置）", cfg.Port)
	}
}

// TestMustLoad_YAMLError MustLoad 在 YAML 解析失败时中止并返回错误。
func TestMustLoad_YAMLError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(":\n:[broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &testConfig{}
	ldr, _ := flagLoader(cfg)
	if err := ldr.MustLoad(path); err == nil {
		t.Fatal("坏 YAML 应使 MustLoad 失败")
	}
}

// TestToJSON_MarshalError 含 chan 字段的配置序列化失败 → 错误占位串。
func TestToJSON_MarshalError(t *testing.T) {
	t.Parallel()
	cfg := &struct {
		Ch chan int `json:"ch"`
	}{Ch: make(chan int)}
	got := ToJSON(cfg)
	if !strings.HasPrefix(got, "<marshal error:") {
		t.Errorf("ToJSON 应返回错误占位串, got %q", got)
	}
}

// ===== hot_reload.go =====

// TestNewConfigWatcher_DefaultInterval 零值间隔取默认 5s。
func TestNewConfigWatcher_DefaultInterval(t *testing.T) {
	t.Parallel()
	w := NewConfigWatcher(ConfigWatcherOptions{Path: "/tmp/x"})
	if w.interval != defaultPollInterval {
		t.Errorf("interval = %v, want %v", w.interval, defaultPollInterval)
	}
	if w.stopCh == nil {
		t.Error("stopCh 不应为 nil")
	}
}

// TestConfigWatcher_EmptyPath 空路径 Start 报错。
func TestConfigWatcher_EmptyPath(t *testing.T) {
	t.Parallel()
	w := NewConfigWatcher(ConfigWatcherOptions{Path: ""})
	if err := w.Start(); err == nil {
		t.Fatal("空路径应返回错误")
	}
}

// TestConfigWatcher_ReadErrorOnStart stat 成功但读取失败（目录路径）→ Start 报错。
func TestConfigWatcher_ReadErrorOnStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := NewConfigWatcher(ConfigWatcherOptions{
		Path:     dir, // 目录：Stat 成功、ReadFile 失败
		Interval: 50 * time.Millisecond,
	})
	if err := w.Start(); err == nil {
		t.Fatal("目录路径应返回读取错误")
	}
}

// TestConfigWatcher_CheckStatFails 运行中文件被删除 → check() stat 失败安全返回。
func TestConfigWatcher_CheckStatFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w := NewConfigWatcher(ConfigWatcherOptions{
		Path:     path,
		Interval: 50 * time.Millisecond,
		OnChange: func([]byte) error { calls.Add(1); return nil },
	})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	// 删除文件 → 后续 poll 的 stat 失败 → warn 返回（不应 panic）
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("onChange 应只在启动时调用 1 次, got %d", got)
	}
}

// TestConfigWatcher_TouchSameContent 内容不变仅 mtime 变化 → 不触发 onChange。
func TestConfigWatcher_TouchSameContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"v":1}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w := NewConfigWatcher(ConfigWatcherOptions{
		Path:     path,
		Interval: 50 * time.Millisecond,
		OnChange: func([]byte) error { calls.Add(1); return nil },
	})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	// 重写相同内容（mtime 推进但哈希不变）
	time.Sleep(120 * time.Millisecond)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("内容未变时 onChange 应只调用 1 次（启动时）, got %d", got)
	}
}

// TestConfigWatcher_OnChangeErrorKeepsState onChange 失败 → lastHash 不更新，
// 修正内容后仍能被后续 poll 拾取（自愈）。
func TestConfigWatcher_OnChangeErrorKeepsState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var okCalls, failCalls atomic.Int32
	w := NewConfigWatcher(ConfigWatcherOptions{
		Path:     path,
		Interval: 50 * time.Millisecond,
		OnChange: func(data []byte) error {
			if strings.Contains(string(data), "bad") {
				failCalls.Add(1)
				return os.ErrInvalid
			}
			okCalls.Add(1)
			return nil
		},
	})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	// 第一次变更内容触发 onChange 失败
	time.Sleep(120 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"v":"bad"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	// 第二次变更为合法内容 → 应被拾取（失败未污染 lastHash）
	if err := os.WriteFile(path, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	if failCalls.Load() == 0 {
		t.Error("应至少记录 1 次 onChange 失败")
	}
	if okCalls.Load() < 2 {
		t.Errorf("失败后应能恢复拾取, okCalls = %d", okCalls.Load())
	}
}

// TestConfigWatcher_CheckReadFails 运行中文件被替换为目录 → check() 读失败安全返回。
func TestConfigWatcher_CheckReadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w := NewConfigWatcher(ConfigWatcherOptions{
		Path:     path,
		Interval: 50 * time.Millisecond,
		OnChange: func([]byte) error { calls.Add(1); return nil },
	})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	// 文件替换为目录：Stat 成功（mtime 新）、ReadFile 失败
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("读失败不应触发 onChange, got %d", got)
	}
}

// TestLoadConfigFromFile_Errors 不存在文件与坏 JSON 两条错误路径。
func TestLoadConfigFromFile_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var cfg struct {
		V int `json:"v"`
	}
	if err := LoadConfigFromFile(filepath.Join(dir, "nonexist.json"), &cfg); err == nil {
		t.Error("不存在的文件应报错")
	}
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadConfigFromFile(badPath, &cfg); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

// TestWatchConfigFile_LoadError 初始加载失败 → 返回错误（不启动监视）。
func TestWatchConfigFile_LoadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := WatchConfigFile(filepath.Join(dir, "nonexist.json"), &struct{}{}, 50*time.Millisecond); err == nil {
		t.Error("初始加载失败应返回错误")
	}
}

// TestWatchConfigFile_OnChangeError 热加载内容非法 → onChange 失败但目标保持旧值。
func TestWatchConfigFile_OnChangeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"value":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	type TestConfig struct {
		mu    sync.RWMutex
		Value int `json:"value"`
	}
	var cfg TestConfig
	w, err := WatchConfigFile(path, &cfg, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// 写入非法 JSON → onChange 的 Unmarshal 失败 → 目标保持 1
	time.Sleep(120 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 再写入合法内容 → 恢复加载
	time.Sleep(150 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`{"value":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := w.Stop(); err != nil {
		t.Fatal(err)
	}
	cfg.mu.RLock()
	got := cfg.Value
	cfg.mu.RUnlock()
	if got != 7 {
		t.Errorf("非法内容被跳过后应恢复到 7, got %d", got)
	}
}

// ===== feature.go =====

// TestFeatureFlag_LoadFromFile_Boundaries 特性开关文件加载边界。
func TestFeatureFlag_LoadFromFile_Boundaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// 不存在文件 → 错误
	ff := NewFeatureFlag()
	if err := ff.LoadFromFile(filepath.Join(dir, "nonexist.json")); err == nil {
		t.Error("不存在的文件应报错")
	}

	// 坏 JSON → 错误
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ff.LoadFromFile(badPath); err == nil {
		t.Error("坏 JSON 应报错")
	}

	// 合法 JSON：合并语义（保留已有项、覆盖同名项、新增文件项）
	ff.Enable("pre-existing")
	ff.Disable("override-me")
	okPath := filepath.Join(dir, "flags.json")
	if err := os.WriteFile(okPath, []byte(`{"new-a":true,"new-b":false,"override-me":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ff.LoadFromFile(okPath); err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}
	list := ff.List()
	if !list["pre-existing"] {
		t.Error("已有开关应保留")
	}
	if !list["new-a"] || list["new-b"] {
		t.Errorf("文件项应生效: %v", list)
	}
	if !list["override-me"] {
		t.Error("同名项应被文件覆盖为 true")
	}
	if !ff.IsEnabled("new-a") {
		t.Error("IsEnabled 应读到新加载项")
	}
}
