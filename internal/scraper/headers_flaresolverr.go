package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"

	fhttp "github.com/bogdanfinn/fhttp"
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
	logger.Pl.I("Requesting FlareSolverr's browser headers from %q...", flareSolverrHeaderEchoURL)
	fsResp, err := flareSolverrGet(ctx, baseURL, flareSolverrHeaderEchoURL, consts.FlareSolverrDefaultTimeout)
	if err != nil {
		return nil, err
	}

	// Parse the headers from the header echo page's response, as rendered by a browser.
	headers, err := parseHeaderEcho(fsResp.Solution.Response)
	if err != nil {
		return nil, err
	}

	// Log the copied headers.
	logger.Pl.I("Got FlareSolverr's browser headers from FlareSolverr at %q:\n\n%v", baseURL, headers)
	return headers, nil
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

// setFlareSolverrHeaders adds headers copied from FlareSolverr's browser request, in the browser's order.
// Headers already set (e.g. user agent and cookies) are kept, except Colly's default "Accept: */*".
func setFlareSolverrHeaders(h fhttp.Header, browser [][2]string) {
	if h.Get("Accept") == "*/*" {
		h.Del("Accept")
	}

	order := []string{"host"}
	for _, kv := range browser {
		if h.Get(kv[0]) == "" {
			h.Set(kv[0], kv[1])
		}
		if kv[0] == "priority" {
			order = append(order, "cookie") // Chrome sends cookies just before priority.
		}
		order = append(order, kv[0])
	}
	if !slices.Contains(order, "cookie") {
		order = append(order, "cookie")
	}
	h[fhttp.HeaderOrderKey] = order
}
