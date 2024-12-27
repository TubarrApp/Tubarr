package browser

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

var AuthenticatedCookies map[string][]*http.Cookie

// channelAuth authenticates a user for a given channel, if login credentials are present.
func channelAuth(username, password, channelURL, loginURL string) ([]*http.Cookie, error) {
	parsed, err := url.Parse(channelURL)
	if err != nil {
		return nil, err
	}
	channelURL = parsed.Hostname()

	if AuthenticatedCookies[channelURL] == nil { // If the user is not already authenticated
		cookies, err := login(username, password, loginURL)
		if err != nil {
			return nil, err
		}
		AuthenticatedCookies[channelURL] = cookies
	}
	return AuthenticatedCookies[channelURL], nil
}

// login logs the user in and returns the authentication cookie.
func login(username, password, loginURL string) ([]*http.Cookie, error) {
	client := &http.Client{}

	// Get the login page to retrieve any tokens
	resp, err := client.Get(loginURL)
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
	data.Set("username", username)
	data.Set("email", username)
	data.Set("password", password)
	if token != "" {
		data.Set("_token", token)
	}

	// Post the login form
	req, err := http.NewRequest("POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
