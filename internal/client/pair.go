package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/security"
)

// PairWithCode 用 6 位配对码换回长期 token，并顺带解决"指纹谁来验"的问题。
//
// 配对这一刻我们还不知道对方的证书指纹（不知道才要配对），所以 TLS 这一步只能先接受
// 对方出示的证书、把它记下来。真正的验证在后面：服务端返回 HMAC(配对码, 它自己的指纹)，
// 我们用**刚刚握手时看到的**指纹算一遍。对得上，说明出示这张证书的人确实知道配对码，
// 中间人插不进来；对不上就整个作废，不留 profile。
//
// 这比原来的做法更安全，也更省事：原来要人把 64 个十六进制字符抄到另一台机器上，
// 抄错了会失败，不抄照样"能用"——于是大多数人根本不比对。
func PairWithCode(ctx context.Context, host, code string) (Profile, error) {
	var seen []byte
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 见上：此刻还没有可信指纹，验证靠下面的 proof
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return fmt.Errorf("对端没有出示证书")
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				seen = sum[:]
				return nil
			},
		},
	}
	client := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/v1/pair", bytes.NewReader(body))
	if err != nil {
		return Profile{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Profile{}, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("配对失败(%d): %s", resp.StatusCode, bytes.TrimSpace(payload))
	}
	var out struct {
		Token string `json:"token"`
		Proof string `json:"proof"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return Profile{}, fmt.Errorf("配对响应无法解析: %w", err)
	}
	if out.Token == "" {
		return Profile{}, fmt.Errorf("配对响应里没有 token")
	}
	fingerprint := hex.EncodeToString(seen)
	if !security.VerifyPairProof(code, fingerprint, out.Proof) {
		// 对方能给出 token，却证明不了自己知道配对码 —— 典型的中间人。
		return Profile{}, fmt.Errorf("证书校验失败：对方无法证明它知道这个配对码，已中止（可能有中间人）")
	}
	return Profile{Host: host, Token: out.Token, Fingerprint: fingerprint}, nil
}

// ArmPairCode 让正在运行的 Agent 生成一个配对码。走已鉴权通道：能读配置文件的人本来
// 就持有 token，不需要再发明一套权限。
func (c *Client) ArmPairCode(ctx context.Context) (string, time.Time, error) {
	resp, err := c.request(ctx, http.MethodPost, "/v1/pair-code", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var out struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", time.Time{}, err
	}
	return out.Code, out.ExpiresAt, nil
}
