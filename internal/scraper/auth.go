package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"tubarr/internal/auth"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

// login logs the user in and returns the authentication cookie.
func login(ctx context.Context, a *models.ChannelAccessDetails) ([]*http.Cookie, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}
	logger.Pl.I("Logging in to %q with username %q and password %s", a.LoginURL, a.Username, auth.StarPassword(a.Password))

	// 'GET' the login page to get a fresh token
	req, err := http.NewRequestWithContext(ctx, "GET", a.LoginURL, nil)
	if err != nil {
		return nil, err
	}
	getResp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if getResp == nil {
		return nil, fmt.Errorf("'GET' request returned a nil response")
	}
	defer func() {
		if getResp != nil && getResp.Body != nil {
			if err := getResp.Body.Close(); err != nil {
				logger.Pl.E("failed to close 'resp.Body' for login URL %v: %v", a.LoginURL, err)
			}
		}
	}()

	body, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, err
	}
	logger.Pl.D(4, "Got login page body %s", string(body))

	// Parse the login page to find any hidden token fields
	token := parseToken(string(body))

	// Prepare the login form data
	data := url.Values{}
	data.Set("email", a.Username)
	data.Set("username", a.Username)
	data.Set("password", a.Password)
	if token != "" {
		data.Set("_token", token)
	}
	logger.Pl.D(1, "Sending token %q", data.Get("_token"))

	// 'POST' auth details to the login form
	req, err = http.NewRequestWithContext(ctx, "POST", a.LoginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	postResp, err := client.Do(req) // Use different variable name to avoid shadowing
	if err != nil {
		return nil, err
	}
	if postResp == nil {
		return nil, fmt.Errorf("'POST' request returned a nil response")
	}
	defer func() {
		if postResp != nil && postResp.Body != nil {
			if err := postResp.Body.Close(); err != nil {
				logger.Pl.E("failed to close 'resp.Body' for login URL (after sending token) %v: %v", a.LoginURL, err)
			}
		}
	}()

	// A rejected login often answers with an error, or redirects back to the login page.
	final := postResp.Request.URL
	switch {
	case postResp.StatusCode >= 400:
		logger.Pl.W("Login to %q answered %q: check the username and password", a.LoginURL, postResp.Status)
	case postResp.Request.Method == http.MethodGet && final.Host == req.URL.Host &&
		strings.TrimSuffix(final.Path, "/") == strings.TrimSuffix(req.URL.Path, "/"):
		logger.Pl.W("Login to %q redirected back to the login page: check the username and password", a.LoginURL)
	}

	// Cookies from the final response keep their attributes. The jar's others for the login and final URLs (e.g. a
	// session cookie set on a redirect) get their URL's host as domain, as the jar keeps only names and values.
	cookies := postResp.Cookies()
	have := make(map[string]bool, len(cookies))
	for _, cookie := range cookies {
		if cookie.Domain == "" {
			cookie.Domain = final.Hostname()
		}
		have[cookie.Name] = true
	}
	for _, u := range []*url.URL{final, req.URL} {
		for _, cookie := range jar.Cookies(u) {
			if !have[cookie.Name] {
				have[cookie.Name] = true
				cookie.Domain, cookie.Path = u.Hostname(), "/"
				cookies = append(cookies, cookie)
			}
		}
	}
	if logging.Level() > 1 {
		for _, cookie := range cookies {
			logger.Pl.I("Cookie received: %s; Expires=%s", cookie.Name, cookie.Expires) // Value omitted, it's a session secret.
		}
	}

	return cookies, nil
}

// parseToken parses the HTML body to find the value of the token field.
func parseToken(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}

	var (
		token string
		f     func(*html.Node)
	)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			var name, value string
			for _, attr := range n.Attr {
				if attr.Key == "name" && attr.Val == "_token" {
					name = attr.Val
				}
				if attr.Key == "value" {
					value = attr.Val
				}
			}
			if name == "_token" {
				token = value
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	return token
}
