package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/agent"
)

type Client struct {
	profile Profile
	http    *http.Client
}

func New(profile Profile) (*Client, error) {
	fingerprint, err := hex.DecodeString(strings.ReplaceAll(profile.Fingerprint, ":", ""))
	if err != nil || len(fingerprint) != sha256.Size {
		return nil, fmt.Errorf("无效证书指纹")
	}
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // 由下方 SHA-256 pin 替代系统 CA 验证。
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("服务端没有提供证书")
			}
			sum := sha256.Sum256(state.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(sum[:], fingerprint) != 1 {
				return fmt.Errorf("服务端证书指纹不匹配")
			}
			return nil
		},
	}
	return &Client{
		profile: profile,
		http:    &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	}, nil
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	base := strings.TrimRight(c.profile.Host, "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.profile.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) Status(ctx context.Context, out io.Writer) error {
	resp, err := c.request(ctx, http.MethodGet, "/v1/status", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	return err
}

func (c *Client) Mkdir(ctx context.Context, path string) error {
	body, _ := json.Marshal(map[string]string{"path": path})
	resp, err := c.request(ctx, http.MethodPost, "/v1/mkdir", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectOK(resp)
}

func (c *Client) Upload(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	path := "/v1/upload?path=" + url.QueryEscape(remotePath)
	resp, err := c.request(ctx, http.MethodPost, path, f)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return expectOK(resp)
}

func (c *Client) Download(ctx context.Context, remotePath, localPath string) error {
	path := "/v1/download?path=" + url.QueryEscape(remotePath)
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil && filepath.Dir(localPath) != "." {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(localPath), ".winforge-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, localPath)
}

func (c *Client) Exec(ctx context.Context, req agent.ExecRequest, stdout, stderr io.Writer) (int, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return -1, err
	}
	resp, err := c.request(ctx, http.MethodPost, "/v1/exec", bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if err := expectOK(resp); err != nil {
		return -1, err
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	exitCode := -1
	for scanner.Scan() {
		var event agent.ExecEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return -1, err
		}
		switch event.Stream {
		case "stdout":
			fmt.Fprintln(stdout, event.Data)
		case "stderr":
			fmt.Fprintln(stderr, event.Data)
		case "exit":
			exitCode = event.ExitCode
			if event.Error != "" {
				return exitCode, fmt.Errorf("远端执行: %s", event.Error)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return -1, err
	}
	if exitCode < 0 {
		return -1, fmt.Errorf("远端未返回退出码")
	}
	return exitCode, nil
}

func expectOK(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Minute)
}
