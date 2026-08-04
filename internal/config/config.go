package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type Config struct {
	Listen         string `json:"listen"`
	Root           string `json:"root"`
	Token          string `json:"token"`
	CertFile       string `json:"cert_file"`
	KeyFile        string `json:"key_file"`
	LogFile        string `json:"log_file,omitempty"`
	MaxUploadBytes int64  `json:"max_upload_bytes"`
	// Advertise 控制是否在局域网内用 mDNS 公告自己；为空视为开启。
	Advertise *bool `json:"advertise,omitempty"`
	// InstanceName 是公告用的实例名，为空时取主机名。
	InstanceName   string        `json:"instance_name,omitempty"`
	CommandTimeout time.Duration `json:"-"`
	TimeoutText    string        `json:"command_timeout"`
}

// AdvertiseEnabled 在旧配置没有该字段时默认开启，保持"重启换 IP 也能被找到"。
func (c Config) AdvertiseEnabled() bool {
	return c.Advertise == nil || *c.Advertise
}

func DefaultPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("PROGRAMDATA")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "WinForge", "config.json")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "winforge", "config.json")
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("解析配置: %w", err)
	}
	if err := cfg.Validate(path); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) Validate(configPath string) error {
	if c.Listen == "" || c.Root == "" || c.Token == "" {
		return errors.New("listen、root 和 token 不能为空")
	}
	if c.CertFile == "" || c.KeyFile == "" {
		return errors.New("cert_file 和 key_file 不能为空")
	}
	base := filepath.Dir(configPath)
	c.Root = absolute(base, c.Root)
	c.CertFile = absolute(base, c.CertFile)
	c.KeyFile = absolute(base, c.KeyFile)
	if c.LogFile != "" {
		c.LogFile = absolute(base, c.LogFile)
	}
	if c.MaxUploadBytes <= 0 {
		c.MaxUploadBytes = 512 << 20
	}
	if c.TimeoutText == "" {
		c.TimeoutText = "30m"
	}
	d, err := time.ParseDuration(c.TimeoutText)
	if err != nil || d <= 0 {
		return fmt.Errorf("无效 command_timeout: %q", c.TimeoutText)
	}
	c.CommandTimeout = d
	return nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func absolute(base, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(base, value)
}
