package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"tubarr/internal/models"
)

// TestLoginCookieDomain tests that a login cookie sent without a domain gets the login host's, so it can be saved for yt-dlp.
func TestLoginCookieDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc"}) // Host-only: no Domain attribute.
		}
	}))
	defer srv.Close()

	cookies, err := login(context.Background(), &models.ChannelAccessDetails{LoginURL: srv.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	if len(cookies) != 1 || cookies[0].Domain != u.Hostname() {
		t.Fatalf("expected one cookie with domain %q, got %+v", u.Hostname(), cookies)
	}
}

// TestLoginRedirectCookie tests that a session cookie set only on the login's redirect is returned.
func TestLoginRedirectCookie(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/home", http.StatusFound)
		}
	})
	mux.HandleFunc("/home", func(http.ResponseWriter, *http.Request) {}) // Sets no cookies.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cookies, err := login(context.Background(), &models.ChannelAccessDetails{LoginURL: srv.URL + "/login", Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "abc" || cookies[0].Domain != u.Hostname() {
		t.Fatalf("expected the redirect's session cookie with domain %q, got %+v", u.Hostname(), cookies)
	}
}
