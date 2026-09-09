// trust_fixture_test.go — 签名信任链跨语言对账夹具（Go 为权威生成方）
//
// 产出 testdata/trust_fixture.json：{payload_sha256, signature_der_b64,
// pubkey_uncompressed_b64}——TS 消费端（sdk/typescript/src/tools/lifecycle.ts）
// 经 WebCrypto（ECDSA P-256 / SHA-256）验证同一签名（矩阵 #3 协议对等）。
// 再生方式：AP_WRITE_TRUST_FIXTURE=1 go test ./internal/tools/lifecycle/ -run TestWriteTrustFixture
package lifecycle

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type trustFixture struct {
	Payload            string `json:"payload"`              // 被签原文（UTF-8）
	PayloadSHA256      string `json:"payload_sha256"`       // 摘要十六进制
	SignatureDERB64    string `json:"signature_der_b64"`    // ECDSA ASN.1 DER 签名
	PubUncompressedB64 string `json:"pub_uncompressed_b64"` // P-256 非压缩公钥（0x04||X||Y）
	Note               string `json:"note"`                 // 口径说明
}

// TestWriteTrustFixture 生成夹具（默认跳过）。
func TestWriteTrustFixture(t *testing.T) {
	if os.Getenv("AP_WRITE_TRUST_FIXTURE") == "" {
		t.Skip("设置 AP_WRITE_TRUST_FIXTURE=1 以重新生成夹具")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("agentprimordia/lifecycle-trust-fixture/v1")
	digest := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	fx := trustFixture{
		Payload:            string(payload),
		PayloadSHA256:      hex.EncodeToString(digest[:]),
		SignatureDERB64:    base64.StdEncoding.EncodeToString(sig),
		PubUncompressedB64: base64.StdEncoding.EncodeToString(func() []byte {
			// 非压缩公钥：0x04 || X (32 bytes) || Y (32 bytes)，与 elliptic.Marshal 口径一致
			buf := make([]byte, 65)
			buf[0] = 0x04
			key.X.FillBytes(buf[1:33])
			key.Y.FillBytes(buf[33:65])
			return buf
		}()),
		Note:               "cosign 同款口径：SHA-256 摘要 + ECDSA P-256（ASN.1 DER 签名）；TS 侧 WebCrypto 需 DER→raw 转换后导入非压缩公钥",
	}
	data, err := json.MarshalIndent(fx, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "trust_fixture.json"), append(data, byte(10)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("信任链夹具已写出")
}
