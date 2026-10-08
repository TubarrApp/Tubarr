package scraper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"tubarr/internal/domain/logger"
)

const (
	// flareSolverrMaxTimeout is how long FlareSolverr may spend solving a challenge and loading a page.
	flareSolverrMaxTimeout = 60 * time.Second
	// flareSolverrRequestTimeout leaves headroom over flareSolverrMaxTimeout for FlareSolverr to respond.
	flareSolverrRequestTimeout = flareSolverrMaxTimeout + 30*time.Second
)

// flareSolverrRoundTripper fetches pages through a FlareSolverr instance, which loads them in a real browser
// to get past Cloudflare challenges.
type flareSolverrRoundTripper struct {
	endpoint string
	client   *http.Client
}

// flareSolverrRequest is the body of a FlareSolverr API request.
type flareSolverrRequest struct {
	Cmd        string               `json:"cmd"`
	URL        string               `json:"url"`
	MaxTimeout int64                `json:"maxTimeout"`
	Cookies    []flareSolverrCookie `json:"cookies,omitempty"`
}

// flareSolverrCookie is a cookie sent to FlareSolverr's browser.
type flareSolverrCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// flareSolverrResponse is the relevant part of a FlareSolverr API response.
type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL      string `json:"url"`
		Status   int    `json:"status"`
		Response string `json:"response"`
	} `json:"solution"`
}

// newFlareSolverrRoundTripper returns a round tripper for the FlareSolverr instance at baseURL.
func newFlareSolverrRoundTripper(baseURL string) (*flareSolverrRoundTripper, error) {
	endpoint, err := flareSolverrEndpoint(baseURL)
	if err != nil {
		return nil, err
	}
	return &flareSolverrRoundTripper{endpoint: endpoint, client: &http.Client{}}, nil
}

// ValidateFlareSolverrURL checks a FlareSolverr base URL is a usable http(s) URL.
func ValidateFlareSolverrURL(baseURL string) error {
	_, err := flareSolverrEndpoint(baseURL)
	return err
}

// flareSolverrEndpoint validates a FlareSolverr base URL and returns its API endpoint.
func flareSolverrEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid FlareSolverr URL %q (expected e.g. http://localhost:8191)", baseURL)
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	return baseURL, nil
}

// RoundTrip asks FlareSolverr to load the requested page, and returns the page it loaded as the response.
func (t *flareSolverrRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, fmt.Errorf("FlareSolverr only supports GET requests, got %s for %q", req.Method, req.URL)
	}

	logger.Pl.I("Requesting %q through FlareSolverr at %q...", req.URL, t.endpoint)

	body := flareSolverrRequest{
		Cmd:        "request.get",
		URL:        req.URL.String(),
		MaxTimeout: flareSolverrMaxTimeout.Milliseconds(),
	}
	for _, c := range req.Cookies() {
		body.Cookies = append(body.Cookies, flareSolverrCookie{Name: c.Name, Value: c.Value})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to build FlareSolverr request: %w", err)
	}

	fsReq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to build FlareSolverr request: %w", err)
	}
	fsReq.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(fsReq)
	if err != nil {
		return nil, fmt.Errorf("FlareSolverr request to %q failed: %w", t.endpoint, err)
	}
	defer resp.Body.Close()

	var fsResp flareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return nil, fmt.Errorf("failed to decode FlareSolverr response (HTTP %d): %w", resp.StatusCode, err)
	}
	if fsResp.Status != "ok" {
		return nil, fmt.Errorf("FlareSolverr could not load %q: %s", req.URL, fsResp.Message)
	}

	logger.Pl.I("FlareSolverr loaded %q with status %d", req.URL, fsResp.Solution.Status)

	status := fsResp.Solution.Status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body:          io.NopCloser(strings.NewReader(fsResp.Solution.Response)),
		ContentLength: int64(len(fsResp.Solution.Response)),
		Request:       req,
	}, nil
}

// TestFlareSolverr loads a simple page through the FlareSolverr instance at baseURL, to check it works.
func TestFlareSolverr(baseURL string) (status int, elapsed time.Duration, err error) {
	rt, err := newFlareSolverrRoundTripper(baseURL)
	if err != nil {
		return 0, 0, err
	}

	start := time.Now()
	client := &http.Client{Transport: rt, Timeout: flareSolverrRequestTimeout}
	resp, err := client.Get("https://example.com")
	if err != nil {
		return 0, time.Since(start), err
	}
	defer resp.Body.Close()
	return resp.StatusCode, time.Since(start), nil
}
