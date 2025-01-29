package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// parseAndStoreJSON checks if the JSON is valid and if it passes filter checks.
func parseAndStoreJSON(v *models.Video) (valid bool, err error) {
	f, err := os.Open(v.JSONPath)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logging.E(0, "Failed to close file at %q", v.JSONPath)
		}
	}()

	logging.D(1, "About to decode JSON to metamap")

	m := make(map[string]any)
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&m); err != nil {
		return false, fmt.Errorf("failed to decode JSON: %w", err)
	}

	if len(m) > 0 {
		v.MetadataMap = m

		// Extract title from metadata
		if title, ok := m["title"].(string); ok {
			v.Title = title
			logging.D(2, "Extracted title from metadata: %s", title)
		} else {
			logging.D(2, "No title found in metadata or invalid type")
		}

		// Extract upload date if available
		if uploadDate, ok := m["upload_date"].(string); ok {
			if t, err := time.Parse("20060102", uploadDate); err == nil { // If error IS nil
				v.UploadDate = t
				logging.D(2, "Extracted upload date: %s", t.Format("2006-01-02"))
			} else {
				logging.D(2, "Failed to parse upload date %q: %v", uploadDate, err)
			}
		}

		// Extract any additional metadata fields you want to store
		if description, ok := m["description"].(string); ok {
			v.Description = description
			logging.D(2, "Extracted description from metadata")
		}

	} else {
		return false, nil
	}

	if valid, err = filterRequests(v); err != nil {
		return false, err
	} else if !valid {
		return false, nil
	}

	logging.D(1, "Successfully validated and stored metadata for video: %s (Title: %s)", v.URL, v.Title)
	return true, nil
}

// filterRequests uses user input filters to check if the video should be downloaded.
func filterRequests(v *models.Video) (valid bool, err error) {
	// Check if filters are set and validate if so
	if len(v.Settings.Filters) == 0 {
		logging.D(2, "No filters to check for %q", v.URL)
		return true, nil
	}

	// Apply filters if any match metadata content
	for _, filter := range v.Settings.Filters {
		val, exists := v.MetadataMap[filter.Field]

		if filter.Value == "" {
			if !exists {
				switch filter.Type {
				case consts.FilterContains:

					logging.I("Filtering: Field %q not found in metadata for URL %q and filter is set to require it, filtering out", filter.Field, v.URL)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %s: %v", v.JSONPath, err.Error())
					}
					return false, nil

				case consts.FilterOmit:
					logging.D(2, "Passed check: Field %q does not exist", filter.Field)
					continue
				}
			}
			if exists {
				switch filter.Type {
				case consts.FilterOmit:

					logging.I("Filtering: Field %q found in metadata for URL %q and filter is set to omit it, filtering out", filter.Field, v.URL)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
					}
					return false, nil

				case consts.FilterContains:
					logging.D(2, "Passed check: Field %q exists", filter.Field)
					continue
				}
			}
		}

		// Performing field contents checks
		if filter.Value != "" {

			strVal, ok := val.(string)
			if !ok {
				logging.E(0, "Unexpected type for field %s: expected string, got %T", filter.Field, val)
				continue
			}

			lowerStrVal := strings.ToLower(strVal)
			lowerFilterVal := strings.ToLower(filter.Value)

			// Apply the filter logic
			switch filter.Type {
			case consts.FilterOmit:
				if strings.Contains(lowerStrVal, lowerFilterVal) {

					logging.D(1, "Filtering out video %q which contains %q in field %q", v.URL, filter.Value, filter.Field)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
					}
					return false, nil
				}

			case consts.FilterContains:
				if !strings.Contains(lowerStrVal, lowerFilterVal) {

					logging.D(1, "Filtering out video %q which does not contain %q in field %q", v.URL, filter.Value, filter.Field)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
					}
					return false, nil
				}

			default:
				logging.D(1, "Unrecognized filter type, skipping...")
				continue
			}
		}
	}
	logging.D(1, "Video %q passed filter checks", v.URL)
	return true, nil
}

// removeUnwantedJSON removes filtered out JSON files.
func removeUnwantedJSON(path string) error {
	if path == "" {
		return errors.New("path sent in empty, not removing")
	}

	check, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("not deleting unwanted JSON file, got error: %w", err)
	}

	switch {
	case check.IsDir():
		return fmt.Errorf("JSON path sent in as a directory %q, not deleting", path)
	case !check.Mode().IsRegular():
		return fmt.Errorf("JSON file %q is not a regular file, not deleting", path)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove unwanted JSON file %q due to error %w", path, err)
	} else {
		logging.S(0, "Removed unwanted JSON file %q", path)
	}

	return nil
}

// isPrivateNetwork returns true if the URL is detected as a LAN network.
func isPrivateNetwork(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		if u, err := url.Parse(host); err == nil { // err IS nil
			h = u.Hostname()
		} else {
			parts := strings.Split(host, ":")
			if _, afterProto, found := strings.Cut(parts[0], "//"); found {
				h = afterProto
			} else {
				h = parts[0]
			}
		}
	}

	if h == "localhost" {
		return true
	}

	ip := net.ParseIP(h)
	if ip == nil {
		return isPrivateNetworkFallback(h)
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

// isPrivateNetworkFallback resolves the hostname and checks if the IP is private.
func isPrivateNetworkFallback(h string) bool {
	// Attempt to resolve hostname to IP addresses
	ips, err := net.LookupIP(h)
	if err == nil {
		// Iterate through resolved IPs and check if any are private
		for _, ip := range ips {
			if isPrivateIP(ip.String(), h) {
				return true
			}
		}
		return false
	}

	// If resolution fails, check if the input is a direct IP address
	parts := strings.Split(h, ".")
	if len(parts) == 4 {
		octets := make([]int, 4)
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n > 255 {
				logging.E(0, "Malformed IP string %q", h)
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

	logging.E(0, "Failed to resolve hostname %q", h)
	return false
}

// isPrivateIP checks if a given IP is in the private range.
func isPrivateIP(ip, h string) bool {
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
		logging.I("Host %q resolved to private IP address %q.", h, ip)
		return true
	}

	logging.I("Host %q resolved to public IP address %q.", h, ip)
	return false
}
