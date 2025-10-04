package process

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/progflags"
	"tubarr/internal/utils/files"
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

	if len(m) == 0 {
		return false, nil
	}

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

	// Extract description
	if description, ok := m["description"].(string); ok {
		v.Description = description
		logging.D(2, "Extracted description from metadata")
	}

	logging.D(1, "Successfully validated and stored metadata for video: %s (Title: %s)", v.URL, v.Title)
	return true, nil
}

// checkMoveOps checks if Metarr should use an output directory based on existent metadata.
func checkMoveOps(v *models.Video, dirParser *parsing.Directory) (outputDir string, channelURL string) {
	// Load move ops from file if present
	if v.Settings.MoveOpFile != "" {
		v.Settings.MoveOps = append(v.Settings.MoveOps, loadMoveOpsFromFile(v, dirParser)...)
	}

	for _, op := range v.Settings.MoveOps {
		if raw, exists := v.MetadataMap[op.Field]; exists {
			// Convert any type to string
			val := fmt.Sprint(raw)

			if strings.Contains(strings.ToLower(val), strings.ToLower(op.Value)) {
				logging.I("Move op filters matched: Field %q contains the value %q. Output directory retrieved as %q", op.Field, op.Value, op.OutputDir)
				return op.OutputDir, op.ChannelURL
			}
		}
	}
	return "", ""
}

// filterRequests uses user input filters to check if the video should be downloaded.
func filterRequests(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.Directory) (valid bool, err error) {

	// Load filter ops from file if present
	v.Settings.Filters = append(v.Settings.Filters, loadFilterOpsFromFile(v, dirParser)...)

	// Check filter ops
	passFilterOps, err := filterOpsFilter(v, cu)
	if err != nil {
		return false, err
	}
	if !passFilterOps {
		return false, nil
	}

	// Upload date filter
	passUploadDate, err := uploadDateFilter(v)
	if err != nil {
		return false, err
	}
	if !passUploadDate {
		return false, nil
	}

	logging.I("Video %q for channel %q passed filter checks", v.URL, c.Name)
	return true, nil
}

// filterOpsFilter determines whether a video should be filtered out based on metadata it contains or omits.
func filterOpsFilter(v *models.Video, cu *models.ChannelURL) (bool, error) {
	mustTotal, mustPassed := 0, 0
	anyTotal, anyPassed := 0, 0

	for _, filter := range v.Settings.Filters {

		// Skip filter if it is associated with a channel URL, and that URL does not match the channel URL associated with the video.
		if filter.ChannelURL != "" && (!strings.EqualFold(strings.TrimSpace(filter.ChannelURL), strings.TrimSpace(cu.URL))) {
			logging.D(2, "Skipping filter %v. This filter's channel URL does not match video (%q)'s associated channel URL %q", filter, v.URL, cu.URL)
			continue
		}

		switch filter.MustAny {
		case "must":
			mustTotal++
		case "any":
			anyTotal++
		}

		val, exists := v.MetadataMap[filter.Field]
		strVal := strings.ToLower(fmt.Sprint(val))
		filterVal := strings.ToLower(filter.Value)

		passed, failHard := false, false

		switch filter.Value {
		case "": // empty filter value
			passed, failHard = checkFilterWithEmptyValue(filter, exists)
		default: // non-empty filter value
			passed, failHard = checkFilterWithValue(filter, strVal, filterVal) // Treats non-existent and empty metadata fields the same...
		}

		if failHard {
			if err := removeUnwantedJSON(v.JSONPath); err != nil {
				logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
			}
			return false, nil
		}

		if passed {
			switch filter.MustAny {
			case "must":
				mustPassed++
			case "any":
				anyPassed++
			}
		}
	}

	// Tally checks
	if mustPassed != mustTotal {
		return false, nil
	}
	if anyTotal > 0 && anyPassed == 0 && mustPassed == 0 {
		return false, nil
	}

	if len(v.Settings.Filters) > 0 {
		logging.I("Video passed download filter checks: %v", v.Settings.Filters)
	}
	return true, nil
}

// checkFilterWithEmptyValue checks a filter's empty value against its matching metadata field.
func checkFilterWithEmptyValue(filter models.DLFilters, exists bool) (passed, failHard bool) {
	switch filter.Type {
	case consts.FilterContains:
		if !exists {
			logging.I("Filtering: field %q not found and must contain it", filter.Field)
			return false, true
		}
		return true, false
	case consts.FilterOmits:
		if exists && filter.MustAny == "must" {
			logging.I("Filtering: field %q exists but must omit it", filter.Field)
			return false, true
		}
		return !exists, false
	}
	return false, false
}

// checkFilterWithValue checks a filter's value against its matching metadata field.
func checkFilterWithValue(filter models.DLFilters, strVal, filterVal string) (passed, failHard bool) {
	switch filter.Type {
	case consts.FilterContains:
		if strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == "must" {
			logging.I("Filtering out: does not contain %q in %q", filter.Value, filter.Field)
			return false, true
		}
	case consts.FilterOmits:
		if !strings.Contains(strVal, filterVal) {
			return true, false
		}
		if filter.MustAny == "must" {
			logging.I("Filtering out: contains %q in %q", filter.Value, filter.Field)
			return false, true
		}
	}
	return false, false
}

// uploadDateFilter filters a video based on its upload date.
func uploadDateFilter(v *models.Video) (bool, error) {
	if !v.UploadDate.IsZero() {
		uploadDateNum, err := strconv.Atoi(v.UploadDate.Format("20060102"))
		if err != nil {
			return false, fmt.Errorf("failed to convert upload date to integer: %w", err)
		}

		if v.Settings.FromDate != "" {
			fromDate, err := strconv.Atoi(v.Settings.FromDate)
			if err != nil {
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, fmt.Errorf("invalid 'from date' format: %w", err)
			}
			if uploadDateNum < fromDate {
				logging.I("Filtering out %q: uploaded on \"%d\", before 'from date' %q", v.URL, uploadDateNum, v.Settings.FromDate)
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, nil
			} else {
				logging.D(1, "URL %q passed 'from date' (%q) filter, upload date is \"%d\"", v.URL, v.Settings.FromDate, uploadDateNum)
			}
		}

		if v.Settings.ToDate != "" {
			toDate, err := strconv.Atoi(v.Settings.ToDate)
			if err != nil {
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, fmt.Errorf("invalid 'to date' format: %w", err)
			}
			if uploadDateNum > toDate {
				logging.I("Filtering out %q: uploaded on \"%d\", after 'to date' %q", v.URL, uploadDateNum, v.Settings.ToDate)
				if err := removeUnwantedJSON(v.JSONPath); err != nil {
					logging.E(0, "Failed to remove unwanted JSON at %q: %v", v.JSONPath, err)
				}
				return false, nil
			} else {
				logging.D(1, "URL %q passed 'to date' (%q) filter, upload date is \"%d\"", v.URL, v.Settings.FromDate, uploadDateNum)
			}
		}
	} else {
		logging.D(1, "Did not parse an upload date from the video %q, skipped applying to/from filters", v.URL)
	}
	return true, nil
}

// loadFilterOpsFromFile loads filter operations from a file (one per line).
func loadFilterOpsFromFile(v *models.Video, p *parsing.Directory) []models.DLFilters {
	var err error

	if v.Settings.FilterFile == "" {
		return nil
	}

	filterFile := v.Settings.FilterFile

	if filterFile, err = p.ParseDirectory(filterFile, v, "filter-ops"); err != nil {
		logging.E(0, "Failed to parse directory %q: %v", filterFile, err)
		return nil
	}

	logging.I("Adding filters from file %q...", filterFile)
	filters, err := files.ReadFileLines(filterFile)
	if err != nil {
		logging.E(0, "Error loading filters from file %q: %v", filterFile, err)
	}

	if len(filters) == 0 {
		logging.I("No valid filters found in file. Format is one per line 'title:contains:dogs:must' (Only download videos with 'dogs' in the title)")
		return nil
	}

	validFilters, err := validation.ValidateFilterOps(filters)
	if err != nil {
		logging.E(0, "Error loading filters from file %v: %v", filterFile, err)
	}
	if len(validFilters) > 0 {
		logging.D(1, "Found following filters in file:\n\n%v", validFilters)
	}

	return validFilters
}

// loadMoveOpsFromFile loads move operations from a file (one per line).
func loadMoveOpsFromFile(v *models.Video, p *parsing.Directory) []models.MoveOps {
	var err error

	if v.Settings.MoveOpFile == "" {
		return nil
	}

	moveOpFile := v.Settings.MoveOpFile

	if moveOpFile, err = p.ParseDirectory(moveOpFile, v, "move-ops"); err != nil {
		logging.E(0, "Failed to parse directory %q: %v", moveOpFile, err)
		return nil
	}

	logging.I("Adding filter move operations from file %q...", moveOpFile)
	moves, err := files.ReadFileLines(moveOpFile)
	if err != nil {
		logging.E(0, "Error loading filter move operations from file %q: %v", moveOpFile, err)
	}

	if len(moves) == 0 {
		logging.I("No valid filter move operations found in file. Format is one per line 'title:dogs:/home/dogs' (Metarr outputs files with 'dogs' in the title to '/home/dogs)")
	}

	validMoves, err := validation.ValidateMoveOps(moves)
	if err != nil {
		logging.E(0, "Error loading filter move operations from file %q: %v", moveOpFile, err)
	}
	if len(validMoves) > 0 {
		logging.D(1, "Found following filter move operations in file:\n\n%v", validMoves)
		v.Settings.MoveOps = append(v.Settings.MoveOps, validMoves...)
	}

	return validMoves
}

// notifyURLs notifies URLs set for the channel by the user.
func notifyURLs(cs interfaces.ChannelStore, c *models.Channel) error {
	// Some successful downloads, notify URLs
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		logging.D(1, "No notification URL for channel with name %q and ID: %d", c.Name, c.ID)
	}

	if len(notifyURLs) > 0 {
		if errs := notify(c, notifyURLs); len(errs) != 0 {
			return fmt.Errorf("errors sending notifications for channel with ID %d:\n%w", c.ID, errors.Join(errs...))
		}
	}

	return nil
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

// checkCustomScraperNeeds checks if a custom scraper should be used for this release.
func checkCustomScraperNeeds(v *models.Video) error {
	// Check for custom scraper needs

	// Detect censored.tv links
	if strings.Contains(v.URL, "censored.tv") {
		if !progflags.CensoredTVUseCustom {
			logging.I("Using regular censored.tv scraper...")
		} else {
			logging.I("Detected a censored.tv link. Using specialized scraper.")
			err := browserInstance.ScrapeCensoredTVMetadata(v.URL, v.ParsedJSONDir, v)
			if err != nil {
				return fmt.Errorf("failed to scrape censored.tv metadata: %w", err)
			}
		}
	}
	return nil
}

// parseVideoJSONDirs parses video and JSON directories.
func parseVideoJSONDirs(v *models.Video, dirParser *parsing.Directory) (jsonDir, videoDir string) {
	// Initialize directory parser
	var (
		cSettings = v.Settings
		err       error
	)

	if strings.Contains(cSettings.JSONDir, "{") || strings.Contains(cSettings.VideoDir, "{") {

		jsonDir, err = dirParser.ParseDirectory(cSettings.JSONDir, v, "JSON")
		if err != nil {
			logging.E(0, "Failed to parse JSON directory %q", cSettings.JSONDir)
		}
		videoDir, err = dirParser.ParseDirectory(cSettings.VideoDir, v, "video")
		if err != nil {
			logging.E(0, "Failed to parse video directory %q", cSettings.VideoDir)
		}
	}

	return jsonDir, videoDir
}
