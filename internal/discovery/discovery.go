// Package discovery 用 mDNS/DNS-SD 在局域网内公告和查找 Agent。
//
// 安全边界：发现结果是不可信提示。它只提供"去哪里连"，不提供"该不该信"。
// 身份仍然由 profile 里固定的证书指纹和 token 决定，客户端必须先校验指纹再发送 token。
// TXT 里带的指纹只是用来在多台 Agent 之间挑出正确的一台，不能替代带外获取的指纹。
package discovery

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const (
	// ServiceType 是 DNS-SD 服务类型，可用 `dns-sd -B _winforge._tcp` 浏览。
	ServiceType = "_winforge._tcp.local."
	// MulticastPort 是 mDNS 标准端口。
	MulticastPort = 5353

	multicastGroup4 = "224.0.0.251"
	defaultTTL      = 120
	txtVersion      = "1"
)

// Service 描述本机要公告的 Agent。
type Service struct {
	// Instance 是实例名，DNS-SD 里显示给用户的那一段，默认取主机名。
	Instance string
	// Hostname 是 SRV 指向的主机名，不含 .local 后缀。
	Hostname string
	Port     int
	// Fingerprint 是证书 SHA-256 指纹，公开信息，用于客户端在多台之间匹配。
	Fingerprint string
	// Addrs 是要公告的 IPv4 地址；为空时由 Advertise 自动枚举。
	Addrs []netip.Addr
}

// Instance 是一次发现的结果。
type Instance struct {
	Name        string
	Hostname    string
	Port        int
	Addrs       []netip.Addr
	Fingerprint string
	Version     string
}

// URLs 返回按地址展开的候选 https 地址，调用方需逐个验证指纹。
func (i Instance) URLs() []string {
	urls := make([]string, 0, len(i.Addrs))
	for _, addr := range i.Addrs {
		host := addr.String()
		if addr.Is6() {
			host = "[" + host + "]"
		}
		urls = append(urls, fmt.Sprintf("https://%s:%d", host, i.Port))
	}
	return urls
}

// MatchesFingerprint 判断实例公告的指纹是否与固定指纹一致。
// 公告里没带指纹时返回 true，交由后续 TLS pin 兜底。
func (i Instance) MatchesFingerprint(fingerprint string) bool {
	if i.Fingerprint == "" || fingerprint == "" {
		return true
	}
	return normalizeFingerprint(i.Fingerprint) == normalizeFingerprint(fingerprint)
}

func normalizeFingerprint(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, ":", ""))
}

// instanceFQDN 返回 "实例名._winforge._tcp.local."。
func instanceFQDN(instance string) string {
	return escapeLabel(instance) + "." + ServiceType
}

// escapeLabel 按 DNS 主文件语义转义实例名里的 '.' 和 '\'，
// 使带点的实例名不会被拆成多个 label。
func escapeLabel(label string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `.`, `\.`)
	return replacer.Replace(label)
}

func unescapeLabel(label string) string {
	var out strings.Builder
	for i := 0; i < len(label); i++ {
		if label[i] == '\\' && i+1 < len(label) {
			i++
			out.WriteByte(label[i])
			continue
		}
		out.WriteByte(label[i])
	}
	return out.String()
}

// instanceName 从 "实例名._winforge._tcp.local." 里取出实例名。
func instanceName(fqdn string) string {
	if !strings.HasSuffix(fqdn, "."+ServiceType) {
		return ""
	}
	return unescapeLabel(strings.TrimSuffix(fqdn, "."+ServiceType))
}

func txtRecords(service Service) [][]byte {
	records := [][]byte{[]byte("v=" + txtVersion)}
	if service.Fingerprint != "" {
		records = append(records, []byte("fp="+normalizeFingerprint(service.Fingerprint)))
	}
	return records
}

func applyTXT(instance *Instance, records [][]byte) {
	for _, record := range records {
		key, value, found := strings.Cut(string(record), "=")
		if !found {
			continue
		}
		switch key {
		case "fp":
			instance.Fingerprint = value
		case "v":
			instance.Version = value
		}
	}
}

func sortInstances(instances []Instance) {
	sort.Slice(instances, func(a, b int) bool { return instances[a].Name < instances[b].Name })
	for i := range instances {
		sort.Slice(instances[i].Addrs, func(a, b int) bool {
			left, right := instances[i].Addrs[a], instances[i].Addrs[b]
			if left.Is4() != right.Is4() {
				return left.Is4()
			}
			return left.Less(right)
		})
	}
}
