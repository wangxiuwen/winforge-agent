package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

type Profile struct {
	Host        string `json:"host"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
	// Instance 是 mDNS 实例名。填了它，Host 因为 DHCP 变化而连不上时可以自动重新解析。
	Instance string `json:"instance,omitempty"`
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ProfilesDir 返回 profile 存放目录。
// WINFORGE_HOME 可以覆盖它，便于隔离多套配置，测试也用它避免写到用户真实目录。
func ProfilesDir() string {
	if base := os.Getenv("WINFORGE_HOME"); base != "" {
		return filepath.Join(base, "profiles")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "profiles"
	}
	return filepath.Join(dir, "winforge", "profiles")
}

func SaveProfile(name string, profile Profile) error {
	if !profileName.MatchString(name) {
		return fmt.Errorf("无效 profile 名称: %q", name)
	}
	dir := ProfilesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600)
}

func LoadProfile(name string) (Profile, error) {
	if !profileName.MatchString(name) {
		return Profile{}, fmt.Errorf("无效 profile 名称: %q", name)
	}
	b, err := os.ReadFile(filepath.Join(ProfilesDir(), name+".json"))
	if err != nil {
		return Profile{}, err
	}
	var profile Profile
	if err := json.Unmarshal(b, &profile); err != nil {
		return Profile{}, err
	}
	if profile.Host == "" || profile.Token == "" || profile.Fingerprint == "" {
		return Profile{}, fmt.Errorf("profile 缺少 host、token 或 fingerprint")
	}
	return profile, nil
}
