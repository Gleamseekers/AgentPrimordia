package main

import (
	"strings"
	"testing"
)

// TestQuickstartTemplateWiresTelemetry 回归：ap start 生成的 quickstart 项目
// 必须接入 OTel 遥测（TelemetryFromEnv + WithTelemetry），
// 否则 pkg/otel.go 的 Experimental 子集又会退化为"无生产构造点"。
func TestQuickstartTemplateWiresTelemetry(t *testing.T) {
	files, err := Generate(GenerateOptions{Name: "otel-demo", Template: "quickstart"})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	main, ok := files["main.go"]
	if !ok {
		t.Fatal("quickstart 模板缺 main.go")
	}
	content := string(main)
	for _, want := range []string{"TelemetryFromEnv", "WithTelemetry", "tp.Shutdown()"} {
		if !strings.Contains(content, want) {
			t.Errorf("quickstart main.go 缺少遥测接线片段: %q", want)
		}
	}
}
