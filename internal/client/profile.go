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
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func ProfilesDir() string {
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
