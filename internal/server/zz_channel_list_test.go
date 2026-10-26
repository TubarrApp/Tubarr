package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// listedChannel is a channel as the channel list returns it.
type listedChannel struct {
	Name     string         `json:"name"`
	Profile  string         `json:"profile"`
	Settings map[string]any `json:"settings"`
}

// TestChannelListShowsProfileSettings tests that the channel list shows the settings in effect, its profile's filling in
// those the channel leaves unset, and names the profile.
func TestChannelListShowsProfileSettings(t *testing.T) {
	setupFuzzServer(t)
	dir := filepath.Join(fuzzServer.dir, "list-profile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {"List Profile"}, "crawl_freq": {"45"}, "video_directory": {dir}}); w.Code != http.StatusCreated {
		t.Fatalf("adding the profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", "List Profile").Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	form := url.Values{"name": {"List Channel"}, "urls": {"https://example.com/@list"}, "profile_id": {fmt.Sprint(profileID)}}
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding the channel = %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { deleteChannelNamed(t, "List Channel") })

	var channels []listedChannel
	fetchJSON(t, "/api/v1/channels/all", &channels)
	i := slices.IndexFunc(channels, func(c listedChannel) bool { return c.Name == "List Channel" })
	if i < 0 {
		t.Fatal("the channel isn't listed")
	}
	got := channels[i]
	if got.Profile != "List Profile" {
		t.Errorf("profile listed as %q, want %q", got.Profile, "List Profile")
	}
	if g := formValue(got.Settings["crawl_freq"]); g != "45" {
		t.Errorf("crawl frequency listed as %q, want the profile's 45", g)
	}
	if g := formValue(got.Settings["video_directory"]); g != dir {
		t.Errorf("video directory listed as %q, want the profile's %q", g, dir)
	}
}
