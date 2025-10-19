package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"tubarr/internal/auth"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

var globalAuthCookieCache sync.Map

// channelAuth authenticates a user for a given channel, if login credentials are present.
func channelAuth(ctx context.Context, channelURL string, a *models.ChannelAccessDetails) ([]*http.Cookie, error) {
	// First check exact hostname
	if cookies, found := tryLoadCachedCookies(channelURL); found {
		return cookies, nil
	}

	// Check base domain as fallback
	baseDomain, err := getBaseDomain(channelURL)
	if err == nil {
		if cookies, found := tryLoadCachedCookies(baseDomain); found {
			return cookies, nil
		}
	}

	// If neither exists, login and store under exact hostname
	cookies, err := login(ctx, a)
	if err != nil {
		return nil, err
	}
	globalAuthCookieCache.Store(channelURL, cookies)

	// Return cookies
	return cookies, nil
}

// login logs the user in and returns the authentication cookie.
func login(ctx context.Context, a *models.ChannelAccessDetails) ([]*http.Cookie, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar}
	logging.I("Logging in to %q with username %q and password %s", a.LoginURL, a.Username, auth.StarPassword(a.Password))

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
				logging.E("failed to close 'resp.Body' for login URL %v: %v", a.LoginURL, err)
			}
		}
	}()

	body, err := io.ReadAll(getResp.Body)
	if err != nil {
		return nil, err
	}
	logging.D(4, "Got login page body %s", string(body))

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
	logging.D(1, "Sending token %q", data.Get("_token"))

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
				logging.E("failed to close 'resp.Body' for login URL (after sending token) %v: %v", a.LoginURL, err)
			}
		}
	}()

	// Log the cookies for debugging
	if logging.Level > 1 {
		for _, cookie := range postResp.Cookies() {
			logging.I("Cookie received: %s=%s; Expires=%s", cookie.Name, cookie.Value, cookie.Expires)
		}
	}

	return postResp.Cookies(), nil
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
