package certs

import (
	"context"
	"net"
	"testing"

	"github.com/libdns/libdns"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinDNS(t *testing.T) {
	ctx := context.Background()
	s := newBuiltinDNS("ACME.example.com")

	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &dns.Server{PacketConn: packetConn, Handler: s}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })

	query := func(t *testing.T, name string, qtype uint16) *dns.Msg {
		req := new(dns.Msg)
		req.SetQuestion(name, qtype)
		resp, err := dns.Exchange(req, packetConn.LocalAddr().String())
		require.NoError(t, err)
		return resp
	}
	txts := func(resp *dns.Msg) []string {
		var values []string
		for _, rr := range resp.Answer {
			values = append(values, rr.(*dns.TXT).Txt...)
		}
		return values
	}

	// The challenges of "example.com" and "*.example.com" are both delegated to
	// the same name, and must be served at the same time.
	zone := "acme.example.com."
	for _, value := range []string{"apex-token", "wildcard-token"} {
		_, err := s.AppendRecords(ctx, zone, []libdns.Record{libdns.TXT{Name: "_acme-challenge", Text: value}})
		require.NoError(t, err)
	}

	resp := query(t, "_ACME-challenge.acme.example.com.", dns.TypeTXT)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	assert.True(t, resp.Authoritative)
	assert.ElementsMatch(t, []string{"apex-token", "wildcard-token"}, txts(resp))

	deleted, err := s.DeleteRecords(ctx, zone, []libdns.Record{libdns.TXT{Name: "_acme-challenge", Text: "apex-token"}})
	require.NoError(t, err)
	assert.Len(t, deleted, 1)
	assert.Equal(t, []string{"wildcard-token"}, txts(query(t, "_acme-challenge.acme.example.com.", dns.TypeTXT)))

	_, err = s.DeleteRecords(ctx, zone, []libdns.Record{libdns.TXT{Name: "_acme-challenge", Text: "wildcard-token"}})
	require.NoError(t, err)
	resp = query(t, "_acme-challenge.acme.example.com.", dns.TypeTXT)
	assert.Equal(t, dns.RcodeNameError, resp.Rcode)
	assert.Empty(t, resp.Answer)

	// Finding the zone of the challenge name relies on its SOA record.
	resp = query(t, "acme.example.com.", dns.TypeSOA)
	assert.Equal(t, dns.RcodeSuccess, resp.Rcode)
	require.Len(t, resp.Answer, 1)
	assert.Equal(t, "acme.example.com.", resp.Answer[0].Header().Name)

	// It must not answer, and must not be used to set records, outside the zone.
	assert.Equal(t, dns.RcodeRefused, query(t, "example.com.", dns.TypeSOA).Rcode)
	assert.Equal(t, dns.RcodeRefused, query(t, "_acme-challenge.example.com.", dns.TypeTXT).Rcode)
	_, err = s.AppendRecords(ctx, "example.com.", []libdns.Record{libdns.TXT{Name: "_acme-challenge", Text: "token"}})
	assert.Error(t, err)
}
