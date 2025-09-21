package process

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	cfgchannel "tubarr/internal/cfg/channel"
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
	// Apply filters if any match metadata content

	if v.Channel.Settings.FilterFile != "" {
		logging.I("Adding filters from file %v...", v.Channel.Settings.FilterFile)
		filters, err := loadFiltersFromFile(v.Channel.Settings.FilterFile)
		if err != nil {
			logging.E(0, "Error loading filters from file %v: %v", v.Channel.Settings.FilterFile, err)
		}

		if len(filters) > 0 {
			validFilters, err := cfgchannel.VerifyChannelOps(filters)
			if err != nil {
				logging.E(0, "Error loading filters from file %v: %v", v.Channel.Settings.FilterFile, err)
			}
			if len(validFilters) > 0 {
				logging.D(1, "Found following filters in file:\n\n%v", validFilters)
				v.Settings.Filters = append(v.Settings.Filters, validFilters...)
			}
		} else {
			logging.I("No valid filters found in file. Format is one per line 'field:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		}
	}

	mustTotal := 0
	mustPassed := 0
	anyTotal := 0
	anyPassed := 0

	for _, filter := range v.Settings.Filters {

		switch filter.MustAny {
		case "must":
			mustTotal++
		case "any":
			anyTotal++
		}

		val, exists := v.MetadataMap[filter.Field]

		// Empty field logic
		if filter.Value == "" {
			if !exists {
				switch filter.Type {
				case consts.FilterContains:

					logging.I("Filtering: Field %q not found in metadata for URL %q and filter is set to require it, filtering out", filter.Field, v.URL)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %s: %v", v.JSONPath, err.Error())
					}
					return false, nil // Empty field cannot contain any contain filter, return here.

				case consts.FilterOmit:
					logging.D(2, "Passed check: Field %q does not exist", filter.Field)

					switch filter.MustAny {
					case "must":
						mustPassed++
						logging.D(3, "'Must' filter %v passed for field %v", filter.Value, val)
					case "any":
						anyPassed++
						logging.D(3, "'Any' filter %v passed for field %v", filter.Value, val)
					}

					continue
				}
			}

			if exists {
				switch filter.Type {
				case consts.FilterOmit:

					switch filter.MustAny {

					case "must":
						logging.I("Filtering: Field %q found in metadata for URL %q and filter is set to omit it, filtering out", filter.Field, v.URL)
						if err := removeUnwantedJSON(v.JSONPath); err != nil {
							logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
						}
						return false, nil // Must omit but contains the field
					case "any":
						continue
					}

				case consts.FilterContains:
					logging.D(2, "Passed check: Field %q exists", filter.Field)

					switch filter.MustAny {
					case "must":
						mustPassed++
						logging.D(3, "'Must' filter %v passed for field %v", filter.Value, val)
					case "any":
						anyPassed++
						logging.D(3, "'Any' filter %v passed for field %v", filter.Value, val)
					}

					continue
				}
			}
		}

		// Non-empty field logic
		if filter.Value != "" {

			passed := false

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

				if !strings.Contains(lowerStrVal, lowerFilterVal) {
					passed = true
				} else if filter.MustAny == "must" {
					// Fail hard for must
					logging.D(1, "Filtering out video %q which contains %q in field %q", v.URL, filter.Value, filter.Field)
					if err := removeUnwantedJSON(v.JSONPath); err != nil {
						logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
					}
					return false, nil
				}

			case consts.FilterContains:

				if strings.Contains(lowerStrVal, lowerFilterVal) {
					passed = true
				} else if filter.MustAny == "must" {
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

			if passed {
				switch filter.MustAny {
				case "must":
					mustPassed++
					logging.D(3, "'Must' filter %v passed for field %v", filter.Value, val)
				case "any":
					anyPassed++
					logging.D(3, "'Any' filter %v passed for field %v", filter.Value, val)
				}
			}
		}
	}

	// Some "must" filters failed
	if mustPassed != mustTotal {
		logging.D(1, "Filtering out video %q which does not meet 'must contain' threshold (number of 'musts': %d 'musts' found: %d)", v.URL, mustTotal, mustPassed)
		return false, nil
	}

	// No "any" filters passed and no "must" filters passed
	if anyTotal > 0 && anyPassed == 0 && mustPassed == 0 {
		logging.D(1, "Filtering out video %q which does not meet any filter threshold.\n\nNumber of 'anys': %d\n'Anys' found: %d\n\nNumber of 'musts': %d\n'Musts' found: %d\n\n", v.URL, anyTotal, anyPassed, mustTotal, mustPassed)
		return false, nil
	}

	if len(v.Settings.Filters) > 0 {
		logging.I("Video passed download filter checks: %v", v.Settings.Filters)
	}

	logging.D(3, "Filter tally:\n\nNumber of 'anys': %d\n'Anys' found: %d\n\nNumber of 'musts': %d\n'Musts' found: %d\n\n", anyTotal, anyPassed, mustTotal, mustPassed)

	// Other filters
	// Upload date filter
	if !v.UploadDate.IsZero() {
		uploadDateNum, err := strconv.Atoi(v.UploadDate.Format("20060102"))
		if err != nil {
			return false, fmt.Errorf("failed to convert upload date to integer: %w", err)
		}

		if v.Channel.Settings.FromDate != "" {
			fromDate, err := strconv.Atoi(v.Channel.Settings.FromDate)
			if err != nil {
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, fmt.Errorf("invalid 'from date' format: %w", err)
			}
			if uploadDateNum < fromDate {
				logging.I("Filtering out %q: uploaded on \"%d\", before 'from date' %q", v.URL, uploadDateNum, v.Channel.Settings.FromDate)
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, nil
			} else {
				logging.D(1, "URL %q passed 'from date' (%q) filter, upload date is \"%d\"", v.URL, v.Channel.Settings.FromDate, uploadDateNum)
			}
		} else {
			logging.D(1, "No 'from date' grabbed for channel %q for URL %q", v.Channel.Name, v.URL)
		}

		if v.Channel.Settings.ToDate != "" {
			toDate, err := strconv.Atoi(v.Channel.Settings.ToDate)
			if err != nil {
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, fmt.Errorf("invalid 'to date' format: %w", err)
			}
			if uploadDateNum > toDate {
				logging.I("Filtering out %q: uploaded on \"%d\", after 'to date' %q", v.URL, uploadDateNum, v.Channel.Settings.ToDate)
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, nil
			} else {
				logging.D(1, "URL %q passed 'to date' (%q) filter, upload date is \"%d\"", v.URL, v.Channel.Settings.FromDate, uploadDateNum)
			}
		} else {
			logging.D(1, "No 'to date' grabbed for channel %q for URL %q", v.Channel.Name, v.URL)
		}
	} else {
		logging.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
	}

	logging.I("Video %q for channel %q passed filter checks", v.URL, v.Channel.Name)
	return true, nil
}

// loadFiltersFromFile loads filters from a file (one per line).
func loadFiltersFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	filters := []string{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip blank lines and comments
		}
		filters = append(filters, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return filters, nil
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
