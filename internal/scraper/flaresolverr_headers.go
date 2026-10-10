package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"tubarr/internal/domain/consts"
)

// flareSolverrHeaderEchoURL is a public page that echoes back the request headers it received, in the order sent.
const flareSolverrHeaderEchoURL = "https://tls.peet.ws/api/all"

// preRegex matches the contents of the <pre> element browsers show plain text and JSON in.
var preRegex = regexp.MustCompile(`(?s)<pre[^>]*>(.*?)</pre>`)

// headerEcho is the relevant part of the header echo page's response.
type headerEcho struct {
	HTTP2 struct {
		SentFrames []struct {
			FrameType string   `json:"frame_type"`
			Headers   []string `json:"headers"`
		} `json:"sent_frames"`
	} `json:"http2"`
	HTTP1 struct {
		Headers []string `json:"headers"`
	} `json:"http1"`
}

// flareSolverrBrowserHeaders has FlareSolverr's browser load the header echo page, and returns the headers it sent,
// in order (leaving out ones that differ per request, like the host and cookies).
func flareSolverrBrowserHeaders(ctx context.Context, baseURL string) ([][2]string, error) {
	fsResp, err := flareSolverrGet(ctx, baseURL, flareSolverrHeaderEchoURL, consts.FlareSolverrDefaultTimeout)
	if err != nil {
		return nil, err
	}
	return parseHeaderEcho(fsResp.Solution.Response)
}

// parseHeaderEcho returns the headers in the header echo page's response, as rendered by a browser.
func parseHeaderEcho(page string) ([][2]string, error) {
	body := page
	if m := preRegex.FindStringSubmatch(page); m != nil {
		body = html.UnescapeString(m[1])
	}

	var echo headerEcho
	if err := json.Unmarshal([]byte(body), &echo); err != nil {
		return nil, fmt.Errorf("failed to read headers from %q: %w", flareSolverrHeaderEchoURL, err)
	}

	lines := echo.HTTP1.Headers
	for _, frame := range echo.HTTP2.SentFrames {
		if frame.FrameType == "HEADERS" {
			lines = frame.Headers
			break
		}
	}

	var headers [][2]string
	for _, line := range lines {
		name, value, ok := strings.Cut(line, ": ")
		name = strings.ToLower(strings.TrimSpace(name))
		if !ok || name == "" || strings.HasPrefix(name, ":") {
			continue
		}
		switch name {
		case "host", "cookie", "content-length":
			continue
		}
		headers = append(headers, [2]string{name, value})
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("no headers found in %q's response", flareSolverrHeaderEchoURL)
	}
	return headers, nil
}
