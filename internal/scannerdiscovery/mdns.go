// Package scannerdiscovery finds IPv4 Bonjour eSCL advertisements on connected LANs.
// Advertisements are candidates, never permission to scan or proof of identity.
package scannerdiscovery

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

var ErrUnavailable = errors.New("scanner discovery unavailable")

type Candidate struct{ Name, Endpoint string }
type record struct {
	value   dnsmessage.Resource
	expires time.Time
	sender  netip.Addr
}
type cache map[string]record

var services = []string{"_uscan._tcp.local.", "_uscans._tcp.local."}

// Browse uses shared UDP 5353 sockets for both unicast and multicast mDNS replies.
// No daemon, raw socket or extra Linux capability is needed.
// A round has bounded time, packets, records and output, including on hostile LANs.
func Browse(ctx context.Context) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, ErrUnavailable
	}
	records := cache{}
	queried := map[string]bool{}
	for _, iface := range interfaces {
		if ctx.Err() != nil {
			break
		}
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addrs {
			prefix, err := netip.ParsePrefix(address.String())
			if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsGlobalUnicast() {
				continue
			}
			conn, err := net.ListenMulticastUDP("udp4", &iface, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
			if err != nil {
				continue
			}
			packet := ipv4.NewPacketConn(conn)
			if packet.SetMulticastInterface(&iface) != nil || packet.SetMulticastTTL(255) != nil || packet.SetMulticastLoopback(true) != nil {
				conn.Close()
				continue
			}
			query := dnsmessage.Message{}
			for _, service := range services {
				query.Questions = append(query.Questions, dnsmessage.Question{Name: dnsmessage.MustNewName(service), Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET | 0x8000})
			}
			raw, _ := query.Pack()
			_ = conn.SetDeadline(time.Now().Add(min(time.Second, time.Until(deadline(ctx)))))
			if _, err = conn.WriteToUDP(raw, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}); err == nil {
				buffer := make([]byte, 9001)
				for count := 0; count < 128 && ctx.Err() == nil; count++ {
					n, sender, err := conn.ReadFromUDP(buffer)
					if err != nil {
						break
					}
					ip, ok := netip.AddrFromSlice(sender.IP)
					if !ok || sender.Port != 5353 {
						continue
					}
					ip = ip.Unmap()
					if !prefix.Contains(ip) || ip == prefix.Addr() {
						continue
					}
					records.ingest(buffer[:n], ip, time.Now())
					questions := records.questions(ip, queried, time.Now())
					if len(questions) > 0 {
						followup, _ := (&dnsmessage.Message{Questions: questions}).Pack()
						_, _ = conn.WriteToUDP(followup, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
					}

				}
			}
			conn.Close()
		}
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, ctx.Err()
	}
	return records.candidates(time.Now()), nil
}
func deadline(ctx context.Context) time.Time { value, _ := ctx.Deadline(); return value }
func (c cache) ingest(raw []byte, sender netip.Addr, now time.Time) {
	if len(raw) > 9000 {
		return
	}
	var message dnsmessage.Message
	if message.Unpack(raw) != nil || !message.Header.Response || message.Header.OpCode != 0 || message.Header.RCode != 0 || message.Header.Truncated || len(message.Answers)+len(message.Additionals) > 64 {
		return
	}
	for _, resource := range append(message.Answers, message.Additionals...) {
		if resource.Header.Class&0x7fff != dnsmessage.ClassINET {
			continue
		}
		name := strings.ToLower(resource.Header.Name.String())
		if !strings.HasSuffix(name, ".local.") {
			continue
		}
		key := sender.String() + "/" + name + "/" + strconv.Itoa(int(resource.Header.Type))
		if resource.Header.TTL == 0 {
			delete(c, key)
			continue
		}
		if len(c) >= 512 {
			continue
		}
		c[key] = record{resource, now.Add(time.Duration(min(resource.Header.TTL, 120)) * time.Second), sender}
	}
}
func (c cache) candidates(now time.Time) []Candidate {
	out := []Candidate{}
	seen := map[string]bool{}
	find := func(name string, kind dnsmessage.Type, sender netip.Addr) (dnsmessage.Resource, bool) {
		r, ok := c[sender.String()+"/"+strings.ToLower(name)+"/"+strconv.Itoa(int(kind))]
		return r.value, ok && now.Before(r.expires)
	}
	for _, entry := range c {
		ptr, ok := entry.value.Body.(*dnsmessage.PTRResource)
		if !ok || !now.Before(entry.expires) {
			continue
		}
		service := strings.ToLower(entry.value.Header.Name.String())
		if service != services[0] && service != services[1] {
			continue
		}
		instance := ptr.PTR.String()
		if !strings.HasSuffix(strings.ToLower(instance), "."+service) {
			continue
		}
		resource, ok := find(instance, dnsmessage.TypeSRV, entry.sender)
		if !ok {
			continue
		}
		srv, ok := resource.Body.(*dnsmessage.SRVResource)
		if !ok || srv.Port == 0 {
			continue
		}
		resource, ok = find(srv.Target.String(), dnsmessage.TypeA, entry.sender)
		if !ok {
			continue
		}
		a, ok := resource.Body.(*dnsmessage.AResource)
		if !ok || netip.AddrFrom4(a.A) != entry.sender || !entry.sender.IsGlobalUnicast() {
			continue
		}
		resource, ok = find(instance, dnsmessage.TypeTXT, entry.sender)
		if !ok {
			continue
		}
		txt, ok := resource.Body.(*dnsmessage.TXTResource)
		if !ok {
			continue
		}
		path := "eSCL"
		valid := true
		keys := map[string]bool{}
		for _, value := range txt.TXT {
			key, value, _ := strings.Cut(value, "=")
			key = strings.ToLower(key)
			if keys[key] {
				valid = false
				break
			}
			keys[key] = true
			if key == "rs" {
				path = value
			}
		}
		if !valid || path == "" || len(path) > 256 || strings.ContainsAny(path, "\\?#%\r\n") {
			continue
		}
		path = "/" + strings.TrimLeft(path, "/")
		for _, part := range strings.Split(path, "/") {
			if part == "." || part == ".." {
				valid = false
			}
		}
		name := instance[:len(instance)-len(service)-1]
		if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 {
			continue
		}
		if strings.ContainsFunc(name+path, func(r rune) bool { return unicode.IsControl(r) }) {
			continue
		}
		scheme := "http"
		if service == services[1] {
			scheme = "https"
		}
		endpoint := (&url.URL{Scheme: scheme, Host: net.JoinHostPort(entry.sender.String(), strconv.Itoa(int(srv.Port))), Path: path}).String()
		if valid && !seen[endpoint] {
			seen[endpoint] = true
			out = append(out, Candidate{name, endpoint})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out[:min(len(out), 32)]
}

// Some scanners put PTR, SRV/TXT and address records in separate replies.
func (c cache) questions(sender netip.Addr, queried map[string]bool, now time.Time) []dnsmessage.Question {
	out := []dnsmessage.Question{}
	ask := func(name dnsmessage.Name, kind dnsmessage.Type) {
		key := sender.String() + "/" + strings.ToLower(name.String()) + "/" + strconv.Itoa(int(kind))
		if r, ok := c[key]; ok && now.Before(r.expires) {
			return
		}
		if queried[key] || len(queried) >= 64 {
			return
		}
		queried[key] = true
		out = append(out, dnsmessage.Question{Name: name, Type: kind, Class: dnsmessage.ClassINET | 0x8000})
	}
	for _, entry := range c {
		if entry.sender != sender || !now.Before(entry.expires) {
			continue
		}
		service := strings.ToLower(entry.value.Header.Name.String())
		if ptr, ok := entry.value.Body.(*dnsmessage.PTRResource); ok && (service == services[0] || service == services[1]) && strings.HasSuffix(strings.ToLower(ptr.PTR.String()), "."+service) {
			ask(ptr.PTR, dnsmessage.TypeSRV)
			ask(ptr.PTR, dnsmessage.TypeTXT)
		}
		if srv, ok := entry.value.Body.(*dnsmessage.SRVResource); ok && (strings.HasSuffix(service, "._uscan._tcp.local.") || strings.HasSuffix(service, "._uscans._tcp.local.")) && strings.HasSuffix(strings.ToLower(srv.Target.String()), ".local.") {
			ask(srv.Target, dnsmessage.TypeA)
		}
	}
	return out
}
