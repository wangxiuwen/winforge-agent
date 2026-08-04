package discovery

import (
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	classINET       = 1
	classCacheFlush = 1 << 15 // 响应里表示"唯一记录，替换缓存"
	classUnicast    = 1 << 15 // 问题里表示"请单播回我"
)

// buildQuery 生成 PTR 查询。带单播回应位，这样客户端不必占用 5353 端口，
// 在 macOS 上也就不会和系统 mDNS 响应器抢端口。
func buildQuery() ([]byte, error) {
	name, err := dnsmessage.NewName(ServiceType)
	if err != nil {
		return nil, err
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{RecursionDesired: false},
		Questions: []dnsmessage.Question{{
			Name:  name,
			Type:  dnsmessage.TypePTR,
			Class: dnsmessage.Class(classINET | classUnicast),
		}},
	}
	return msg.Pack()
}

// buildAnnouncement 生成完整的服务公告，ttl 为 0 时表示下线告别。
func buildAnnouncement(service Service, addrs []netip.Addr, ttl uint32) ([]byte, error) {
	answers, err := serviceRecords(service, addrs, ttl)
	if err != nil {
		return nil, err
	}
	msg := dnsmessage.Message{
		Header:  dnsmessage.Header{Response: true, Authoritative: true},
		Answers: answers,
	}
	return msg.Pack()
}

// answerFor 针对收到的查询构造应答。第二个返回值表示对方是否要求单播回应。
func answerFor(query []byte, service Service, addrs []netip.Addr) ([]byte, bool, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(query)
	if err != nil {
		return nil, false, err
	}
	if header.Response {
		return nil, false, nil
	}
	serviceName := instanceFQDN(service.Instance)
	hostName := hostFQDN(service.Hostname)

	matched := false
	unicast := false
	for {
		question, err := parser.Question()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return nil, false, err
		}
		name := question.Name.String()
		wantsUnicast := uint16(question.Class)&classUnicast != 0
		switch {
		case question.Type == dnsmessage.TypePTR && strings.EqualFold(name, ServiceType),
			(question.Type == dnsmessage.TypeSRV || question.Type == dnsmessage.TypeTXT ||
				question.Type == dnsmessage.TypeALL) && strings.EqualFold(name, serviceName),
			(question.Type == dnsmessage.TypeA || question.Type == dnsmessage.TypeALL) &&
				strings.EqualFold(name, hostName):
			matched = true
			unicast = unicast || wantsUnicast
		}
	}
	if !matched {
		return nil, false, nil
	}
	payload, err := buildAnnouncement(service, addrs, defaultTTL)
	if err != nil {
		return nil, false, err
	}
	return payload, unicast, nil
}

func serviceRecords(service Service, addrs []netip.Addr, ttl uint32) ([]dnsmessage.Resource, error) {
	serviceType, err := dnsmessage.NewName(ServiceType)
	if err != nil {
		return nil, err
	}
	serviceName, err := dnsmessage.NewName(instanceFQDN(service.Instance))
	if err != nil {
		return nil, err
	}
	hostName, err := dnsmessage.NewName(hostFQDN(service.Hostname))
	if err != nil {
		return nil, err
	}

	shared := dnsmessage.ResourceHeader{Name: serviceType, Class: classINET, TTL: ttl}
	unique := func(name dnsmessage.Name) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: name, Class: classINET | classCacheFlush, TTL: ttl}
	}

	resources := []dnsmessage.Resource{
		{Header: shared, Body: &dnsmessage.PTRResource{PTR: serviceName}},
		{Header: unique(serviceName), Body: &dnsmessage.SRVResource{
			Target: hostName,
			Port:   uint16(service.Port),
		}},
		{Header: unique(serviceName), Body: &dnsmessage.TXTResource{TXT: txtStrings(service)}},
	}
	for _, addr := range addrs {
		if !addr.Is4() {
			continue
		}
		resources = append(resources, dnsmessage.Resource{
			Header: unique(hostName),
			Body:   &dnsmessage.AResource{A: addr.As4()},
		})
	}
	return resources, nil
}

func txtStrings(service Service) []string {
	records := txtRecords(service)
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, string(record))
	}
	return out
}

// parseResponse 从一个 mDNS 响应包里提取实例信息，answer 与 additional 一起看。
func parseResponse(payload []byte, into map[string]*Instance) error {
	var parser dnsmessage.Parser
	header, err := parser.Start(payload)
	if err != nil {
		return err
	}
	if !header.Response {
		return nil
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return err
	}
	hosts := map[string][]netip.Addr{}
	targets := map[string]string{}

	collect := func(next func() (dnsmessage.Resource, error)) error {
		for {
			resource, err := next()
			if errors.Is(err, dnsmessage.ErrSectionDone) {
				return nil
			}
			if err != nil {
				return err
			}
			name := resource.Header.Name.String()
			switch body := resource.Body.(type) {
			case *dnsmessage.PTRResource:
				if !strings.EqualFold(name, ServiceType) {
					continue
				}
				instanceOf(into, body.PTR.String())
			case *dnsmessage.SRVResource:
				instance := instanceOf(into, name)
				if instance == nil {
					continue
				}
				if resource.Header.TTL == 0 {
					instance.Port = 0
					continue
				}
				instance.Port = int(body.Port)
				instance.Hostname = strings.TrimSuffix(body.Target.String(), ".")
				targets[name] = body.Target.String()
			case *dnsmessage.TXTResource:
				instance := instanceOf(into, name)
				if instance == nil {
					continue
				}
				records := make([][]byte, 0, len(body.TXT))
				for _, text := range body.TXT {
					records = append(records, []byte(text))
				}
				applyTXT(instance, records)
			case *dnsmessage.AResource:
				if resource.Header.TTL == 0 {
					continue
				}
				hosts[name] = append(hosts[name], netip.AddrFrom4(body.A))
			case *dnsmessage.AAAAResource:
				if resource.Header.TTL == 0 {
					continue
				}
				addr := netip.AddrFrom16(body.AAAA)
				if addr.Is4In6() {
					addr = addr.Unmap()
				}
				hosts[name] = append(hosts[name], addr)
			}
		}
	}

	if err := collect(parser.Answer); err != nil {
		return err
	}
	if err := collect(parser.Authority); err != nil {
		return err
	}
	if err := collect(parser.Additional); err != nil {
		return err
	}

	for name, instance := range into {
		target, ok := targets[name]
		if !ok {
			continue
		}
		for _, addr := range hosts[target] {
			instance.Addrs = appendUniqueAddr(instance.Addrs, addr)
		}
	}
	return nil
}

func instanceOf(into map[string]*Instance, fqdn string) *Instance {
	name := instanceName(fqdn)
	if name == "" {
		return nil
	}
	if existing, ok := into[fqdn]; ok {
		return existing
	}
	instance := &Instance{Name: name}
	into[fqdn] = instance
	return instance
}

func appendUniqueAddr(addrs []netip.Addr, addr netip.Addr) []netip.Addr {
	for _, existing := range addrs {
		if existing == addr {
			return addrs
		}
	}
	return append(addrs, addr)
}

func hostFQDN(hostname string) string {
	host := strings.TrimSuffix(strings.TrimSuffix(hostname, "."), ".local")
	if host == "" {
		host = "winforge"
	}
	return escapeLabel(host) + ".local."
}
