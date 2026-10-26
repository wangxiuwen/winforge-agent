package desk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
)

// 密码学设计：
//   配对：双端各出临时 X25519 密钥做 ECDH，6 位配对码由共享密钥派生并同时在
//   两台机器屏幕显示，人工比对可发现中间人；确认后派生 PSK 落盘。
//   会话：每次连接再出临时 X25519，会话密钥 = HKDF(临时DH ‖ PSK)，双方用
//   HMAC 证明持有 PSK；之后所有帧 AES-256-GCM，方向独立 8 字节计数器 nonce。
// 输入事件只在共享激活期间转发且仅存于内存，不落盘。

var (
	errBadPubKey    = errors.New("非法 X25519 公钥")
	errBadProof     = errors.New("身份证明校验失败")
	errNonceReused  = errors.New("nonce 计数器异常回退")
	errCodeMismatch = errors.New("配对码不一致")
)

const (
	infoPairCode = "winforge-desk/pair-code/v1"
	infoPairPSK  = "winforge-desk/psk/v1"
	infoSession  = "winforge-desk/session/v1"
)

// transcript 用长度前缀拼接若干段，避免歧义。
func transcript(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		out = append(out, l[:]...)
		out = append(out, p...)
	}
	return out
}

// hkdfExtractExpand 是 RFC 5869 的最小实现，避免引入额外依赖。
func hkdfExtractExpand(salt, ikm, info []byte, length int) ([]byte, error) {
	extractor := hmac.New(sha256.New, salt)
	extractor.Write(ikm)
	prk := extractor.Sum(nil)
	if length <= 0 || length > 255*sha256.Size {
		return nil, fmt.Errorf("HKDF 输出长度非法: %d", length)
	}
	out := make([]byte, 0, length)
	var t []byte
	for i := byte(1); len(out) < length; i++ {
		mac := hmac.New(sha256.New, prk)
		mac.Write(t)
		mac.Write(info)
		mac.Write([]byte{i})
		t = mac.Sum(nil)
		out = append(out, t...)
	}
	return out[:length], nil
}

// ephKeyPair 是临时 X25519 密钥对。
type ephKeyPair struct {
	priv *ecdh.PrivateKey
	pub  []byte // 32 字节
}

func newEphKeyPair() (*ephKeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &ephKeyPair{priv: priv, pub: priv.PublicKey().Bytes()}, nil
}

// dh 计算与对端公钥的共享密钥。
func (k *ephKeyPair) dh(peerPub []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(peerPub)
	if err != nil {
		return nil, errBadPubKey
	}
	secret, err := k.priv.ECDH(pub)
	if err != nil {
		return nil, errBadPubKey
	}
	return secret, nil
}

// pairingSecret 从配对 ECDH 派生 32 字节配对密钥（双端输入对称，结果一致）。
func pairingSecret(dhSecret, nonceA, nonceB []byte) ([]byte, error) {
	return hkdfExtractExpand(nonceA, dhSecret, append([]byte("salt"), nonceB...), 32)
}

// pairCode 生成 6 位配对码。
func pairCode(secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(infoPairCode))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(mac.Sum(nil))%1000000)
}

// pairProof 计算配对确认证明，side 区分双方防反射攻击。
func pairProof(secret []byte, side string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(transcript([]byte(infoPairCode), []byte(side)))
	return mac.Sum(nil)
}

// verifyPairProof 校验对端证明。
func verifyPairProof(secret []byte, side string, proof []byte) error {
	want := pairProof(secret, side)
	if len(proof) != len(want) || subtle.ConstantTimeCompare(proof, want) != 1 {
		return errBadProof
	}
	return nil
}

// pairingPSK 从配对密钥派生长期共享密钥。
func pairingPSK(secret []byte) ([]byte, error) {
	return hkdfExtractExpand(nil, secret, []byte(infoPairPSK), 32)
}

// helloContext 组装会话握手上下文（双方视角对称，initiator 在前）。
func helloContext(idA, nonceA, epkA, idB, nonceB, epkB []byte) []byte {
	return transcript([]byte(infoSession), idA, nonceA, epkA, idB, nonceB, epkB)
}

// helloProof 计算会话握手证明。
func helloProof(psk, ctx []byte) []byte {
	mac := hmac.New(sha256.New, psk)
	mac.Write(ctx)
	return mac.Sum(nil)
}

// sessionKeys 持有两个方向的 AEAD 与计数器。
type sessionKeys struct {
	send    cipher.AEAD
	recv    cipher.AEAD
	sendSeq uint64
	recvSeq uint64
}

// deriveSessionKeys 从临时 DH 与 PSK 派生双向会话密钥。
// initiator=true 表示主动连接方：其发送密钥为 okm 前 32 字节。
func deriveSessionKeys(psk, dhSecret, nonceA, nonceB []byte, initiator bool) (*sessionKeys, error) {
	ikm := append(append([]byte{}, dhSecret...), psk...)
	okm, err := hkdfExtractExpand(append(append([]byte{}, nonceA...), nonceB...), ikm, []byte(infoSession), 64)
	if err != nil {
		return nil, err
	}
	sendKey, recvKey := okm[:32], okm[32:64]
	if !initiator {
		sendKey, recvKey = recvKey, sendKey
	}
	k := &sessionKeys{}
	if k.send, err = newGCM(sendKey); err != nil {
		return nil, err
	}
	if k.recv, err = newGCM(recvKey); err != nil {
		return nil, err
	}
	return k, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal 密封一帧：12B nonce（前 4B 为 0，后 8B 计数器）+ AEAD 密文。
func (k *sessionKeys) seal(plain []byte) []byte {
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], k.sendSeq)
	k.sendSeq++
	return k.send.Seal(nonce[:], nonce[:], plain, nil)
}

// open 解开一帧并校验计数器严格递增（首帧例外，任意起始计数均可）。
func (k *sessionKeys) open(frame []byte) ([]byte, error) {
	if len(frame) < 12+16 {
		return nil, errors.New("密文帧过短")
	}
	seq := binary.BigEndian.Uint64(frame[4:12])
	if k.recvSeq != 0 && seq <= k.recvSeq {
		return nil, errNonceReused
	}
	plain, err := k.recv.Open(nil, frame[:12], frame[12:], nil)
	if err != nil {
		return nil, err
	}
	k.recvSeq = seq
	return plain, nil
}

// randomBytes 便捷随机。
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// b64 便捷编码。
func b64(b []byte) string { return base64.RawStdEncoding.EncodeToString(b) }

// unb64 便捷解码，失败返回 nil。
func unb64(s string) []byte {
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
