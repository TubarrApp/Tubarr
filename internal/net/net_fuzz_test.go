package net

import (
	"io"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"tubarr/internal/domain/logger"
)

// TestMain sets up the test environment for the net package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// FuzzIsPrivateNetwork tests that IsPrivateNetwork agrees with the standard library's classification for every IP
// address, in each form the notifier passes it (a URL's Host: host, host:port or [ipv6]:port). Only IP addresses are
// used, so no DNS lookups are made.
func FuzzIsPrivateNetwork(f *testing.F) {
	for _, s := range []string{"192.168.1.243", "10.0.0.1", "172.16.5.4", "172.32.0.1", "127.0.0.1", "8.8.8.8", "100.64.0.1",
		"169.254.1.1", "::1", "fe80::1", "fc00::1", "2001:4860::8888", "::ffff:192.168.1.1", "0.0.0.0", "::"} {
		ip := netip.MustParseAddr(s)
		b := ip.As16()
		f.Add(b[:], ip.Is4(), uint16(32400))
	}
	f.Fuzz(func(t *testing.T, raw []byte, v4 bool, port uint16) {
		if len(raw) < 16 {
			return
		}
		var addr netip.Addr
		if v4 {
			addr = netip.AddrFrom4([4]byte(raw[12:16])) // Where As16 puts an IPv4 address.
		} else {
			addr = netip.AddrFrom16([16]byte(raw[:16]))
		}
		u := addr.Unmap()
		// Link-local (169.254/16, fe80::/10) and CGNAT/Tailscale (100.64/10) count as LAN too.
		want := u.IsPrivate() || u.IsLoopback() || u.IsLinkLocalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(u)

		forms := []string{addr.String(), netip.AddrPortFrom(addr, port).String()}
		for _, form := range forms {
			if got := IsPrivateNetwork(form); got != want {
				t.Errorf("IsPrivateNetwork(%q) = %v, want %v", form, got, want)
			}
		}
	})
}

// TestIsPrivateNetworkPortOnlyForms tests a few forms whose ports or zones are unusual.
func TestIsPrivateNetworkPortOnlyForms(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":                          true,
		"localhost:" + "8191":                true,
		"[::1]:32400":                        true,
		"192.168.1.1:" + strconv.Itoa(65535): true,
		"100.101.102.103:32400":              true, // Tailscale.
		"100.128.0.1":                        false,
	} {
		if got := IsPrivateNetwork(host); got != want {
			t.Errorf("IsPrivateNetwork(%q) = %v, want %v", host, got, want)
		}
	}
}
