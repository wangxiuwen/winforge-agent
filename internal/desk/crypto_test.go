package desk

import (
	"bytes"
	"testing"
)

func TestHKDFVectors(t *testing.T) {
	// RFC 5869 A.1 测试向量（SHA-256）。
	got, err := hkdfExtractExpand(
		[]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
		[]byte{0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b,
			0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b},
		[]byte{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9}, 42)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x3c, 0xb2, 0x5f, 0x25, 0xfa, 0xac, 0xd5, 0x7a, 0x90, 0x43, 0x4f, 0x64, 0xd0, 0x36, 0x2f, 0x2a,
		0x2d, 0x2d, 0x0a, 0x90, 0xcf, 0x1a, 0x5a, 0x4c, 0x5d, 0xb0, 0x2d, 0x56, 0xec, 0xc4, 0xc5, 0xbf,
		0x34, 0x00, 0x72, 0x08, 0xd5, 0xb8, 0x87, 0x18, 0x58, 0x65}
	if !bytes.Equal(got, want) {
		t.Fatalf("HKDF 输出不匹配: %x", got)
	}
}

func TestPairingSecretAndCodeSymmetry(t *testing.T) {
	ka, _ := newEphKeyPair()
	kb, _ := newEphKeyPair()
	nonceA, nonceB := randomBytes(16), randomBytes(16)
	dhA, err := ka.dh(kb.pub)
	if err != nil {
		t.Fatal(err)
	}
	dhB, err := kb.dh(ka.pub)
	if err != nil {
		t.Fatal(err)
	}
	secA, err := pairingSecret(dhA, nonceA, nonceB)
	if err != nil {
		t.Fatal(err)
	}
	secB, err := pairingSecret(dhB, nonceA, nonceB)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secA, secB) {
		t.Fatal("双方配对密钥不一致")
	}
	if pairCode(secA) != pairCode(secB) {
		t.Fatal("双方配对码不一致")
	}
	if len(pairCode(secA)) != 6 {
		t.Fatalf("配对码长度 %d", len(pairCode(secA)))
	}
}

// TestPairingCodeDetectsMITM 模拟中间人：两段独立 DH 会派生出不同配对码。
func TestPairingCodeDetectsMITM(t *testing.T) {
	ka, _ := newEphKeyPair()
	kb, _ := newEphKeyPair()
	km, _ := newEphKeyPair() // 中间人
	nonceA, nonceB := randomBytes(16), randomBytes(16)
	secA, err := pairingSecret(mustDH(t, ka, km.pub), nonceA, nonceB)
	if err != nil {
		t.Fatal(err)
	}
	secB, err := pairingSecret(mustDH(t, kb, km.pub), nonceA, nonceB)
	if err != nil {
		t.Fatal(err)
	}
	if pairCode(secA) == pairCode(secB) {
		t.Fatal("中间人场景配对码不应一致")
	}
}

func TestPairProofVerify(t *testing.T) {
	secret := randomBytes(32)
	if err := verifyPairProof(secret, "A", pairProof(secret, "A")); err != nil {
		t.Fatal("合法证明被拒")
	}
	if err := verifyPairProof(secret, "B", pairProof(secret, "A")); err == nil {
		t.Fatal("错侧证明被接受")
	}
	if err := verifyPairProof(secret, "A", randomBytes(32)); err == nil {
		t.Fatal("伪造证明被接受")
	}
}

func TestSessionKeysRoundTripAndReplay(t *testing.T) {
	psk := randomBytes(32)
	dh := randomBytes(32)
	nonceA, nonceB := randomBytes(16), randomBytes(16)
	kA, err := deriveSessionKeys(psk, dh, nonceA, nonceB, true)
	if err != nil {
		t.Fatal(err)
	}
	kB, err := deriveSessionKeys(psk, dh, nonceA, nonceB, false)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello 密封帧")
	got, err := kB.open(kA.seal(msg))
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatal("A→B 解封失败")
	}
	got, err = kA.open(kB.seal(msg))
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatal("B→A 解封失败")
	}
	// 重放旧帧必须被拒。
	frame := kA.seal(msg)
	if _, err := kB.open(frame); err != nil {
		t.Fatalf("首帧应可解: %v", err)
	}
	if _, err := kB.open(frame); err == nil {
		t.Fatal("重放帧被接受")
	}
	// 篡改必须被拒。
	bad := kA.seal(msg)
	bad[len(bad)-1] ^= 0xff
	if _, err := kB.open(bad); err == nil {
		t.Fatal("篡改帧被接受")
	}
}

func TestHelloContextSymmetry(t *testing.T) {
	a, na, ea := []byte("AAA"), randomBytes(16), randomBytes(32)
	b, nb, eb := []byte("BBB"), randomBytes(16), randomBytes(32)
	if !bytes.Equal(helloContext(a, na, ea, b, nb, eb), helloContext(a, na, ea, b, nb, eb)) {
		t.Fatal("上下文不稳定")
	}
	if bytes.Equal(helloContext(a, na, ea, b, nb, eb), helloContext(b, nb, eb, a, na, ea)) {
		t.Fatal("方向不同上下文应不同")
	}
}

func mustDH(t *testing.T, k *ephKeyPair, peer []byte) []byte {
	t.Helper()
	s, err := k.dh(peer)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
