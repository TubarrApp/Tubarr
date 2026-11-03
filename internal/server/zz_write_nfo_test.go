package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"testing"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// TestWriteNFOChannelChoices tests the channel form's three choices: blank saves unset so a profile's Yes applies, and an
// explicit No is kept (not dropped as if blank), so it beats the profile's Yes.
func TestWriteNFOChannelChoices(t *testing.T) {
	setupFuzzServer(t)
	const profile = "NFO Writer"
	if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {profile}, "metarr_write_nfo": {"true"}}); w.Code != http.StatusCreated {
		t.Fatalf("adding profile = %d %s", w.Code, w.Body.String())
	}
	var profileID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", profile).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
	})

	for _, tc := range []struct {
		name     string
		choice   string
		saved    any // As the edit page reads it.
		inEffect any // As list channels reads it.
	}{
		{"default", "", nil, true},
		{"yes", "true", true, true},
		{"no", "false", false, false},
	} {
		channelURL := "https://example.com/@nfo-" + tc.name
		form := webChannelForm(t, "NFO "+tc.name, channelURL)
		form.Set("profile_id", fmt.Sprint(profileID))
		if tc.choice != "" {
			form.Set("metarr_write_nfo", tc.choice)
		}
		id := addWebChannel(t, form)

		var details channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &details)
		if got := details.Metarr["metarr_write_nfo"]; got != tc.saved {
			t.Errorf("%s: saved as %v, want %v", tc.name, got, tc.saved)
		}

		type listedChannel struct {
			ID     int64          `json:"id"`
			Metarr map[string]any `json:"metarr"`
		}
		var channels []listedChannel
		fetchJSON(t, "/api/v1/channels/all", &channels)
		i := slices.IndexFunc(channels, func(c listedChannel) bool { return c.ID == id })
		if i < 0 {
			t.Fatalf("%s: channel %d isn't listed", tc.name, id)
		}
		if got := channels[i].Metarr["metarr_write_nfo"]; got != tc.inEffect {
			t.Errorf("%s: listed as %v, want %v", tc.name, got, tc.inEffect)
		}
	}
}

// TestWriteNFOURLChoices tests a URL's explicit No is kept, as a boolean in its own settings.
func TestWriteNFOURLChoices(t *testing.T) {
	setupFuzzServer(t)
	const channelURL = "https://example.com/@nfo-url"
	form := webChannelForm(t, "NFO URL", channelURL)
	form.Set("metarr_write_nfo", "true")
	setURLSettings(t, form, channelURL, map[string]any{"metarr_write_nfo": false})
	id := addWebChannel(t, form)

	var details channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &details)
	if got := details.URLSettings[channelURL].Metarr["metarr_write_nfo"]; got != false {
		t.Errorf("URL's No saved as %v", got)
	}
}

// TestWriteNFODownloadAs tests Download As reads a saved choice back, No included, with the channel URL's as the default.
func TestWriteNFODownloadAs(t *testing.T) {
	setupFuzzServer(t)
	const channelURL = "https://example.com/@nfo-download-as"
	form := webChannelForm(t, "NFO Download As", channelURL)
	form.Set("metarr_write_nfo", "true")
	chanID := addWebChannel(t, form)
	var cuID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = ? AND url = ?", chanID, channelURL).Scan(&cuID); err != nil {
		t.Fatal(err)
	}

	vs := repo.GetVideoStore(fuzzServer.db)
	const videoURL = "https://example.com/v/nfo-download-as"
	if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: cuID, ChannelURL: channelURL, URL: videoURL, Title: "NFO"}}, chanID); err != nil {
		t.Fatal(err)
	}
	var videoID int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE url = ?", videoURL).Scan(&videoID); err != nil {
		t.Fatal(err)
	}

	// The dialog's body decodes to a parsed override, as the handlers read it.
	var input metarrArgsInput
	if err := json.Unmarshal([]byte(`{"metarr_write_nfo":false}`), &input); err != nil {
		t.Fatal(err)
	}
	overrides, err := parseOverrideLines(&models.OverrideLines{}, &input)
	if err != nil {
		t.Fatal(err)
	}
	if err := vs.SetVideoOverrides([]int64{videoID}, overrides); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Defaults map[string]any `json:"defaults_metarr"`
		Saved    map[string]any `json:"saved_metarr"`
	}
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d/override-ops/%d?video=%d", chanID, cuID, videoID), &got)
	if got.Defaults["metarr_write_nfo"] != true {
		t.Errorf("default = %v, want the channel URL's Yes", got.Defaults["metarr_write_nfo"])
	}
	if got.Saved["metarr_write_nfo"] != false {
		t.Errorf("saved = %v, want the video's No", got.Saved["metarr_write_nfo"])
	}
}

// TestDownloadAsMetarrFields tests the Download As dialog's Metarr fields are all reloaded by metarrForm and read by the
// server, and that its Yes/No fields, which select strings, are sent as the booleans the server reads.
func TestDownloadAsMetarrFields(t *testing.T) {
	src := readWebFile(t, "partials/download-as.html")
	m := regexp.MustCompile(`metarrKeys: \[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("download-as.html has no metarrKeys")
	}
	var reloaded []string
	for _, s := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(m[1], -1) {
		reloaded = append(reloaded, s[1])
	}

	bound := boundKeys(bindings(src), "form.metarr.")
	boundNames := make([]string, 0, len(bound))
	for k := range bound {
		boundNames = append(boundNames, k)
	}
	reportDiff(t, "download-as.html", "fields", boundNames, "metarrKeys", reloaded)

	kinds := fieldKinds(metarrArgsInput{})
	for _, k := range boundNames {
		kind, ok := kinds[k]
		if !ok {
			t.Errorf("download-as.html: field %q isn't read by the server", k)
			continue
		}
		sent := regexp.MustCompile(`metarr\.` + k + ` = metarr\.` + k + ` === 'true'`)
		if kind.String() == "bool" && !sent.MatchString(src) {
			t.Errorf("download-as.html: Yes/No field %q isn't sent as a boolean", k)
		}
	}
}
