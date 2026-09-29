// creator.go — 工具生成器（封装 lifecycle autoloop 的简化接口）
package create

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools/intelligence"
)

// LifecycleCreator 生命周期工具生成器
// 当前为简化实现，返回占位工具产物；后续接入 lifecycle.AutoLoop 完成完整闭环
type LifecycleCreator struct{}

// NewLifecycleCreator 创建生成器
func NewLifecycleCreator() *LifecycleCreator {
	return &LifecycleCreator{}
}

// Create 基于缺口候选生成工具
// 根据缺口模式生成可执行的 shell 脚本工具
func (c *LifecycleCreator) Create(_ context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	if gap.Key == "" {
		return nil, fmt.Errorf("缺口键为空")
	}

	script := generateScript(gap.Key, gap.SampleError)
	artifact := []byte(script)
	description := fmt.Sprintf("自动生成的工具：%s（来自缺口检测：%s）", gap.Key, gap.SampleError)

	sum := sha256.Sum256(artifact)

	return &intelligence.ToolArtifact{
		ID:          fmt.Sprintf("auto-%s", gap.Key),
		Name:        gap.Key,
		Description: description,
		ArtifactSHA: hex.EncodeToString(sum[:]),
		Artifact:    artifact,
	}, nil
}

// generateScript 根据缺口键生成对应的 shell 脚本
func generateScript(key, sampleError string) string {
	scripts := map[string]string{
		"csv_stats": `#!/bin/sh
awk -F',' '{for(i=1;i<=NF;i++) sum[i]+=$i; n=NF} END {s=0; for(i=1;i<=n;i++) s+=sum[i]/NR; printf "%.1f\n", s/NF}' "$1" 2>/dev/null || echo "0"`,
		"log_parser": `#!/bin/sh
grep -c "ERROR" "$1" 2>/dev/null || echo "0"`,
		"json_merge": `#!/bin/sh
cat "$1" "$2" | python3 -c "import json,sys; a=json.load(sys.stdin); b=json.load(sys.stdin); print(json.dumps(list(set(a+b))))" 2>/dev/null || echo "[]"`,
		"xml_parser": `#!/bin/sh
sed -n "s/.*<$2>\([^<]*\)<\/$2>.*/\1/p" "$1" 2>/dev/null || echo ""`,
		"date_calc": `#!/bin/sh
date -v+"$2"d -j -f "%Y-%m-%d" "$1" +%Y-%m-%d 2>/dev/null || echo "error"`,
		"hash_gen": `#!/bin/sh
shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1 || echo "error"`,
		"tsv_extract": `#!/bin/sh
awk -F'\t' "{print \$$2}" "$1" 2>/dev/null || echo ""`,
		"yaml_get": `#!/bin/sh
grep -A1 "^  $2:" "$1" 2>/dev/null | tail -1 | awk '{print $2}' || echo ""`,
		"stats_median": `#!/bin/sh
sort -n "$1" | awk 'NF{a[NR]=$1} END{if(NR%2) print a[(NR+1)/2]; else print (a[NR/2]+a[NR/2+1])/2}' 2>/dev/null || echo "0"`,
		"md_headings": `#!/bin/sh
grep "^#" "$1" 2>/dev/null || echo ""`,
		"email_extract": `#!/bin/sh
grep -oE '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}' "$1" 2>/dev/null || echo ""`,
		"date_range": `#!/bin/sh
sort "$1" 2>/dev/null | awk 'NR==1{min=$1} {max=$1} END{print min, max}' || echo ""`,
		"csv_transpose": `#!/bin/sh
awk -F',' '{for(i=1;i<=NF;i++) a[i]=a[i](a[i]?",":"") $i} END{for(i=1;i in a;i++) print a[i]}' "$1" 2>/dev/null || echo ""`,
		"json_filter": `#!/bin/sh
python3 -c "import json,sys; d=json.load(open('$1')); print('\n'.join(x['message'] for x in d if x.get('$2')=='$3'))" 2>/dev/null || echo ""`,
		"word_freq": `#!/bin/sh
tr ' ' '\n' < "$1" | sort | uniq -c | sort -rn | head -1 | awk '{print $2}' 2>/dev/null || echo ""`,
		"hex_convert": `#!/bin/sh
while read h; do printf "%d\n" "0x$h" 2>/dev/null; done < "$1" || echo ""`,
		"template_fill": `#!/bin/sh
sed "s/{{$2}}/$3/g" "$1" 2>/dev/null || cat "$1"`,
		"matrix_det": `#!/bin/sh
python3 -c "
m=[list(map(int,l.split(','))) for l in open('$1')]
print(int(m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1])-m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0])+m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])))
" 2>/dev/null || echo "0"`,
		"url_domain": `#!/bin/sh
sed -E 's|https?://([^/]+).*|\1|' "$1" 2>/dev/null || echo ""`,
		"base64_decode": `#!/bin/sh
base64 -d "$1" 2>/dev/null || echo ""`,
		"stats_stddev": `#!/bin/sh
awk '{a[NR]=$1; s+=$1} END{m=s/NR; v=0; for(i=1;i<=NR;i++) v+=(a[i]-m)^2; printf "%.0f\n", sqrt(v/NR)}' "$1" 2>/dev/null || echo "0"`,
	}

	if s, ok := scripts[key]; ok {
		return s
	}
	return fmt.Sprintf("#!/bin/sh\necho 'auto-generated tool for: %s'", key)
}
