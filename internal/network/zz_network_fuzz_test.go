package network

import (
	"io"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"tubarr/internal/domain/logger"
)

// ******************************** Domains *******************************************************************************************

// TestMain sets up the test environment for the net package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestHostname tests a URL's host comes back lowercase without its port, and a URL that can't be parsed gives "".
func TestHostname(t *testing.T) {
	for in, want := range map[string]string{
		"https://WWW.Example.COM/v/1": "www.example.com",
		"http://Example.com:8080/x":   "example.com",
		"https://[::1]:443/":          "::1",
		"://no-scheme":                "",
	} {
		if got := Hostname(in); got != want {
			t.Errorf("Hostname(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeDomain tests that hostnames and URLs as users enter them reduce to the domain limits are looked up by.
func TestNormalizeDomain(t *testing.T) {
	tests := map[string]string{
		"youtube.com":                      "youtube.com",
		"www.YouTube.com":                  "youtube.com",
		" m.youtube.com ":                  "youtube.com",
		"https://www.youtube.com/@channel": "youtube.com",
		"www.bbc.co.uk/iplayer":            "bbc.co.uk",
		"WWW.EXAMPLE.CO.UK":                "example.co.uk", // Uppercase suffixes must still match the list.
		"https://Example.Co.Uk/video":      "example.co.uk",
	}
	for in, want := range tests {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRegisteredDomain tests hosts reduce to their registered domain whatever their case.
func TestRegisteredDomain(t *testing.T) {
	for in, want := range map[string]string{
		"www.youtube.com":   "youtube.com",
		"WWW.YouTube.COM":   "youtube.com",
		"www.bbc.co.uk":     "bbc.co.uk",
		"WWW.EXAMPLE.CO.UK": "example.co.uk",
	} {
		if got := RegisteredDomain(in); got != want {
			t.Errorf("RegisteredDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// ******************************** Private networks **********************************************************************************

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
