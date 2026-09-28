package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	// 空闲超时兜底：交互终端没有命令超时，长时间无人输入的会话必须被
	// 回收，否则泄漏的 conhost/powershell 会越积越多。
	defaultShellIdle = 10 * time.Minute
	// 并发会话上限。ConPTY 每会话各起一个 conhost + powershell，不设上限
	// 的话一个失控的循环就能拖垮构建机。
	maxShellSessions = 2
	defaultTermCols  = 120
	defaultTermRows  = 30
	// 键入粘贴是大消息的来源，给到 1 MiB。
	shellReadLimit = 1 << 20
)

// shellSession 是一条交互终端会话。Read 返回终端输出（Windows 上是 ConPTY
// 的 VT 字节流），Write 送入用户键入，Wait 阻塞到会话进程退出并返回退出码，
// Close 立即终止会话进程并释放伪控制台。
type shellSession interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Wait() (int, error)
	Close() error
}

// TermSize 以字符列数描述伪控制台尺寸。
type TermSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// ShellStart 是客户端握手时携带的会话启动参数（query 承载）。
type ShellStart struct {
	// Cwd 是 workspace 内相对路径，与 exec 同一套解析规则。
	Cwd  string `json:"cwd,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// shellResize 是客户端发来的控制消息（Text 帧）；close=true 表示客户端
// 键入结束（Ctrl-D），要求服务端收尾会话。
type shellResize struct {
	Resize *TermSize `json:"resize,omitempty"`
	Close  bool      `json:"close,omitempty"`
}

// shellExit 是服务端发出的收尾消息（Text 帧），之后连接关闭。
type shellExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// handleShell 把一条 WebSocket 双工连接接到伪控制台：Binary 帧 = 键入/
// 终端输出字节流，Text 帧承载 resize 与退出事件。参数走握手 query（cwd 为
// workspace 内相对路径，认证由外层 authenticate 完成）。
//
// 之所以不用「POST 请求体上行 + 响应体下行」：HTTP/1.1 下 Go 客户端要等
// 请求体发完才交付响应头，而终端的请求体（键入流）永不结束，实测死锁。
// WebSocket 的全双工帧正是终端语义。
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	// 非阻塞占座：满了立刻拒绝，不排队——排队只会让客户端对着黑洞等。
	select {
	case s.shellSlots <- struct{}{}:
		defer func() { <-s.shellSlots }()
	default:
		http.Error(w, "shell 会话数已达上限", http.StatusServiceUnavailable)
		return
	}
	cwd, err := s.workspace.Resolve(r.URL.Query().Get("cwd"), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cols, rows := normalizeSize(
		uint16(queryUint(r, "cols", defaultTermCols)),
		uint16(queryUint(r, "rows", defaultTermRows)))
	sess, err := s.newShellSession(cols, rows, cwd)
	if err != nil {
		http.Error(w, "无法启动 shell: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer sess.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "")
	conn.SetReadLimit(shellReadLimit)

	// 空闲看门狗：每条消息刷新 lastActive，超时即拆会话。AfterFunc 链式
	// Reset 有并发坑，定期检查最直白。
	var lastActive atomic.Int64
	lastActive.Store(time.Now().UnixNano())
	watchStop := make(chan struct{})
	defer close(watchStop)
	go func() {
		tick := s.shellIdle / 4
		if tick < time.Second {
			tick = time.Second
		}
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-watchStop:
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastActive.Load())) > s.shellIdle {
					cancel()
					sess.Close()
					return
				}
			}
		}
	}()

	// 输出泵：终端输出以 Binary 帧持续推给客户端，会话退出（Read EOF）后
	// 结束。读循环与 pump 是仅有的两个写者，coder/websocket 只允许一个并
	// 发写者，用 connMu 串行化。
	var connMu sync.Mutex
	writeFrame := func(typ websocket.MessageType, data []byte) error {
		connMu.Lock()
		defer connMu.Unlock()
		return conn.Write(ctx, typ, data)
	}
	pumpDone := make(chan struct{})
	sessionDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		buf := make([]byte, 32<<10)
		for {
			n, readErr := sess.Read(buf)
			if n > 0 {
				if writeFrame(websocket.MessageBinary, buf[:n]) != nil {
					return
				}
			}
			if readErr != nil {
				close(sessionDone) // 远端 shell 自己退了，别再等键入
				return
			}
		}
	}()
	readCtx, readCancel := context.WithCancel(ctx)
	defer readCancel()

	// 收尾：停会话 → 拿退出码 → 把 exit 事件排完终端输出后送达。三种触发
	// （客户端断开/close 帧、空闲看门狗、会话自行退出）都汇聚到这里，只执
	// 行一次。注意 coder/websocket 里 cancel 传给 conn.Read 的 ctx 会关闭
	// 整条连接——所以 exit 帧必须在 readCancel 之前发出。
	var finishOnce sync.Once
	graceful := func() {
		finishOnce.Do(func() {
			sess.Close()
			exit := shellExit{Code: -1}
			waitCh := make(chan int, 1)
			go func() { code, _ := sess.Wait(); waitCh <- code }()
			select {
			case exit.Code = <-waitCh:
			case <-time.After(5 * time.Second):
			}
			<-pumpDone // exit 事件排在终端输出之后，保证顺序
			if ctx.Err() == nil {
				_ = writeFrame(websocket.MessageText, mustJSON(map[string]any{"exit": exit}))
				_ = conn.Close(websocket.StatusNormalClosure, "")
			}
		})
	}
	go func() {
		select {
		case <-sessionDone:
			graceful()
			readCancel() // 连接已优雅关闭，解除主循环里挂着的 Read
		case <-readCtx.Done():
		}
	}()

	// 读循环：Binary = 键入；Text = 控制消息（resize / close）。
loop:
	for {
		typ, data, err := conn.Read(readCtx)
		if err != nil {
			break // 客户端断开、空闲看门狗或会话自行退出
		}
		lastActive.Store(time.Now().UnixNano())
		switch typ {
		case websocket.MessageBinary:
			_, _ = sess.Write(data)
		case websocket.MessageText:
			var msg shellResize
			if json.Unmarshal(data, &msg) == nil {
				if msg.Close {
					break loop // 客户端结束会话，走正常收尾
				}
				if msg.Resize != nil {
					c, rw := normalizeSize(msg.Resize.Cols, msg.Resize.Rows)
					_ = sess.Resize(c, rw)
				}
			}
		}
	}
	graceful()
}

func normalizeSize(cols, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = defaultTermCols
	}
	if rows == 0 {
		rows = defaultTermRows
	}
	return cols, rows
}

func queryUint(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
