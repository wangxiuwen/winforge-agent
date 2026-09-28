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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

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
		// ProxyFromEnvironment 支持 HTTPS_PROXY 走本地转发(如 macOS 本地网络
		// 权限拦截时经豁免工具出站),隧道不影响上层证书指纹校验。
		http: &http.Client{Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: tlsConfig,
		}},
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

// Shell 打开一条远端交互终端会话（WebSocket）：stdin 的键入以 Binary 帧
// 上行（EOF 发 close 控制帧结束会话），终端输出以 Binary 帧写 stdout，
// resize 通道接收窗口尺寸变化（可为 nil）。返回远端 shell 的退出码。
func (c *Client) Shell(ctx context.Context, start agent.ShellStart, stdin io.Reader, stdout io.Writer, resize <-chan agent.TermSize) (int, error) {
	q := url.Values{}
	q.Set("cwd", start.Cwd)
	q.Set("cols", strconv.Itoa(int(start.Cols)))
	q.Set("rows", strconv.Itoa(int(start.Rows)))
	wsURL := wsScheme(c.profile.Host) + "/v1/shell?" + q.Encode()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: c.http,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.profile.Token}},
	})
	if err != nil {
		return -1, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	var sendMu sync.Mutex
	sendText := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		sendMu.Lock()
		defer sendMu.Unlock()
		return conn.Write(ctx, websocket.MessageText, b)
	}
	if resize != nil {
		go func() {
			for size := range resize {
				if sendText(map[string]any{"resize": size}) != nil {
					return
				}
			}
		}()
	}
	// 键入转发：stdin EOF = 用户要结束会话（Ctrl-D），发 close 控制帧，
	// 服务端收尾后回 exit 事件。
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdin.Read(buf)
			if n > 0 {
				sendMu.Lock()
				werr := conn.Write(ctx, websocket.MessageBinary, buf[:n])
				sendMu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				_ = sendText(map[string]any{"close": true})
				return
			}
		}
	}()

	exitCode := -1
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch typ {
		case websocket.MessageBinary:
			if _, werr := stdout.Write(data); werr != nil {
				return exitCode, werr
			}
		case websocket.MessageText:
			var event struct {
				Exit *struct {
					Code  int    `json:"code"`
					Error string `json:"error"`
				} `json:"exit"`
			}
			if json.Unmarshal(data, &event) == nil && event.Exit != nil {
				exitCode = event.Exit.Code
				if event.Exit.Error != "" {
					return exitCode, fmt.Errorf("远端会话: %s", event.Exit.Error)
				}
			}
		}
	}
	if exitCode < 0 {
		return exitCode, fmt.Errorf("连接中断，远端会话已结束")
	}
	return exitCode, nil
}

// wsScheme 把 agent 的 https:// host 换成 wss://（http 同理换 ws）。
func wsScheme(host string) string {
	switch {
	case strings.HasPrefix(host, "https://"):
		return "wss://" + strings.TrimPrefix(host, "https://")
	case strings.HasPrefix(host, "http://"):
		return "ws://" + strings.TrimPrefix(host, "http://")
	default:
		return "wss://" + host
	}
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
