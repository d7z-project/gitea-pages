package goja

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	nurl "net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestrictedDialContextDialsValidatedIP(t *testing.T) {
	cfg := FetchConfig{BlockPrivateNetwork: true}
	var gotAddr string
	dial := restrictedDialContext(cfg, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.10")}, nil
	}, func(_ context.Context, _, addr string) (net.Conn, error) {
		gotAddr = addr
		return nil, nil
	})

	conn, err := dial(context.Background(), "tcp", "public.example:443")
	assert.NoError(t, err)
	assert.Nil(t, conn)
	assert.Equal(t, "203.0.113.10:443", gotAddr)
}

func TestRestrictedDialContextFallsBackAcrossPublicIPs(t *testing.T) {
	cfg := FetchConfig{BlockPrivateNetwork: true}
	var addrs []string
	dial := restrictedDialContext(cfg, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("203.0.113.10"),
			netip.MustParseAddr("203.0.113.11"),
		}, nil
	}, func(_ context.Context, _, addr string) (net.Conn, error) {
		addrs = append(addrs, addr)
		if len(addrs) == 1 {
			return nil, errors.New("first failed")
		}
		return nil, nil
	})

	conn, err := dial(context.Background(), "tcp", "public.example:443")
	assert.NoError(t, err)
	assert.Nil(t, conn)
	assert.Equal(t, []string{"203.0.113.10:443", "203.0.113.11:443"}, addrs)
}

func TestRestrictedDialContextRejectsPrivateTargets(t *testing.T) {
	cfg := FetchConfig{BlockPrivateNetwork: true}
	dial := restrictedDialContext(cfg, func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("10.0.0.5"),
			netip.MustParseAddr("127.0.0.1"),
		}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dial should not be called for private targets")
		return nil, nil
	})

	conn, err := dial(context.Background(), "tcp", "internal.example:443")
	assert.Nil(t, conn)
	assert.EqualError(t, err, "fetch target ip is not allowed")
}

func TestRestrictedDialContextPropagatesResolverErrors(t *testing.T) {
	cfg := FetchConfig{BlockPrivateNetwork: true}
	dial := restrictedDialContext(cfg, func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("dns failed")
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dial should not be called when resolution fails")
		return nil, nil
	})

	conn, err := dial(context.Background(), "tcp", "broken.example:443")
	assert.Nil(t, conn)
	assert.EqualError(t, err, "dns failed")
}

func TestNewFetchTransportDisablesProxyWhenBlockingPrivateNetworks(t *testing.T) {
	transport := newFetchTransport(FetchConfig{BlockPrivateNetwork: true})
	proxyURL, err := transport.Proxy(&http.Request{URL: &nurl.URL{Scheme: "https", Host: "example.com"}})
	assert.NoError(t, err)
	assert.Nil(t, proxyURL)
}
