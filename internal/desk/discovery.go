package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

// 发现：UDP 定向广播 announce 包（默认 41832/UDP，双端需一致）。
// 广播只承载“去哪里连”：实例 ID、显示名、控制端口、是否已配对。
// 是否可信由配对码与 PSK 决定，本表中的地址仅作连接提示。

// announcePacket 是 UDP 广播载荷。
type announcePacket struct {
	V       int    `json:"v"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Paired  int    `json:"paired"`
	Sharing bool   `json:"sharing"`
}

const announceVersion = 1

// Presence 是一台在线设备（含自己广播看到的已配对设备）。
type Presence struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Addr     string    `json:"addr"`
	Port     int       `json:"port"`
	Paired   bool      `json:"paired"`
	Sharing  bool      `json:"sharing"`
	LastSeen time.Time `json:"-"`
}

// Discovery 周期广播自己并监听同网段广播。
type Discovery struct {
	mu          sync.Mutex
	selfID      string
	name        func() string
	port        func() int
	paired      func() int
	sharing     func() bool
	presence    map[string]*Presence
	onUpdate    func([]Presence)
	conn        *net.UDPConn
	knownPaired func(string) bool
}

// NewDiscovery 构造发现服务。回调 name/port/paired/sharing 每次广播时取当前值。
func NewDiscovery(selfID string, port int, name func() string, controlPort func() int,
	pairedCount func() int, sharing func() bool, isPaired func(string) bool, onUpdate func([]Presence)) *Discovery {
	return &Discovery{
		selfID:      selfID,
		port:        controlPort,
		name:        name,
		paired:      pairedCount,
		sharing:     sharing,
		presence:    map[string]*Presence{},
		onUpdate:    onUpdate,
		knownPaired: isPaired,
	}
}

// Start 启动广播与监听，直到 ctx 结束。
// 监听带 SO_REUSEADDR（unix 另加 SO_REUSEPORT）：同一台机器跑多个 desk
// 实例（双实例自测）或与系统服务共存时都能占住同一发现端口。
func (d *Discovery) Start(ctx context.Context, discoveryPort int) error {
	addr := &net.UDPAddr{Port: discoveryPort}
	cfg := net.ListenConfig{Control: reuseControl}
	conn, err := cfg.ListenPacket(context.Background(), "udp4", addr.String())
	if err != nil {
		return err
	}
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		conn.Close()
		return errors.New("发现监听类型异常")
	}
	d.conn = udpConn
	broadcast := &net.UDPAddr{IP: net.IPv4bcast, Port: discoveryPort}
	loopback := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: discoveryPort}

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		d.send(broadcast, loopback)
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return
			case <-ticker.C:
				d.send(broadcast, loopback)
			}
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p announcePacket
			if json.Unmarshal(buf[:n], &p) != nil || p.V != announceVersion || p.ID == "" || p.ID == d.selfID {
				continue
			}
			changed := d.upsert(p, from.IP.String())
			if changed && d.onUpdate != nil {
				d.onUpdate(d.List())
			}
		}
	}()
	return nil
}

func (d *Discovery) send(targets ...*net.UDPAddr) {
	p := announcePacket{
		V:       announceVersion,
		ID:      d.selfID,
		Name:    d.name(),
		Port:    d.port(),
		Paired:  d.paired(),
		Sharing: d.sharing(),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	for _, t := range targets {
		if _, err := d.conn.WriteToUDP(b, t); err != nil {
			return
		}
	}
}

// upsert 记录一次广播，返回表是否变化。
func (d *Discovery) upsert(p announcePacket, fromIP string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	cur, ok := d.presence[p.ID]
	if ok {
		cur.LastSeen = now
		if fromIP != "" {
			cur.Addr = fromIP
		}
		cur.Name = sanitizeDisplayName(p.Name)
		cur.Port = p.Port
		cur.Sharing = p.Sharing
		cur.Paired = d.knownPaired(p.ID)
		return true
	}
	d.presence[p.ID] = &Presence{
		ID:       p.ID,
		Name:     sanitizeDisplayName(p.Name),
		Addr:     fromIP,
		Port:     p.Port,
		Paired:   d.knownPaired(p.ID),
		Sharing:  p.Sharing,
		LastSeen: now,
	}
	return true
}

// Prune 剔除超过 ttl 未上报的设备，返回是否变化。
func (d *Discovery) Prune(ttl time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	changed := false
	for id, p := range d.presence {
		if time.Since(p.LastSeen) > ttl {
			delete(d.presence, id)
			changed = true
		}
	}
	return changed
}

// List 返回当前在线设备快照。
func (d *Discovery) List() []Presence {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Presence, 0, len(d.presence))
	for _, p := range d.presence {
		copied := *p
		out = append(out, copied)
	}
	return out
}

// StartPruner 周期清理离线设备并触发回调。
func (d *Discovery) StartPruner(ctx context.Context, interval, ttl time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if d.Prune(ttl) && d.onUpdate != nil {
					d.onUpdate(d.List())
				}
			}
		}
	}()
}
