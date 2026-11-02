package engine

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestProviderDialFailsOverPinnedAddresses(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialProviderIPs(ctx, "tcp", port, []net.IPAddr{{IP: net.ParseIP("::1")}, {IP: net.ParseIP("127.0.0.1")}})
	if err != nil {
		t.Fatal("IPv4 fallback after unavailable IPv6 failed", err)
	}
	defer conn.Close()
	peer, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	peer.Close()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if conn, err = dialProviderIPs(ctx, "tcp", port, []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}); !errors.Is(err, context.Canceled) || conn != nil {
		t.Fatal("cancelled fallback connected", conn, err)
	}
	// DNS validation still precedes every attempt, including all fallback IPs.
	if conn, err = (&Store{}).dial(context.Background(), "tcp", l.Addr().String()); err == nil || conn != nil {
		t.Fatal("private address guard bypassed")
	}
}
