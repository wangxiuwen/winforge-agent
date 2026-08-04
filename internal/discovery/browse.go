package discovery

import (
	"context"
	"errors"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

// Browse 在局域网内查找 Agent，直到超时或 ctx 结束。
//
// 查询带单播回应位，响应收在临时端口上，因此客户端不需要占用 5353，
// 在 macOS 上不会和系统 mDNS 响应器冲突，也不需要管理员权限。
func Browse(ctx context.Context, timeout time.Duration) ([]Instance, error) {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	query, err := buildQuery()
	if err != nil {
		return nil, err
	}
	sendQuery(conn, query)

	go func() {
		// 首包可能丢，中途补发一次，代价只有一个 UDP 包。
		select {
		case <-ctx.Done():
		case <-time.After(timeout / 3):
			sendQuery(conn, query)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = conn.SetReadDeadline(time.Now())
	}()

	found := map[string]*Instance{}
	buf := make([]byte, 9000)
	for {
		if err := conn.SetReadDeadline(deadlineOf(ctx)); err != nil {
			break
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				break
			}
			break
		}
		_ = parseResponse(buf[:n], found)
	}

	instances := make([]Instance, 0, len(found))
	for _, instance := range found {
		if instance.Port == 0 || len(instance.Addrs) == 0 {
			continue // 只拿到 PTR、没拿到 SRV/A 的半截结果不返回。
		}
		instances = append(instances, *instance)
	}
	sortInstances(instances)
	return instances, nil
}

// Lookup 返回名字匹配、且指纹不冲突的实例。
func Lookup(ctx context.Context, name, fingerprint string, timeout time.Duration) (Instance, bool, error) {
	instances, err := Browse(ctx, timeout)
	if err != nil {
		return Instance{}, false, err
	}
	for _, instance := range instances {
		if instance.Name == name && instance.MatchesFingerprint(fingerprint) {
			return instance, true, nil
		}
	}
	return Instance{}, false, nil
}

func deadlineOf(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(3 * time.Second)
}

// sendQuery 逐个多播网卡发送，避免只走默认路由那一张网卡而漏掉另一个网段。
func sendQuery(conn *net.UDPConn, query []byte) {
	group := &net.UDPAddr{IP: net.ParseIP(multicastGroup4), Port: MulticastPort}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		_, _ = conn.WriteToUDP(query, group)
		return
	}
	packetConn := ipv4.NewPacketConn(conn)
	sent := false
	for i := range interfaces {
		iface := interfaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if err := packetConn.SetMulticastInterface(&iface); err != nil {
			continue
		}
		if _, err := conn.WriteToUDP(query, group); err == nil {
			sent = true
		}
	}
	if !sent {
		_, _ = conn.WriteToUDP(query, group)
	}
}
