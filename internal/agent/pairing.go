package agent

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wangxiuwen/winforge-agent/internal/security"
)

// 配对码的三道闸，缺一不可：6 位数字只有一百万种，没有这三样它就是个弱口令。
const (
	PairCodeTTL      = 10 * time.Minute // 过期
	PairMaxAttempts  = 5                // 错几次作废
	pairFailureDelay = 300 * time.Millisecond
)

var (
	ErrNoPendingPair = errors.New("没有待配对的请求：先在 Agent 上执行 winforge pair-code")
	ErrPairExpired   = errors.New("配对码已过期，请重新生成")
	ErrPairAttempts  = errors.New("配对码错误次数过多，已作废，请重新生成")
	ErrPairMismatch  = errors.New("配对码不正确")
)

// PendingPair 是一次待完成的配对。一次性：兑换成功即失效。
type PendingPair struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	used     bool
}

// Arm 生成一个新的配对码并覆盖旧的（重新生成即作废前一个）。
func (p *PendingPair) Arm(now time.Time) (string, time.Time, error) {
	code, err := security.NewPairCode()
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code, p.expires, p.attempts, p.used = code, now.Add(PairCodeTTL), 0, false
	return code, p.expires, nil
}

// Redeem 校验配对码。成功后立刻作废，防止同一个码被用第二次。
func (p *PendingPair) Redeem(code string, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || p.used {
		return ErrNoPendingPair
	}
	if now.After(p.expires) {
		p.code = ""
		return ErrPairExpired
	}
	if p.attempts >= PairMaxAttempts {
		p.code = ""
		return ErrPairAttempts
	}
	p.attempts++
	if subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		if p.attempts >= PairMaxAttempts {
			p.code = ""
			return ErrPairAttempts
		}
		return ErrPairMismatch
	}
	p.used, p.code = true, ""
	return nil
}

// handleArmPair 在 Agent 本机生成配对码。它走正常的 token 校验——能读到配置文件的人
// 本来就持有 token，所以这里不额外发明一套权限；真正对外开放的只有 /v1/pair。
func (s *Server) handleArmPair(w http.ResponseWriter, r *http.Request) {
	if s.fingerprint == "" {
		http.Error(w, "证书指纹不可用，无法配对", http.StatusServiceUnavailable)
		return
	}
	code, expires, err := s.pending.Arm(time.Now())
	if err != nil {
		http.Error(w, "生成配对码失败", http.StatusInternalServerError)
		return
	}
	s.logger.Printf("配对码已生成，%s 前有效", PairCodeTTL)
	writeJSON(w, http.StatusOK, armPairResponse{Code: code, ExpiresAt: expires, Port: listenPort(s.cfg.Listen)})
}

type armPairResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	Port      string    `json:"port"`
}

func listenPort(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i+1:]
	}
	return listen
}

type pairRequest struct {
	Code string `json:"code"`
}

type pairResponse struct {
	Token string `json:"token"`
	// Proof = HMAC(配对码, 证书指纹)。客户端拿它握手时**实际看到的**指纹验证，
	// 这样指纹不用人抄，中间人也伪造不出来。
	Proof string `json:"proof"`
	Host  string `json:"host,omitempty"`
}

// handlePair 是唯一不需要 token 的接口——它就是用来拿 token 的。
// 因此这里的每一条防线都不能省。
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := s.pending.Redeem(req.Code, time.Now()); err != nil {
		// 慢一点回，别让人拿它当在线爆破的计时器。
		time.Sleep(pairFailureDelay)
		status := http.StatusForbidden
		if errors.Is(err, ErrNoPendingPair) {
			status = http.StatusConflict
		}
		s.logger.Printf("pair rejected: %v", err)
		http.Error(w, err.Error(), status)
		return
	}
	s.logger.Printf("pair accepted from %s", r.RemoteAddr)
	writeJSON(w, http.StatusOK, pairResponse{
		Token: s.cfg.Token,
		Proof: security.PairProof(req.Code, s.fingerprint),
		Host:  hostname(),
	})
}
