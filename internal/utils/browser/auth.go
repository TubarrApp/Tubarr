package browser

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"golang.org/x/net/publicsuffix"

	"golang.org/x/net/html"
)

// channelAuth authenticates a user for a given channel, if login credentials are present.
func channelAuth(channelURL, cookiesFilePath string, c *models.Channel) ([]*http.Cookie, error) {
	if customAuthCookies[channelURL] == nil { // If the user is not already authenticated
		cookies, err := login(cookiesFilePath, c)
		if err != nil {
			return nil, err
		}
		customAuthCookies[channelURL] = cookies
	}
	return customAuthCookies[channelURL], nil
}

// login logs the user in and returns the authentication cookie.
func login(cookiesFilePath string, c *models.Channel) ([]*http.Cookie, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	client := &http.Client{Jar: jar}

	logging.I("Logging in to %q with username %q", c.LoginURL, c.Username)

	// Fetch the login page to get a fresh token
	req, err := http.NewRequest("GET", c.LoginURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse the login page to find any hidden token fields
	token := parseToken(string(body))

	// Prepare the login form data
	data := url.Values{}
	data.Set("email", c.Username)
	data.Set("username", c.Username)
	data.Set("password", c.Password)
	if token != "" {
		data.Set("_token", token)
	}
	logging.D(1, "Sending token %q", data.Get("_token"))

	// Post the login form
	req, err = http.NewRequest("POST", c.LoginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Log the cookies for debugging
	if logging.Level > 1 {
		for _, cookie := range resp.Cookies() {
			logging.I("Cookie received: %s=%s; Expires=%s", cookie.Name, cookie.Value, cookie.Expires)
		}
	}

	// Save cookies to file
	err = saveCookiesToFile(resp.Cookies(), cookiesFilePath, c)
	if err != nil {
		return nil, err
	}

	return resp.Cookies(), nil
}

// parseToken parses the HTML body to find the value of the token field.
func parseToken(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}

	var token string
	var f func(*html.Node)
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
