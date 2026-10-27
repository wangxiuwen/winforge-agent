// desk 包实现 WinForge Desk：局域网多机键鼠共享 + 消息/文件互助。
//
// 独立于构建 Agent：`winforge desk` 是面向最终用户的托盘程序，
// 配对必须双端确认同一个配对码，键鼠转发只在共享激活期间进行，
// 不落盘、不记录任何输入内容。
package desk

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Config 是 desk.json 的内容，按用户存放（区别于 Agent 的 ProgramData 系统配置）。
// 并发模型：Peers/字段会被 hub 多个 goroutine 读写（补连、accept、UI 命令），
// 方法集内部互斥；直接字段访问仅限持有 hub.mu 的同步块。
type Config struct {
	mu sync.Mutex

	// ID 是本机稳定标识，首次运行生成，配对双方靠它认人。
	ID string `json:"id"`
	// Name 是显示名，默认取主机名。
	Name string `json:"name"`
	// UIPort 是控制台 HTTP 端口（只绑 127.0.0.1），0 表示自动挑空闲端口。
	UIPort int `json:"ui_port"`
	// ControlPort 是配对/会话 TCP 端口，0 表示自动挑空闲端口。
	ControlPort int `json:"control_port"`
	// DiscoveryPort 是 UDP 广播端口，两端必须一致才能互相发现。
	DiscoveryPort int `json:"discovery_port"`
	// Hotkey 是"把键鼠切给对端 / 拿回来"的全局热键，如 "Ctrl+Alt+D"。
	Hotkey string `json:"hotkey"`
	// DownloadsDir 是收到文件的保存目录。
	DownloadsDir string `json:"downloads_dir"`
	// MaxFileBytes 是单个传输文件的上限。
	MaxFileBytes int64 `json:"max_file_bytes"`
	// ShareEnabled 是总开关：关掉后不捕获也不接受键鼠控制。
	ShareEnabled bool `json:"share_enabled"`
	// Peers 是已配对设备。
	Peers []Peer `json:"peers"`
}

// Peer 是一台已配对的机器。
type Peer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// PSK 是配对时派生的 32 字节共享密钥（base64），知道它就等于身份。
	PSK string `json:"psk"`
	// Addr 是最近一次成功连接的地址，重连优先用它。
	Addr string `json:"addr,omitempty"`
	// Position 是它在本机屏幕的哪一侧："left"、"right" 或空（不参与边缘切换）。
	Position string `json:"position,omitempty"`
	// Enabled 为 false 时不断连、不接受它的控制。
	Enabled bool `json:"enabled"`
	// AddedAt 是配对时间（Unix 秒）。
	AddedAt int64 `json:"added_at"`
}

const (
	DefaultDiscoveryPort = 41832
	defaultMaxFileBytes  = int64(2) << 30 // 2 GiB
)

// DefaultConfigPath 返回 desk.json 的默认路径（按用户）。
func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "winforge-desk.json"
	}
	return filepath.Join(dir, "winforge", "desk.json")
}

// DefaultConfig 生成一份带全新 ID 的默认配置。
func DefaultConfig() (*Config, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "本机"
	}
	downloads := ""
	if home, err := os.UserHomeDir(); err == nil {
		downloads = filepath.Join(home, "Downloads", "WinForgeDesk")
	}
	return &Config{
		ID:            id,
		Name:          host,
		UIPort:        41830,
		ControlPort:   41831,
		DiscoveryPort: DefaultDiscoveryPort,
		Hotkey:        "Ctrl+Alt+D",
		DownloadsDir:  downloads,
		MaxFileBytes:  defaultMaxFileBytes,
		ShareEnabled:  true,
	}, nil
}

// LoadConfig 读取配置；文件不存在时生成并写回默认配置。
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		cfg, err := DefaultConfig()
		if err != nil {
			return nil, err
		}
		return cfg, SaveConfig(path, cfg)
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	if cfg.ID == "" {
		return nil, errors.New("配置缺少 id")
	}
	if cfg.Name == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "本机"
		}
		cfg.Name = host
	}
	if cfg.DiscoveryPort == 0 {
		cfg.DiscoveryPort = DefaultDiscoveryPort
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultMaxFileBytes
	}
	if cfg.DownloadsDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.DownloadsDir = filepath.Join(home, "Downloads", "WinForgeDesk")
		}
	}
	if cfg.Hotkey == "" {
		cfg.Hotkey = "Ctrl+Alt+D"
	}
	return &cfg, nil
}

// SaveConfig 原子写回配置。
func SaveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Peer 返回指定 ID 的配对记录。
func (c *Config) Peer(id string) (Peer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.Peers {
		if p.ID == id {
			return p, true
		}
	}
	return Peer{}, false
}

// UpPeer 新增或更新一条配对记录并写盘。
func (c *Config) UpPeer(path string, p Peer) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Peers {
		if c.Peers[i].ID == p.ID {
			c.Peers[i] = p
			return SaveConfig(path, c)
		}
	}
	c.Peers = append(c.Peers, p)
	return SaveConfig(path, c)
}

// RemovePeer 删除配对记录并写盘；对端不存在时报错。
func (c *Config) RemovePeer(path, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.Peers[:0]
	found := false
	for _, p := range c.Peers {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return fmt.Errorf("没有配对设备 %s", id)
	}
	c.Peers = out
	return SaveConfig(path, c)
}

// SetPeerPosition 更新对端方位（"left"/"right"/""），并保证每侧至多一台。
func (c *Config) SetPeerPosition(path, id, pos string) error {
	if pos != "left" && pos != "right" && pos != "" {
		return fmt.Errorf("非法方位 %q", pos)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	found := false
	for i := range c.Peers {
		if c.Peers[i].Position == pos && pos != "" && c.Peers[i].ID != id {
			c.Peers[i].Position = ""
		}
		if c.Peers[i].ID == id {
			c.Peers[i].Position = pos
			found = true
		}
	}
	if !found {
		return fmt.Errorf("没有配对设备 %s", id)
	}
	return SaveConfig(path, c)
}

// SetPeerEnabled 更新启用开关并写盘。
func (c *Config) SetPeerEnabled(path, id string, on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Peers {
		if c.Peers[i].ID == id {
			c.Peers[i].Enabled = on
			return SaveConfig(path, c)
		}
	}
	return fmt.Errorf("没有配对设备 %s", id)
}

// PeersSnapshot 返回配对列表的并发安全快照（外部直接遍历 Peers 字段会 race）。
func (c *Config) PeersSnapshot() []Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Peer(nil), c.Peers...)
}

// 快照 getter：hub 各 goroutine 读取标量配置的并发安全入口。
func (c *Config) NameV() string         { c.mu.Lock(); defer c.mu.Unlock(); return c.Name }
func (c *Config) SelfID() string        { c.mu.Lock(); defer c.mu.Unlock(); return c.ID }
func (c *Config) ShareEnabledV() bool   { c.mu.Lock(); defer c.mu.Unlock(); return c.ShareEnabled }
func (c *Config) HotkeyV() string       { c.mu.Lock(); defer c.mu.Unlock(); return c.Hotkey }
func (c *Config) DownloadsDirV() string { c.mu.Lock(); defer c.mu.Unlock(); return c.DownloadsDir }
func (c *Config) MaxFileBytesV() int64  { c.mu.Lock(); defer c.mu.Unlock(); return c.MaxFileBytes }

// SetMaxFileBytes 运行时更新传输上限（只改内存，不落盘）。
func (c *Config) SetMaxFileBytes(v int64) { c.mu.Lock(); c.MaxFileBytes = v; c.mu.Unlock() }

// newID 生成 16 字节随机 hex。
func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// defaultPeerAddress 推断对端默认地址：优先记录地址，否则空。
func sanitizeDisplayName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\n", " "))
	const max = 64
	if len(name) > max {
		r := []rune(name)
		if len(r) > max {
			name = string(r[:max])
		}
	}
	if name == "" {
		name = "未知设备"
	}
	return name
}

// downloadsDirDefault 供测试与其他平台兜底。
func downloadsDirDefault() string {
	switch runtime.GOOS {
	case "windows":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Downloads", "WinForgeDesk")
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Downloads", "WinForgeDesk")
		}
	}
	return filepath.Join(os.TempDir(), "WinForgeDesk")
}
