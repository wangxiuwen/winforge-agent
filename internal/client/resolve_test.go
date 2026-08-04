package client

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/discovery"
)

func withStubs(t *testing.T, instances []discovery.Instance,
	verify func(context.Context, Profile) error) {
	t.Helper()
	originalBrowse, originalVerify := browseInstances, verifyCandidate
	browseInstances = func(context.Context, time.Duration) ([]discovery.Instance, error) {
		return instances, nil
	}
	verifyCandidate = verify
	t.Cleanup(func() {
		browseInstances, verifyCandidate = originalBrowse, originalVerify
	})
}

func instance(name, fingerprint string, addrs ...string) discovery.Instance {
	parsed := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		parsed = append(parsed, netip.MustParseAddr(addr))
	}
	return discovery.Instance{Name: name, Port: 9443, Addrs: parsed, Fingerprint: fingerprint}
}

func TestRelocatePicksVerifiedCandidate(t *testing.T) {
	var attempted []string
	withStubs(t,
		[]discovery.Instance{instance("build-box", "aabb", "198.51.100.7", "203.0.113.9")},
		func(_ context.Context, profile Profile) error {
			attempted = append(attempted, profile.Host)
			if profile.Host == "https://203.0.113.9:9443" {
				return nil
			}
			return errors.New("连接失败")
		})

	profile := Profile{Token: "t", Fingerprint: "AA:BB", Instance: "build-box",
		Host: "https://198.51.100.1:9443"}
	got, err := Relocate(context.Background(), profile, time.Second)
	if err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	if got.Host != "https://203.0.113.9:9443" {
		t.Fatalf("Host = %q", got.Host)
	}
	if len(attempted) != 2 {
		t.Fatalf("应逐个尝试候选地址，实际 %v", attempted)
	}
	if got.Token != "t" || got.Instance != "build-box" {
		t.Fatalf("其余字段不应被改动: %+v", got)
	}
}

func TestRelocateSkipsFingerprintMismatch(t *testing.T) {
	// 指纹对不上的实例连试都不能试——试一次就等于把 token 递过去。
	withStubs(t,
		[]discovery.Instance{instance("build-box", "ffff", "198.51.100.7")},
		func(_ context.Context, profile Profile) error {
			t.Fatalf("不应向指纹不匹配的实例发起请求: %q", profile.Host)
			return nil
		})

	profile := Profile{Token: "t", Fingerprint: "aabb", Instance: "build-box"}
	if _, err := Relocate(context.Background(), profile, time.Second); err == nil {
		t.Fatal("期望失败")
	}
}

func TestRelocateSkipsOtherInstances(t *testing.T) {
	withStubs(t,
		[]discovery.Instance{instance("other-box", "aabb", "198.51.100.7")},
		func(_ context.Context, profile Profile) error {
			t.Fatalf("不应连接别的实例: %q", profile.Host)
			return nil
		})

	profile := Profile{Token: "t", Fingerprint: "aabb", Instance: "build-box"}
	if _, err := Relocate(context.Background(), profile, time.Second); err == nil {
		t.Fatal("期望失败")
	}
}

func TestRelocateRequiresInstanceName(t *testing.T) {
	profile := Profile{Token: "t", Fingerprint: "aabb", Host: "https://198.51.100.7:9443"}
	if _, err := Relocate(context.Background(), profile, time.Second); err == nil {
		t.Fatal("没有实例名时不应尝试发现")
	}
}

func TestOpenSavesRelocatedHost(t *testing.T) {
	t.Setenv("WINFORGE_HOME", t.TempDir())

	// New() 会校验指纹长度，这里用一个合法的 32 字节十六进制指纹。
	fingerprint := strings.Repeat("ab", 32)
	original := Profile{
		// 环回口的 1 端口会被立刻拒绝，且不会被本机代理/TUN 接管，稳定触发重新解析。
		Host:        "https://127.0.0.1:1",
		Token:       "t",
		Fingerprint: fingerprint,
		Instance:    "build-box",
	}
	if err := SaveProfile("box", original); err != nil {
		t.Fatalf("保存 profile: %v", err)
	}
	withStubs(t,
		[]discovery.Instance{instance("build-box", fingerprint, "198.51.100.7")},
		func(context.Context, Profile) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Open(ctx, "box"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	saved, err := LoadProfile("box")
	if err != nil {
		t.Fatalf("读取 profile: %v", err)
	}
	if saved.Host != "https://198.51.100.7:9443" {
		t.Fatalf("新地址没有写回 profile: %q", saved.Host)
	}
}
