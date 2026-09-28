package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
)

// 配对码是给人念的那一段：6 位数字，人能在电话里读、能手打，不会抄错。
//
// 它本身不是长期凭证——长期 token 仍然是 32 字节随机数，只是不再需要人搬运。
// 6 位数字只有一百万种，所以它必须同时满足三件事，缺一不可（都由 PendingPair 保证）：
// 短时效、一次性、错几次就作废。
//
// 另一半价值在 Proof：以前指纹要人肉比对 64 个十六进制字符，实际上没人真比，
// 于是"防中间人"形同虚设。现在服务端用配对码作为密钥、对自己的证书指纹做 HMAC，
// 客户端拿它**实际握手时看到的**指纹算一遍对比——中间人没有这 6 位数字就伪造不出来，
// 指纹也就不用人搬了。人少记一样东西，安全性反而是升的。
const PairCodeDigits = 6

// NewPairCode 生成一个 6 位数字配对码（含前导零，始终 6 位）。
func NewPairCode() (string, error) {
	max := big.NewInt(1)
	for i := 0; i < PairCodeDigits; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", PairCodeDigits, n), nil
}

// PairProof = HMAC-SHA256(配对码, 证书指纹)。服务端返回它，客户端用自己看到的指纹验证。
func PairProof(code, fingerprint string) string {
	mac := hmac.New(sha256.New, []byte(code))
	mac.Write([]byte(fingerprint))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyPairProof 恒定时间比较，避免按字节泄漏。
func VerifyPairProof(code, fingerprint, proof string) bool {
	want := PairProof(code, fingerprint)
	return subtle.ConstantTimeCompare([]byte(want), []byte(proof)) == 1
}
