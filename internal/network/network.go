// Package network provides networking utilities for Tubarr.
package network

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"tubarr/internal/domain/logger"

	"golang.org/x/net/publicsuffix"
)

// Hostname returns a URL's hostname in lowercase, or "" if it can't be parsed.
func Hostname(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// Domain returns the eTLD+1 hostname of a URL (e.g. "youtube.com").
func Domain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return RegisteredDomain(parsed.Hostname())
}

// NormalizeDomain returns the eTLD+1 of a hostname or URL as entered by a user (e.g. "www.youtube.com" → "youtube.com").
func NormalizeDomain(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "://") {
		return Domain(s)
	}
	host, _, _ := strings.Cut(s, "/")
	return RegisteredDomain(host)
}

// RegisteredDomain returns the eTLD+1 of a hostname, or the lowercased hostname if it has none.
//
// Lowercases first, as the public suffix list is lowercase ("EXAMPLE.CO.UK" would otherwise give "co.uk").
func RegisteredDomain(hostname string) string {
	hostname = strings.ToLower(hostname)
	if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil {
		return domain
	}
	return hostname
}

// IsPrivateNetwork returns true if the URL is detected as a LAN network.
func IsPrivateNetwork(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		if u, parseErr := url.Parse(host); parseErr == nil && u.Hostname() != "" {
			h = u.Hostname()
		} else {
			h = host // fallback to original input if Hostname() was empty.
		}
	}

	if h == "localhost" {
		return true
	}

	ip := net.ParseIP(h)
	if ip == nil {
		return IsPrivateNetworkFallback(h)
	}
	return isPrivateIP(ip)
}

// cgnatNet is the shared address range (100.64.0.0/10) used by carrier-grade NAT and Tailscale, unreachable from the internet.
var cgnatNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isPrivateIP reports whether an IP is on a private, loopback, link-local or CGNAT/Tailscale network.
func isPrivateIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || cgnatNet.Contains(ip)
}

// IsPrivateNetworkFallback resolves the hostname and checks if the IP is private.
func IsPrivateNetworkFallback(h string) bool {
	// Attempt to resolve hostname to IP addresses.
	ips, err := net.LookupIP(h)
	if err == nil { // If err IS nil.
		for _, ip := range ips {
			if isPrivateIP(ip) {
				logger.Pl.D(3, "Host %q resolved to private IP address %q.", h, ip)
				return true
			}
		}
		logger.Pl.D(3, "Host %q resolved to public IP address(es) %v.", h, ips)
		return false
	}
	logger.Pl.E("Unable to perform DNS lookup for %q: %v", h, err)

	// If resolution fails, check if the input is a direct IP address.
	parts := strings.Split(h, ".")
	if len(parts) == 4 {
		octets := make([]int, 4)
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				logger.Pl.E("%q is not a valid IP string: %v", h, err)
				return false
			}
			octets[i] = n
		}
		switch octets[0] {
		case 192:
			return octets[1] == 168
		case 172:
			return octets[1] >= 16 && octets[1] <= 31
		case 10, 127:
			return true
		case 100:
			return octets[1] >= 64 && octets[1] <= 127 // CGNAT/Tailscale.
		}
	}
	return false
}
