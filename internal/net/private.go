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

	// IPv4
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsPrivate() || ip4.IsLoopback() {
			return true
		}
	}

	// IPv6
	if ip.IsPrivate() || ip.IsLoopback() {
		return true
	}
	// Link-local: fe80::/10
	if ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}

// IsPrivateNetworkFallback resolves the hostname and checks if the IP is private.
func IsPrivateNetworkFallback(h string) bool {
	// Attempt to resolve hostname to IP addresses.
	ips, err := net.LookupIP(h)
	if err == nil { // If err IS nil.
		for _, ip := range ips {
			if IsPrivateIP(ip.String(), h) {
				return true
			}
		}
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
		}
	}
	return false
}

// IsPrivateIP checks if a given IP is in the private range.
func IsPrivateIP(ip, h string) bool {
	var isPrivate bool

	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		octets := make([]int, 4)
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				return false
			}
			octets[i] = n
		}

		switch octets[0] {
		case 192:
			if octets[1] == 168 {
				isPrivate = true
			}
		case 172:
			if octets[1] >= 16 && octets[1] <= 31 {
				isPrivate = true
			}
		case 10, 127:
			isPrivate = true
		}
	}

	if isPrivate {
		logger.Pl.I("Host %q resolved to private IP address %q.", h, ip)
		return true
	}

	logger.Pl.I("Host %q resolved to public IP address %q.", h, ip)
	return false
}
