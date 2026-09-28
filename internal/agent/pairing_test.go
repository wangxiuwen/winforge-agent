package agent

import (
	"testing"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/security"
)

func armed(t *testing.T) (*PendingPair, string, time.Time) {
	t.Helper()
	p := &PendingPair{}
	now := time.Now()
	code, _, err := p.Arm(now)
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if len(code) != security.PairCodeDigits {
		t.Fatalf("配对码是 %q，长度 %d，人要念的就得是固定 6 位", code, len(code))
	}
	return p, code, now
}

// 一次性：同一个码被用第二次，就等于一张能反复刷的门禁卡。
func TestPairCodeIsSingleUse(t *testing.T) {
	p, code, now := armed(t)
	if err := p.Redeem(code, now); err != nil {
		t.Fatalf("第一次兑换应当成功: %v", err)
	}
	if err := p.Redeem(code, now); err == nil {
		t.Fatal("同一个配对码被用了第二次")
	}
}

// 过期：配对码常常被写在便签上、留在聊天记录里，不过期就是长期口令。
func TestPairCodeExpires(t *testing.T) {
	p, code, now := armed(t)
	if err := p.Redeem(code, now.Add(PairCodeTTL+time.Second)); err == nil {
		t.Fatal("过期的配对码仍然可用")
	}
}

// 锁定：6 位数字只有一百万种，不限制次数就能在局域网里慢慢爆破。
func TestPairCodeLocksOutAfterAttempts(t *testing.T) {
	p, code, now := armed(t)
	for i := 0; i < PairMaxAttempts; i++ {
		if err := p.Redeem("000000", now); err == nil {
			t.Fatal("错误的配对码竟然通过了")
		}
	}
	if err := p.Redeem(code, now); err == nil {
		t.Fatalf("连错 %d 次之后，正确的配对码仍然可用——爆破不受限", PairMaxAttempts)
	}
}

// proof 把证书指纹绑在配对码上：没有码就伪造不出 proof，指纹因此不用人抄。
func TestPairProofBindsFingerprintToCode(t *testing.T) {
	const fp = "aabbccdd"
	proof := security.PairProof("123456", fp)
	if !security.VerifyPairProof("123456", fp, proof) {
		t.Fatal("自己算的 proof 自己验不过")
	}
	if security.VerifyPairProof("654321", fp, proof) {
		t.Fatal("换一个配对码也能验过 —— proof 没绑住配对码")
	}
	if security.VerifyPairProof("123456", "deadbeef", proof) {
		t.Fatal("换一张证书也能验过 —— 中间人可以直接冒充")
	}
}
