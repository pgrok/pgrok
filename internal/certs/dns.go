package certs

import (
	"context"
	"net"
	"slices"
	"strconv"
	"sync"

	"github.com/libdns/libdns"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"unknwon.dev/x/logx"
)

const builtinProvider = "builtin"

// builtinDNS is an authoritative DNS server for a zone delegated to pgrokd, and
// a libdns provider that adds and removes the TXT records it serves. It
// answers nothing outside the zone and never recurses.
type builtinDNS struct {
	zone string // Fully qualified and lowercase, e.g. "acme.example.com."

	mu      sync.RWMutex
	records map[string][]string // Fully qualified name to TXT values
}

func newBuiltinDNS(zone string) *builtinDNS {
	return &builtinDNS{
		zone:    dns.CanonicalName(zone),
		records: make(map[string][]string),
	}
}

// start serves DNS on UDP and TCP of the given port until ctx is canceled.
func (s *builtinDNS) start(ctx context.Context, logger *logx.Logger, port int) error {
	address := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	packetConn, err := net.ListenPacket("udp", address)
	if err != nil {
		return errors.Wrap(err, "listen UDP")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		_ = packetConn.Close()
		return errors.Wrap(err, "listen TCP")
	}

	servers := []*dns.Server{
		{PacketConn: packetConn, Handler: s},
		{Listener: listener, Handler: s},
	}
	for _, server := range servers {
		go func() {
			if err := server.ActivateAndServe(); err != nil && ctx.Err() == nil {
				logger.Error("Builtin DNS server exited unexpectedly", "error", err)
			}
		}()
	}
	go func() {
		<-ctx.Done()
		for _, server := range servers {
			_ = server.Shutdown()
		}
	}()
	logger.Info("Builtin DNS server listening on", "address", address, "zone", s.zone)
	return nil
}

func (s *builtinDNS) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	defer func() { _ = w.WriteMsg(resp) }()

	if len(req.Question) != 1 {
		resp.Rcode = dns.RcodeFormatError
		return
	}
	q := req.Question[0]
	name := dns.CanonicalName(q.Name)
	if !dns.IsSubDomain(s.zone, name) {
		resp.Rcode = dns.RcodeRefused
		return
	}
	resp.Authoritative = true

	s.mu.RLock()
	values, exists := slices.Clone(s.records[name]), s.records[name] != nil
	s.mu.RUnlock()

	switch {
	case q.Qtype == dns.TypeTXT && exists:
		for _, value := range values {
			resp.Answer = append(resp.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 1},
				Txt: []string{value},
			})
		}
		return
	case q.Qtype == dns.TypeSOA && name == s.zone:
		resp.Answer = append(resp.Answer, s.soa())
		return
	}

	// No data for the type, or no such name.
	if !exists && name != s.zone {
		resp.Rcode = dns.RcodeNameError
	}
	resp.Ns = append(resp.Ns, s.soa())
}

func (s *builtinDNS) soa() dns.RR {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: s.zone, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 1},
		Ns:      s.zone,
		Mbox:    "hostmaster." + s.zone,
		Serial:  1,
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  1,
	}
}

func (s *builtinDNS) txtRecords(zone string, recs []libdns.Record) ([]string, []libdns.TXT, error) {
	names := make([]string, 0, len(recs))
	txts := make([]libdns.TXT, 0, len(recs))
	for _, rec := range recs {
		rr := rec.RR()
		if rr.Type != "TXT" {
			return nil, nil, errors.Errorf("unsupported record type %q", rr.Type)
		}
		name := dns.CanonicalName(libdns.AbsoluteName(rr.Name, zone))
		if !dns.IsSubDomain(s.zone, name) {
			return nil, nil, errors.Errorf("record %q is outside of the zone %q", name, s.zone)
		}
		names = append(names, name)
		txts = append(txts, libdns.TXT{Name: rr.Name, TTL: rr.TTL, Text: rr.Data})
	}
	return names, txts, nil
}

func (s *builtinDNS) AppendRecords(_ context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	names, txts, err := s.txtRecords(zone, recs)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	appended := make([]libdns.Record, 0, len(txts))
	for i, txt := range txts {
		s.records[names[i]] = append(s.records[names[i]], txt.Text)
		appended = append(appended, txt)
	}
	return appended, nil
}

func (s *builtinDNS) DeleteRecords(_ context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	names, txts, err := s.txtRecords(zone, recs)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var deleted []libdns.Record
	for i, txt := range txts {
		values := s.records[names[i]]
		j := slices.Index(values, txt.Text)
		if j < 0 {
			continue
		}
		values = slices.Delete(values, j, j+1)
		if len(values) == 0 {
			delete(s.records, names[i])
		} else {
			s.records[names[i]] = values
		}
		deleted = append(deleted, txt)
	}
	return deleted, nil
}
