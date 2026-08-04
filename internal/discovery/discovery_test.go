package discovery

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func testService() Service {
	return Service{
		Instance:    "build-box",
		Hostname:    "build-box",
		Port:        9443,
		Fingerprint: "AA:BB:CC:DD",
	}
}

func testAddrs() []netip.Addr {
	return []netip.Addr{netip.MustParseAddr("198.51.100.7")}
}

func parseOne(t *testing.T, payload []byte) Instance {
	t.Helper()
	found := map[string]*Instance{}
	if err := parseResponse(payload, found); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("期望 1 个实例，实际 %d", len(found))
	}
	for _, instance := range found {
		return *instance
	}
	return Instance{}
}

func TestAnnouncementRoundTrip(t *testing.T) {
	payload, err := buildAnnouncement(testService(), testAddrs(), defaultTTL)
	if err != nil {
		t.Fatalf("构造公告: %v", err)
	}
	instance := parseOne(t, payload)
	if instance.Name != "build-box" {
		t.Fatalf("实例名 = %q", instance.Name)
	}
	if instance.Port != 9443 {
		t.Fatalf("端口 = %d", instance.Port)
	}
	if got := instance.Addrs; len(got) != 1 || got[0].String() != "198.51.100.7" {
		t.Fatalf("地址 = %v", got)
	}
	// 指纹在公告里统一小写去冒号，方便直接比对。
	if instance.Fingerprint != "aabbccdd" {
		t.Fatalf("指纹 = %q", instance.Fingerprint)
	}
	if urls := instance.URLs(); len(urls) != 1 || urls[0] != "https://198.51.100.7:9443" {
		t.Fatalf("URL = %v", urls)
	}
}

func TestAnswerForQuery(t *testing.T) {
	query, err := buildQuery()
	if err != nil {
		t.Fatalf("构造查询: %v", err)
	}
	payload, unicast, err := answerFor(query, testService(), testAddrs())
	if err != nil {
		t.Fatalf("应答: %v", err)
	}
	if payload == nil {
		t.Fatal("PTR 查询应当被应答")
	}
	if !unicast {
		t.Fatal("查询带单播位，应答应走单播")
	}
	if instance := parseOne(t, payload); instance.Name != "build-box" {
		t.Fatalf("应答里的实例名 = %q", instance.Name)
	}
}

func TestAnswerForIgnoresOtherServices(t *testing.T) {
	// 别的服务类型的查询不应触发我们回应，避免在局域网里乱发包。
	other := Service{Instance: "other", Hostname: "other", Port: 1, Fingerprint: "ff"}
	announcement, err := buildAnnouncement(other, testAddrs(), defaultTTL)
	if err != nil {
		t.Fatalf("构造公告: %v", err)
	}
	payload, _, err := answerFor(announcement, testService(), testAddrs())
	if err != nil {
		t.Fatalf("应答: %v", err)
	}
	if payload != nil {
		t.Fatal("响应包不是查询，不应触发应答")
	}
}

func TestGoodbyeDropsInstance(t *testing.T) {
	payload, err := buildAnnouncement(testService(), testAddrs(), 0)
	if err != nil {
		t.Fatalf("构造告别: %v", err)
	}
	instance := parseOne(t, payload)
	if instance.Port != 0 || len(instance.Addrs) != 0 {
		t.Fatalf("TTL=0 的告别不应产生可用地址: %+v", instance)
	}
}

func TestInstanceNameWithDots(t *testing.T) {
	service := testService()
	service.Instance = "lab.build.01"
	payload, err := buildAnnouncement(service, testAddrs(), defaultTTL)
	if err != nil {
		t.Fatalf("构造公告: %v", err)
	}
	if instance := parseOne(t, payload); instance.Name != "lab.build.01" {
		t.Fatalf("带点的实例名被拆散: %q", instance.Name)
	}
}

func TestMatchesFingerprint(t *testing.T) {
	instance := Instance{Fingerprint: "aabbcc"}
	if !instance.MatchesFingerprint("AA:BB:CC") {
		t.Fatal("大小写和冒号不应影响匹配")
	}
	if instance.MatchesFingerprint("ddeeff") {
		t.Fatal("不同指纹不应匹配")
	}
	if !(Instance{}).MatchesFingerprint("aabbcc") {
		t.Fatal("公告没带指纹时应放行，交由 TLS pin 兜底")
	}
}

func TestBrowseRespectsTimeout(t *testing.T) {
	start := time.Now()
	instances, err := Browse(context.Background(), 300*time.Millisecond)
	if err != nil {
		t.Skipf("本机不允许多播: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 3*time.Second {
		t.Fatalf("Browse 超时未生效，耗时 %s", elapsed)
	}
	for _, instance := range instances {
		if instance.Port == 0 || len(instance.Addrs) == 0 {
			t.Fatalf("返回了不完整的实例: %+v", instance)
		}
	}
}

func TestBrowseHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := Browse(ctx, 5*time.Second); err != nil {
		t.Skipf("本机不允许多播: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("已取消的 ctx 应立即返回，耗时 %s", elapsed)
	}
}
