package client

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/discovery"
)

// 这两个变量只是为了让测试能替换掉真实网络行为。
var (
	browseInstances = discovery.Browse
	verifyCandidate = func(ctx context.Context, profile Profile) error {
		c, err := New(profile)
		if err != nil {
			return err
		}
		return c.Status(ctx, discardWriter{})
	}
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Open 载入 profile 并返回可用的客户端。
// 如果 profile 记了 mDNS 实例名，而记录的 Host 连不上（典型场景是构建机重启后 DHCP 换了地址），
// 就在局域网里重新找一次，验证通过后把新地址写回 profile。
func Open(ctx context.Context, name string) (*Client, error) {
	profile, err := LoadProfile(name)
	if err != nil {
		return nil, err
	}
	if profile.Instance == "" || reachable(profile.Host, 1500*time.Millisecond) {
		return New(profile)
	}
	relocated, err := Relocate(ctx, profile, 4*time.Second)
	if err != nil {
		// 重新解析失败时仍按原地址返回，让真正的请求给出更具体的报错。
		return New(profile)
	}
	if relocated.Host != profile.Host {
		if err := SaveProfile(name, relocated); err != nil {
			return nil, err
		}
	}
	return New(relocated)
}

// Relocate 通过 mDNS 找回 Agent 的当前地址。
//
// 发现结果本身不可信：候选地址必须先通过固定的证书指纹校验、再成功调用一次 status，
// 才会被采纳。指纹不匹配的实例直接跳过，绝不会把 token 发给它。
func Relocate(ctx context.Context, profile Profile, timeout time.Duration) (Profile, error) {
	if profile.Instance == "" {
		return profile, fmt.Errorf("profile 没有记录 mDNS 实例名")
	}
	instances, err := browseInstances(ctx, timeout)
	if err != nil {
		return profile, err
	}
	for _, instance := range instances {
		if instance.Name != profile.Instance || !instance.MatchesFingerprint(profile.Fingerprint) {
			continue
		}
		for _, candidateHost := range instance.URLs() {
			candidate := profile
			candidate.Host = candidateHost
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := verifyCandidate(probeCtx, candidate)
			cancel()
			if err == nil {
				return candidate, nil
			}
		}
	}
	return profile, fmt.Errorf("局域网内没有找到可验证的实例 %q", profile.Instance)
}

func reachable(host string, timeout time.Duration) bool {
	address, err := hostPort(host)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func hostPort(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("无效地址: %q", rawURL)
	}
	if parsed.Port() != "" {
		return parsed.Host, nil
	}
	return net.JoinHostPort(parsed.Hostname(), "443"), nil
}
