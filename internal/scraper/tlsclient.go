package scraper

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// userAgentVersionRegex matches the major version in a Chrome or Firefox user agent.
var userAgentVersionRegex = regexp.MustCompile(`(?:Chrome|Firefox)/(\d+)`)

// tlsRoundTripper adapts a tls-client HttpClient to the net/http RoundTripper interface used by Colly.
type tlsRoundTripper struct {
	client  tlsclient.HttpClient
	browser consts.Impersonate // Browser whose page load headers are sent (Brave and Opera send Chrome's).
	headers [][2]string        // Headers copied from a real browser, sent instead of generated ones (e.g. FlareSolverr's).
}

// newTLSRoundTripper returns a RoundTripper which impersonates the given browser's TLS fingerprint, plus a matching User-Agent.
//
// If userAgent is set (e.g. from FlareSolverr), it is returned as-is, and the profile closest to its browser version is used.
// Redirects and cookies are left to the wrapping net/http client (Colly's), so tls-client neither follows redirects nor keeps a jar.
func newTLSRoundTripper(impersonate consts.Impersonate, userAgent string) (rt *tlsRoundTripper, ua string, err error) {
	profile, version := clientProfile(impersonate, userAgentMajorVersion(userAgent))
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profile),
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithTimeoutSeconds(60),
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create TLS client for impersonation type %q: %w", impersonate, err)
	}
	if userAgent == "" {
		userAgent = browserUserAgent(impersonate, version)
	}
	browser := impersonate
	if browser == consts.ImpersonateBrave || browser == consts.ImpersonateOpera {
		browser = consts.ImpersonateChrome
	}
	return &tlsRoundTripper{client: client, browser: browser}, userAgent, nil
}

// userAgentMajorVersion returns the major browser version in a Chrome or Firefox user agent, or 0 if there is none.
func userAgentMajorVersion(userAgent string) int {
	m := userAgentVersionRegex.FindStringSubmatch(userAgent)
	if m == nil {
		return 0
	}
	major, _ := strconv.Atoi(m[1])
	return major
}

// RoundTrip converts the request to fhttp, sends it through tls-client, and converts the response back.
func (t *tlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	freq, err := fhttp.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, err
	}
	freq.Header = fhttp.Header(req.Header.Clone())
	setRequestHeaders(freq.Header, t.browser, t.headers)
	freq.Host = req.Host
	freq.ContentLength = req.ContentLength

	fresp, err := t.client.Do(freq)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		Status:           fresp.Status,
		StatusCode:       fresp.StatusCode,
		Proto:            fresp.Proto,
		ProtoMajor:       fresp.ProtoMajor,
		ProtoMinor:       fresp.ProtoMinor,
		Header:           http.Header(fresp.Header),
		Body:             fresp.Body,
		ContentLength:    fresp.ContentLength,
		TransferEncoding: fresp.TransferEncoding,
		Uncompressed:     fresp.Uncompressed,
		Request:          req,
	}, nil
}

// clientProfile returns the TLS client profile for the given impersonation type, and its version.
//
// Uses the most recent profile, or if maxMajor is set, the most recent with a major version no higher than it
// (the oldest profile if none are).
func clientProfile(impersonate consts.Impersonate, maxMajor int) (profiles.ClientProfile, []int) {
	prefix := string(impersonate) + "_"

	var profileNames []string
	for name := range profiles.MappedTLSClients {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		if strings.Contains(name, "_PSK") || strings.Contains(name, "_PQ") {
			continue
		}

		profileNames = append(profileNames, name)
	}

	sort.Slice(profileNames, func(i, j int) bool {
		return compareVersions(
			profileVersion(profileNames[i], prefix),
			profileVersion(profileNames[j], prefix),
		) > 0
	})

	if len(profileNames) == 0 {
		logger.Pl.W("No TLS client profiles found for impersonation type %q; using default profile", impersonate)
		return profiles.DefaultClientProfile, nil
	}

	chosen := profileNames[0]
	if maxMajor > 0 {
		chosen = profileNames[len(profileNames)-1]
		for _, name := range profileNames {
			if v := profileVersion(name, prefix); len(v) > 0 && v[0] <= maxMajor {
				chosen = name
				break
			}
		}
	}

	logger.Pl.I("Using TLS client profile %q for impersonation type %q", chosen, impersonate)
	return profiles.MappedTLSClients[chosen], profileVersion(chosen, prefix)
}

// profileVersion extracts the leading numeric version components from a TLS client profile name (e.g. "safari_16_0" -> [16 0]).
func profileVersion(name, prefix string) []int {
	parts := strings.Split(strings.TrimPrefix(name, prefix), "_")
	version := make([]int, 0, len(parts))

	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		version = append(version, n)
	}

	return version
}

// browserUserAgent returns a desktop User-Agent string matching the impersonated browser and profile version.
func browserUserAgent(impersonate consts.Impersonate, version []int) string {
	major := 146 // tls-client's default profile (Chrome 146).
	if len(version) > 0 {
		major = version[0]
	}

	switch impersonate {
	case consts.ImpersonateFirefox:
		return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:%d.0) Gecko/20100101 Firefox/%d.0", major, major)
	case consts.ImpersonateSafari:
		minor := 0
		if len(version) > 1 {
			minor = version[1]
		}
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/%d.%d Safari/605.1.15", major, minor)
	// Opera too difficult to maintain; use Chrome instead.
	// case consts.ImpersonateOpera:
	// 	return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36 OPR/%d.0.0.0", major, major)
	default:
		// Chrome, Brave (sends a plain Chrome UA), and the default profile.
		return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major)
	}
}

// compareVersions compares two version component slices.
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}

	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	default:
		return 0
	}
}
