package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/abstractions"
)

const (
	// flareSolverrMaxTimeout is how long FlareSolverr may spend solving a challenge and loading a page.
	flareSolverrMaxTimeout = 60 * time.Second
	// flareSolverrRequestTimeout leaves headroom over flareSolverrMaxTimeout for FlareSolverr to respond.
	flareSolverrRequestTimeout = flareSolverrMaxTimeout + 30*time.Second
)

// flareSolverrSolution holds the cookies and user agent FlareSolverr's browser got past Cloudflare with.
type flareSolverrSolution struct {
	cookies   []*http.Cookie
	userAgent string
	gen       int // Higher for each solve, so stale solutions can be told apart.
}

// flareSolverrRequest is the body of a FlareSolverr API request.
type flareSolverrRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int64  `json:"maxTimeout"`
}

// flareSolverrResponse is the relevant part of a FlareSolverr API response.
type flareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		Status    int    `json:"status"`
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Expires  float64 `json:"expires"`
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
		} `json:"cookies"`
	} `json:"solution"`
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

// solveFlareSolverr has FlareSolverr load pageURL, and returns the cookies and user agent its browser ended up with,
// plus the page's HTTP status.
func solveFlareSolverr(ctx context.Context, baseURL, pageURL string) (sol *flareSolverrSolution, status int, err error) {
	endpoint, err := flareSolverrEndpoint(baseURL)
	if err != nil {
		return nil, 0, err
	}

	payload, err := json.Marshal(flareSolverrRequest{Cmd: "request.get", URL: pageURL, MaxTimeout: flareSolverrMaxTimeout.Milliseconds()})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build FlareSolverr request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, flareSolverrRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build FlareSolverr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("FlareSolverr request to %q failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	var fsResp flareSolverrResponse
	if err := json.NewDecoder(resp.Body).Decode(&fsResp); err != nil {
		return nil, 0, fmt.Errorf("failed to decode FlareSolverr response (HTTP %d): %w", resp.StatusCode, err)
	}
	if fsResp.Status != "ok" {
		return nil, 0, fmt.Errorf("FlareSolverr could not load %q: %s", pageURL, fsResp.Message)
	}
	if fsResp.Solution.UserAgent == "" {
		return nil, 0, fmt.Errorf("FlareSolverr returned no user agent for %q", pageURL)
	}

	sol = &flareSolverrSolution{userAgent: fsResp.Solution.UserAgent}
	for _, c := range fsResp.Solution.Cookies {
		cookie := &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, HttpOnly: c.HTTPOnly, Secure: c.Secure}
		if c.Expires > 0 {
			cookie.Expires = time.Unix(int64(c.Expires), 0)
		}
		sol.cookies = append(sol.cookies, cookie)
	}
	return sol, fsResp.Solution.Status, nil
}

// flareSolverrSolution returns the cached FlareSolverr solution for pageURL's hostname, solving through FlareSolverr
// if there is none, or it is no newer than generation staleGen (0 accepts any cached solution).
//
// Solves against the site's homepage, since Cloudflare's clearance covers the whole site, and FlareSolverr can time out
// on heavier pages (e.g. Rumble channel pages) that the solved cookies get past fine.
//
// A failure is logged once and remembered for the rest of the crawl session, so callers can fall back to normal requests
// without waiting on FlareSolverr again for every page.
func (cm *CookieManager) flareSolverrSolution(ctx context.Context, pageURL string, staleGen int) (*flareSolverrSolution, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", pageURL, err)
	}
	host := u.Hostname()
	homeURL := u.Scheme + "://" + u.Host + "/"

	cm.fsMu.Lock()
	defer cm.fsMu.Unlock()

	if sol, ok := cm.fsSolutions[host]; ok && sol.gen > staleGen {
		return sol, nil
	}
	if err, failed := cm.fsFailures[host]; failed {
		return nil, err
	}

	baseURL := abstractions.GetString(keys.FlareSolverrURL)
	if baseURL == "" {
		return nil, cm.flareSolverrFailed(host, fmt.Errorf("no FlareSolverr URL is set in settings"))
	}

	logger.Pl.I("Solving %q through FlareSolverr at %q...", homeURL, baseURL)
	sol, _, err := solveFlareSolverr(ctx, baseURL, homeURL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err // Cancelled, not a FlareSolverr failure.
		}
		return nil, cm.flareSolverrFailed(host, err)
	}
	cm.fsGen++
	sol.gen = cm.fsGen
	cm.fsSolutions[host] = sol

	logger.Pl.S("FlareSolverr solved %q (%d cookies, user agent %q)", homeURL, len(sol.cookies), sol.userAgent)
	return sol, nil
}

// flareSolverrFailed logs and remembers a FlareSolverr failure for host for the rest of the crawl session.
//
// Must be called with cm.fsMu held.
func (cm *CookieManager) flareSolverrFailed(host string, err error) error {
	err = fmt.Errorf("FlareSolverr unavailable for %q: %w", host, err)
	cm.fsFailures[host] = err
	logger.Pl.W("%v. Falling back to normal requests for %q for the rest of this crawl.", err, host)
	return err
}

// flareSolverrResolver returns a function that solves cu's URL through FlareSolverr again (unless already solved since
// staleGen), and rewrites cu's cookie file with the new cookies on top of baseCookies.
func (cm *CookieManager) flareSolverrResolver(cu *models.ChannelURL, baseCookies []*http.Cookie) func(staleGen int) (string, int, error) {
	pageURL, loginURL, cookiePath := cu.URL, cu.LoginURL, cu.CookiePath
	return func(staleGen int) (string, int, error) {
		sol, err := cm.flareSolverrSolution(context.Background(), pageURL, staleGen)
		if err != nil {
			return "", 0, err
		}
		if err := saveCookiesToFile(mergeCookies(sol.cookies, baseCookies), loginURL, cookiePath); err != nil {
			return "", 0, fmt.Errorf("failed to save FlareSolverr cookies for %q: %w", pageURL, err)
		}
		return sol.userAgent, sol.gen, nil
	}
}

// isCloudflareChallenge reports whether a response is a Cloudflare challenge page.
func isCloudflareChallenge(status int, header http.Header, body []byte) bool {
	if strings.EqualFold(header.Get("Cf-Mitigated"), "challenge") {
		return true
	}
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable {
		return false
	}
	return bytes.Contains(body, []byte("challenge-platform")) || bytes.Contains(body, []byte("Just a moment"))
}

// TestFlareSolverr solves a simple page through the FlareSolverr instance at baseURL, to check it works.
func TestFlareSolverr(baseURL string) (status int, userAgent string, cookies int, elapsed time.Duration, err error) {
	start := time.Now()
	sol, status, err := solveFlareSolverr(context.Background(), baseURL, "https://example.com")
	if err != nil {
		return 0, "", 0, time.Since(start), err
	}
	return status, sol.userAgent, len(sol.cookies), time.Since(start), nil
}
