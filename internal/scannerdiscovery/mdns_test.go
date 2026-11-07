package scannerdiscovery

import (
	"golang.org/x/net/dns/dnsmessage"
	"net/netip"
	"testing"
	"time"
)

func advertisement(t *testing.T, ip [4]byte, path string) []byte {
	t.Helper()
	name := dnsmessage.MustNewName
	header := func(n string, typ dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: name(n), Type: typ, Class: dnsmessage.ClassINET, TTL: 120}
	}
	instance := "Canon TS8130._uscan._tcp.local."
	m := dnsmessage.Message{Header: dnsmessage.Header{Response: true}, Answers: []dnsmessage.Resource{{Header: header(services[0], dnsmessage.TypePTR), Body: &dnsmessage.PTRResource{PTR: name(instance)}}}, Additionals: []dnsmessage.Resource{
		{Header: header(instance, dnsmessage.TypeSRV), Body: &dnsmessage.SRVResource{Port: 80, Target: name("canon.local.")}},
		{Header: header(instance, dnsmessage.TypeTXT), Body: &dnsmessage.TXTResource{TXT: []string{"rs=" + path}}},
		{Header: header("canon.local.", dnsmessage.TypeA), Body: &dnsmessage.AResource{A: ip}},
	}}
	raw, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestAdvertisementBoundary(t *testing.T) {
	now := time.Now()
	ip := [4]byte{192, 168, 1, 20}
	sender := netip.AddrFrom4(ip)
	for _, tc := range []struct {
		name, path string
		address    [4]byte
		valid      bool
	}{
		{"ordinary", "eSCL", ip, true}, {"slash", "/eSCL", ip, true},
		{"foreign address", "eSCL", [4]byte{192, 168, 1, 21}, false},
		{"loopback", "eSCL", [4]byte{127, 0, 0, 1}, false},
		{"traversal", "../admin", ip, false}, {"encoded traversal", "%2e%2e/admin", ip, false},
		{"query", "eSCL?next=admin", ip, false}, {"fragment", "eSCL#secret", ip, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cache{}
			c.ingest(advertisement(t, tc.address, tc.path), sender, now)
			got := c.candidates(now)
			if (len(got) == 1) != tc.valid {
				t.Fatal(got)
			}
			if tc.valid && (got[0].Name != "Canon TS8130" || got[0].Endpoint != "http://192.168.1.20:80/eSCL") {
				t.Fatal(got)
			}
			if len(c.candidates(now.Add(121*time.Second))) != 0 {
				t.Fatal("expired advertisement retained")
			}
		})
	}
}
func TestMalformedAdvertisementAndRecordBudget(t *testing.T) {
	now := time.Now()
	ip := netip.MustParseAddr("192.168.1.20")
	c := cache{}
	for _, raw := range [][]byte{nil, {0, 1, 2}, make([]byte, 9001)} {
		c.ingest(raw, ip, now)
	}
	if len(c.candidates(now)) != 0 {
		t.Fatal("malformed accepted")
	}
	for n := 0; n < 600; n++ {
		c.ingest(advertisement(t, [4]byte{192, 168, 1, 20}, "eSCL"), netip.AddrFrom4([4]byte{192, 168, byte(n / 254), byte(n%254 + 1)}), now)
	}
	if len(c) > 512 {
		t.Fatal("unbounded cache", len(c))
	}
}

func TestSplitResponsesRequestMissingRecordsOnce(t *testing.T) {
	now := time.Now()
	sender := netip.MustParseAddr("192.168.1.20")
	c := cache{}
	var full dnsmessage.Message
	if err := full.Unpack(advertisement(t, sender.As4(), "eSCL")); err != nil {
		t.Fatal(err)
	}
	ptr := full
	ptr.Additionals = nil
	raw, _ := ptr.Pack()
	c.ingest(raw, sender, now)
	queried := map[string]bool{}
	questions := c.questions(sender, queried, now)
	if len(questions) != 2 {
		t.Fatal("missing SRV/TXT queries", questions)
	}
	if len(c.questions(sender, queried, now)) != 0 {
		t.Fatal("repeated queries")
	}
	srv := full
	srv.Answers = nil
	srv.Additionals = full.Additionals[:2]
	raw, _ = srv.Pack()
	c.ingest(raw, sender, now)
	if questions = c.questions(sender, queried, now); len(questions) != 1 || questions[0].Type != dnsmessage.TypeA {
		t.Fatal("missing address query", questions)
	}
	c.ingest(advertisement(t, sender.As4(), "eSCL"), sender, now)
	if len(c.candidates(now)) != 1 {
		t.Fatal("split advertisement incomplete")
	}
}
