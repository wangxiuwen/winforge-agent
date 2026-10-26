package desk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Name = "测试机"
	peer := Peer{ID: "peer-1", Name: "对端", PSK: "abc", Enabled: true, Position: "right"}
	if err := cfg.UpPeer(path, peer); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Name != "测试机" || len(reloaded.Peers) != 1 || reloaded.Peers[0].Position != "right" {
		t.Fatalf("回读不一致: %+v", reloaded)
	}
}

func TestSetPeerPositionExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.json")
	cfg, _ := LoadConfig(path)
	_ = cfg.UpPeer(path, Peer{ID: "a", Name: "A", PSK: "x", Enabled: true})
	_ = cfg.UpPeer(path, Peer{ID: "b", Name: "B", PSK: "y", Enabled: true})
	if err := cfg.SetPeerPosition(path, "a", "right"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetPeerPosition(path, "b", "right"); err != nil {
		t.Fatal(err)
	}
	pa, _ := cfg.Peer("a")
	pb, _ := cfg.Peer("b")
	if pa.Position != "" || pb.Position != "right" {
		t.Fatalf("每侧应至多一台: a=%q b=%q", pa.Position, pb.Position)
	}
	if err := cfg.SetPeerPosition(path, "b", "up"); err == nil {
		t.Fatal("非法方位应报错")
	}
}

func TestConfigCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("坏配置应报错而不是崩溃")
	}
}
