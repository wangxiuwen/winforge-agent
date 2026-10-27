package desk

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 浏览器模式控制台：只绑 127.0.0.1，随机 token 鉴权，
// 事件用长轮询（/api/events?after=seq），命令走 /api/rpc。
// 桌面窗口模式（Wails）不用这里，但浏览器模式也是双实例自测的入口。

type uiEvent struct {
	Seq   int         `json:"seq"`
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type uiBus struct {
	mu      sync.Mutex
	seq     int
	subs    map[int]chan uiEvent
	nextSub int
	hist    []uiEvent // 有界回放环：补长轮询空档丢失的事件（?after=seq）
}

func newUIBus() *uiBus { return &uiBus{subs: map[int]chan uiEvent{}} }

// uiBusHistCap 回放环容量；超限后较早事件不可补（客户端以快照自愈）。
const uiBusHistCap = 512

func (b *uiBus) Emit(event string, data interface{}) {
	b.mu.Lock()
	b.seq++
	ev := uiEvent{Seq: b.seq, Event: event, Data: data}
	// desk:state 是高频全量快照，不进回放环——否则会把 chat/transfer/pair
	// 等低频关键事件挤出窗口；客户端重连本就会重新拉 snapshot。
	if event != "desk:state" {
		b.hist = append(b.hist, ev)
		if len(b.hist) > uiBusHistCap {
			b.hist = b.hist[len(b.hist)-uiBusHistCap:]
		}
	}
	subs := make([]chan uiEvent, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default: // 慢客户端丢事件，UI 会靠快照自愈
		}
	}
}

// subscribe 订阅事件流；after>0 时先回放 (after, seq] 的历史事件（写入 ch）。
// after 缺省（0）表示老客户端/首连，只投递实时事件不回放。
// 客户端 after 晚于当前 seq（如 hub 重启 seq 归零）时视为无缺口，不回放。
func (b *uiBus) subscribe(after int) (int, chan uiEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSub++
	id := b.nextSub
	ch := make(chan uiEvent, 256)
	b.subs[id] = ch
	if after > 0 && after < b.seq {
		var replay []uiEvent
		for _, ev := range b.hist {
			if ev.Seq > after {
				replay = append(replay, ev)
			}
		}
		// 只回放最近 256 条（=通道缓冲），保证持锁写入不阻塞。
		if len(replay) > 256 {
			replay = replay[len(replay)-256:]
		}
		for _, ev := range replay {
			ch <- ev
		}
	}
	return id, ch
}

func (b *uiBus) unsubscribe(id int) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}

// uiServer 是浏览器模式控制台。
type uiServer struct {
	hub  *Hub
	bus  *uiBus
	mux  *http.ServeMux
	addr string
}

// StartBrowserUI 启动 HTTP 控制台并把事件总线接到 Hub。
// 端口被占（强杀后的 TIME_WAIT 等）自动顺延，最多试 10 个。
// 返回实际地址（127.0.0.1:端口）。
func StartBrowserUI(h *Hub, port int) (string, error) {
	bus := newUIBus()
	s := &uiServer{hub: h, bus: bus, mux: http.NewServeMux()}
	h.AddEmitter(bus)
	var ln net.Listener
	var err error
	for try := 0; try < 10; try++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+try))
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", err
	}
	s.addr = ln.Addr().String()

	s.mux.HandleFunc("/", s.handleStatic)
	s.mux.HandleFunc("/api/rpc", s.auth(s.handleRPC))
	s.mux.HandleFunc("/api/events", s.auth(s.handleEvents))
	s.mux.HandleFunc("/api/upload", s.auth(s.handleUpload))

	srv := &http.Server{Handler: securityHeaders(s.mux)}
	go func() { _ = srv.Serve(ln) }()
	return s.addr, nil
}

func (s *uiServer) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("t")
		if token == "" {
			token = r.Header.Get("X-Desk-Token")
		}
		// 只允许本机来源，防其他网站借浏览器打进来。
		if r.Header.Get("Origin") != "" && !strings.HasPrefix(r.Header.Get("Origin"), "http://127.0.0.1") &&
			!strings.HasPrefix(r.Header.Get("Origin"), "http://localhost") {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		if token == "" || token != s.hub.Token() {
			http.Error(w, "需要控制台令牌", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *uiServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	f, err := WebFS().Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	_, _ = io.Copy(w, f)
}

// securityHeaders 给所有响应加控制台安全头（nosniff/no-referrer）。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// uiCommand 是 /api/rpc 的请求。
type uiCommand struct {
	Cmd  string        `json:"cmd"`
	Args []interface{} `json:"args"`
}

func argString(args []interface{}, i int) string {
	if i < len(args) {
		s, _ := args[i].(string)
		return s
	}
	return ""
}

func argBool(args []interface{}, i int) bool {
	if i < len(args) {
		b, _ := args[i].(bool)
		return b
	}
	return false
}

func (s *uiServer) handleRPC(w http.ResponseWriter, r *http.Request) {
	var c uiCommand
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h := s.hub
	var err error
	switch c.Cmd {
	case "snapshot":
		_ = json.NewEncoder(w).Encode(h.Snapshot())
		return
	case "pairStart":
		err = h.PairStart(argString(c.Args, 0))
	case "pairRespond":
		h.PairRespond(argString(c.Args, 0), argBool(c.Args, 1))
	case "pairOutConfirm":
		h.PairConfirmOut(argBool(c.Args, 0))
	case "peerPosition":
		err = h.PeerPosition(argString(c.Args, 0), argString(c.Args, 1))
	case "peerToggle":
		err = h.PeerToggle(argString(c.Args, 0), argBool(c.Args, 1))
	case "peerRemove":
		err = h.PeerRemove(argString(c.Args, 0))
	case "sendChat":
		err = h.SendChat(argString(c.Args, 0), argString(c.Args, 1))
	case "sendFile":
		err = h.SendFile(argString(c.Args, 0), argString(c.Args, 1))
	case "transferRespond":
		h.TransferRespond(argString(c.Args, 0), argBool(c.Args, 1))
	case "cancelTransfer":
		h.CancelTransfer(argString(c.Args, 0))
	case "clearChat":
		h.DeleteChatHistory(argString(c.Args, 0))
	case "chatHistory":
		_ = json.NewEncoder(w).Encode(h.ChatHistory(argString(c.Args, 0)))
		return
	case "takeControl":
		err = h.TakeControl(argString(c.Args, 0), -1)
	case "releaseControl":
		h.ReleaseControl(-1)
	case "toggleShare":
		h.ToggleShare(argBool(c.Args, 0))
	case "openPath":
		err = h.OpenPath(argString(c.Args, 0))
	default:
		http.Error(w, "未知命令 "+c.Cmd, http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"error": errString(err)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// handleEvents 长轮询：新事件出现后静默 300ms 即返回，空闲最多挂 25 秒。
func (s *uiServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	id, ch := s.bus.subscribe(after)
	defer s.bus.unsubscribe(id)
	batch := []uiEvent{}
	for {
		var tick <-chan time.Time
		if len(batch) > 0 {
			tick = time.After(300 * time.Millisecond)
		} else {
			tick = time.After(25 * time.Second)
		}
		select {
		case ev := <-ch:
			batch = append(batch, ev)
		drain:
			for {
				select {
				case ev2 := <-ch:
					batch = append(batch, ev2)
				default:
					break drain
				}
			}
		case <-tick:
			goto flush
		case <-r.Context().Done():
			return
		}
	}
flush:
	_ = json.NewEncoder(w).Encode(batch)
}

func (s *uiServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var part *multipart.Part
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FormName() == "file" {
			part = p
			break
		}
	}
	if part == nil {
		http.Error(w, "缺少 file 字段", http.StatusBadRequest)
		return
	}
	defer part.Close()
	tmp, err := os.CreateTemp("", "wfdesk-upload-*-"+sanitizeFileName(filepath.Base(part.FileName())))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	limit := s.hub.cfg.MaxFileBytesV()
	size, err := io.Copy(tmp, io.LimitReader(part, limit+1))
	saveErr := tmp.Close()
	if err != nil || saveErr != nil {
		_ = os.Remove(tmp.Name())
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	if size > limit {
		_ = os.Remove(tmp.Name())
		http.Error(w, "文件超过大小上限", http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.hub.SendFile(peer, tmp.Name()); err != nil {
		_ = os.Remove(tmp.Name())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 传输完成后清理临时文件由发送结束路径负责（见 hub）；这里标记临时属性。
	s.hub.markTempUpload(tmp.Name())
	_ = json.NewEncoder(w).Encode(map[string]string{"error": ""})
}
