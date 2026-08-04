package discovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"golang.org/x/net/ipv4"
)

// Advertise 在局域网内公告 Agent，直到 ctx 结束。
// 它只公告实例名、主机名、端口和证书指纹，不公告 token、路径或任何其他本机信息。
func Advertise(ctx context.Context, service Service, logger *log.Logger) error {
	if logger == nil {
		logger = log.Default()
	}
	if service.Port <= 0 || service.Port > 65535 {
		return fmt.Errorf("无效端口: %d", service.Port)
	}
	if service.Instance == "" {
		service.Instance = DefaultInstanceName()
	}
	if service.Hostname == "" {
		service.Hostname = shortHostname()
	}

	conn, err := listenMulticast()
	if err != nil {
		return fmt.Errorf("监听 mDNS: %w", err)
	}
	defer conn.Close()

	packetConn := ipv4.NewPacketConn(conn)
	joined := joinAll(packetConn)
	if joined == 0 {
		logger.Printf("mDNS: 没有可用的多播网卡，发现功能不可用")
	}
	group := &net.UDPAddr{IP: net.ParseIP(multicastGroup4), Port: MulticastPort}

	announce := func(ttl uint32) {
		addrs := service.Addrs
		if len(addrs) == 0 {
			addrs = localAddrs()
		}
		payload, err := buildAnnouncement(service, addrs, ttl)
		if err != nil {
			logger.Printf("mDNS: 构造公告失败: %v", err)
			return
		}
		if _, err := conn.WriteToUDP(payload, group); err != nil {
			logger.Printf("mDNS: 发送公告失败: %v", err)
		}
	}

	logger.Printf("mDNS: 公告 %s 端口 %d", instanceFQDN(service.Instance), service.Port)
	announce(defaultTTL)
	go func() {
		// 首次公告可能丢包，标准做法是短间隔重发几次。
		for _, delay := range []time.Duration{time.Second, 2 * time.Second} {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				announce(defaultTTL)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		announce(0) // 告别，让客户端立刻知道这台下线了。
		_ = conn.SetReadDeadline(time.Now())
		_ = conn.Close()
	}()

	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		payload, unicast, err := answerFor(buf[:n], service, addrsOrLocal(service))
		if err != nil || payload == nil {
			continue
		}
		target := group
		if unicast {
			target = from
		}
		if _, err := conn.WriteToUDP(payload, target); err != nil && ctx.Err() == nil {
			logger.Printf("mDNS: 回应 %s 失败: %v", from, err)
		}
	}
}

func addrsOrLocal(service Service) []netip.Addr {
	if len(service.Addrs) > 0 {
		return service.Addrs
	}
	return localAddrs()
}

// DefaultInstanceName 返回默认实例名，取本机主机名。
func DefaultInstanceName() string {
	return shortHostname()
}

func shortHostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "winforge"
	}
	if index := strings.Index(host, "."); index > 0 {
		host = host[:index]
	}
	return host
}

func listenMulticast() (*net.UDPConn, error) {
	config := net.ListenConfig{Control: reuseControl}
	conn, err := config.ListenPacket(context.Background(), "udp4",
		fmt.Sprintf(":%d", MulticastPort))
	if err != nil {
		return nil, err
	}
	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("非 UDP 连接")
	}
	return udpConn, nil
}

func joinAll(packetConn *ipv4.PacketConn) int {
	group := net.ParseIP(multicastGroup4)
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0
	}
	joined := 0
	for i := range interfaces {
		iface := interfaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		if err := packetConn.JoinGroup(&iface, &net.UDPAddr{IP: group}); err == nil {
			joined++
		}
	}
	return joined
}

func localAddrs() []netip.Addr {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addrs []netip.Addr
	for i := range interfaces {
		iface := interfaces[i]
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ifaceAddr := range ifaceAddrs {
			prefix, err := netip.ParsePrefix(ifaceAddr.String())
			if err != nil {
				continue
			}
			addr := prefix.Addr()
			if addr.Is4In6() {
				addr = addr.Unmap()
			}
			if !addr.Is4() || addr.IsLinkLocalUnicast() {
				continue
			}
			addrs = appendUniqueAddr(addrs, addr)
		}
	}
	return addrs
}
