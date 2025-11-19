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
		// Class A: 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// Class B: 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// Class C: 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// Localhost: 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
	}

	// IPv6
	// Unique Local Address (ULA): fc00::/7
	if ip[0] >= 0xfc && ip[0] <= 0xfd {
		return true
	}
	// Link-local: fe80::/10
	if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	// Localhost: ::1
	if ip.Equal(net.IPv6loopback) {
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

	// If resolution fails, check if the input is a direct IP address.
	parts := strings.Split(h, ".")
	if len(parts) == 4 {
		octets := make([]int, 4)
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				logger.Pl.E("Malformed IP string %q", h)
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

	logger.Pl.E("Failed to resolve hostname %q", h)
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
