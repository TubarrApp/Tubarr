// Package net provides networking utilities for Tubarr.
package net

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"tubarr/internal/domain/logger"
)

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
				logger.Pl.I("Host %q resolved to private IP address %q.", h, ip)
				return true
			}
		}
		logger.Pl.I("Host %q resolved to public IP address(es) %v.", h, ips)
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
