package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wangxiuwen/winforge-agent/internal/config"
)

// fakeSession 把写入的内容原样回显成输出，用来在测试里跑通协议层；
// out 关闭后 Read 返回 EOF，模拟会话进程退出。
type fakeSession struct {
	mu       sync.Mutex
	out      chan []byte
	closed   bool
	written  []byte
	resized  []TermSize
	closeCnt int
	waitCh   chan int
}

func newFakeSession() *fakeSession {
	return &fakeSession{out: make(chan []byte, 16), waitCh: make(chan int, 1)}
}

func (f *fakeSession) Read(p []byte) (int, error) {
	b, ok := <-f.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}

func (f *fakeSession) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.written = append(f.written, p...)
	f.mu.Unlock()
	b := append([]byte(nil), p...)
	select {
	case f.out <- b:
	default:
	}
	return len(p), nil
}

func (f *fakeSession) Resize(cols, rows uint16) error {
	f.mu.Lock()
	f.resized = append(f.resized, TermSize{Cols: cols, Rows: rows})
	f.mu.Unlock()
	return nil
}

func (f *fakeSession) Wait() (int, error) { return <-f.waitCh, nil }

func (f *fakeSession) Close() error {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		close(f.out)
	}
	f.closeCnt++
	f.mu.Unlock()
	select {
	case f.waitCh <- 0:
	default:
	}
	return nil
}

func (f *fakeSession) gotWritten() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.written...)
}

func (f *fakeSession) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCnt
}

// newShellTestServer 起一个注入假会话工厂的测试服务端，返回服务器与已创建
// 的会话通道（每新建一个会话推一个进来）。
func newShellTestServer(t *testing.T, idle time.Duration) (*httptest.Server, chan *fakeSession) {
	t.Helper()
	sessions := make(chan *fakeSession, 8)
	s, err := New(config.Config{Token: "test-token", Root: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.shellIdle = idle
	s.newShellSession = func(cols, rows uint16, cwd string) (shellSession, error) {
		f := newFakeSession()
		sessions <- f
		return f, nil
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, sessions
}

func wsShellURL(tsURL, query string) string {
	return "ws://" + strings.TrimPrefix(tsURL, "http://") + "/v1/shell?" + query
}

func dialShell(t *testing.T, tsURL, query, token string) (*websocket.Conn, error) {
	t.Helper()
	hdr := http.Header{}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, wsShellURL(tsURL, query), &websocket.DialOptions{
		HTTPHeader: hdr,
	})
	return conn, err
}

func TestShellRequiresAuth(t *testing.T) {
	ts, _ := newShellTestServer(t, time.Minute)
	if _, err := dialShell(t, ts.URL, "cwd=.", ""); err == nil {
		t.Fatal("无 token 的握手应失败")
	}
}

func TestShellRejectsTraversalCwd(t *testing.T) {
	ts, _ := newShellTestServer(t, time.Minute)
	if _, err := dialShell(t, ts.URL, `cwd=..\..\Windows`, "test-token"); err == nil {
		t.Fatal("越界 cwd 的握手应失败")
	}
}

func TestShellRoundtripAndExit(t *testing.T) {
	ts, sessions := newShellTestServer(t, time.Minute)
	conn, err := dialShell(t, ts.URL, "cwd=.&cols=100&rows=30", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("dir\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"resize":{"cols":80,"rows":24}}`)); err != nil {
		t.Fatal(err)
	}
	// 回显：写进去的字节应以 Binary 帧原样回来
	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "dir\r\n" {
		t.Fatalf("回显不符: typ=%v data=%q err=%v", typ, data, err)
	}
	// 结束会话
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"close":true}`)); err != nil {
		t.Fatal(err)
	}
	typ, data, err = conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("应收到 exit 事件，got typ=%v err=%v", typ, err)
	}
	var event struct {
		Exit *shellExit `json:"exit"`
	}
	if err := json.Unmarshal(data, &event); err != nil || event.Exit == nil || event.Exit.Code != 0 {
		t.Fatalf("应收到 exit 0，got %s (err=%v)", data, err)
	}

	f := <-sessions
	if string(f.gotWritten()) != "dir\r\n" {
		t.Fatalf("会话没收到输入: %q", f.gotWritten())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.resized) != 1 || f.resized[0].Cols != 80 {
		t.Fatalf("resize 未送达: %+v", f.resized)
	}
}

func TestShellIdleTimeout(t *testing.T) {
	ts, sessions := newShellTestServer(t, 300*time.Millisecond)
	conn, err := dialShell(t, ts.URL, "cwd=.", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	f := <-sessions
	// 不发任何输入：看门狗（tick 最长 1s）应在几秒内拆会话
	deadline := time.Now().Add(5 * time.Second)
	for f.closeCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if f.closeCount() == 0 {
		t.Fatal("空闲超时后未关闭会话")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("客户端应看到连接被服务端关闭")
	}
}

func TestShellSessionLimit(t *testing.T) {
	ts, _ := newShellTestServer(t, time.Minute)
	conns := make([]*websocket.Conn, 0, maxShellSessions)
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()
	for i := 0; i < maxShellSessions; i++ {
		conn, err := dialShell(t, ts.URL, "cwd=.", "test-token")
		if err != nil {
			t.Fatalf("第 %d 个会话应成功: %v", i+1, err)
		}
		conns = append(conns, conn)
	}
	// 占满后的新握手要被立刻拒绝
	if _, err := dialShell(t, ts.URL, "cwd=.", "test-token"); err == nil {
		t.Fatal("超出并发上限的握手应失败")
	}
}

func TestNormalizeSize(t *testing.T) {
	cols, rows := normalizeSize(0, 0)
	if cols != defaultTermCols || rows != defaultTermRows {
		t.Fatalf("零值应取默认 %dx%d，got %dx%d", defaultTermCols, defaultTermRows, cols, rows)
	}
}
