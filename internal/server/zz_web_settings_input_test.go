package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"tubarr/internal/parsing"
)

// The channel form and the per-URL settings the channel pages send, which share the CLI's and config files' settings
// input.

// webSettingsDir makes a folder under the test directory's wiring folder, returning its path.
func webSettingsDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(fuzzServer.dir, "wiring", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// webChannelForm returns a channel form holding only what a channel needs, as the add page sends it.
func webChannelForm(t *testing.T, name, channelURL string) url.Values {
	t.Helper()
	return url.Values{"name": {name}, "urls": {channelURL}, "video_directory": {webSettingsDir(t, "videos-"+name)}}
}

// setURLSettings sets the form's per-URL settings for channelURL, splitting fields into the settings and Metarr sections.
func setURLSettings(t *testing.T, form url.Values, channelURL string, fields map[string]any) {
	t.Helper()
	settings, metarr := map[string]any{}, map[string]any{}
	for k, v := range fields {
		if strings.HasPrefix(k, "metarr_") {
			metarr[k] = v
		} else {
			settings[k] = v
		}
	}
	data, err := json.Marshal(map[string]any{channelURL: map[string]any{"settings": settings, "metarr": metarr}})
	if err != nil {
		t.Fatal(err)
	}
	form.Set("url_settings", string(data))
}

// addWebChannel adds a channel through the API, returning its ID.
func addWebChannel(t *testing.T, form url.Values) int64 {
	t.Helper()
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code >= 300 {
		t.Fatalf("adding channel %q = %d %s", form.Get("name"), w.Code, w.Body.String())
	}
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", form.Get("name")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", "") })
	return id
}

// TestWebInputsReachSettingsInput tests that every per-URL and Download As field the pages send reaches the settings
// input the CLI and config files use.
func TestWebInputsReachSettingsInput(t *testing.T) {
	var s settingsInput
	var m metarrArgsInput
	for _, in := range []any{&s, &m} {
		v := reflect.ValueOf(in).Elem()
		for i := range v.NumField() {
			switch f := v.Field(i); f.Kind() {
			case reflect.Pointer:
				f.Set(reflect.New(f.Type().Elem()))
			case reflect.Slice:
				f.Set(reflect.MakeSlice(f.Type(), 0, 0))
			}
		}
	}
	if got, want := len(parsing.GivenKeys(s.input())), reflect.TypeFor[settingsInput]().NumField(); got != want {
		t.Errorf("%d of the %d per-URL settings fields reach the settings input", got, want)
	}
	if got, want := len(parsing.GivenKeys(m.input())), reflect.TypeFor[metarrArgsInput]().NumField(); got != want {
		t.Errorf("%d of the %d Metarr fields reach the Metarr args input", got, want)
	}
}

// TestWebSettingsNormalized tests how the channel form and per-URL settings save values needing normalizing.
func TestWebSettingsNormalized(t *testing.T) {
	setupFuzzServer(t)
	const u = "https://example.com/@web-normalized"
	form := webChannelForm(t, "Web Normalized", u)
	for k, v := range map[string]string{
		"json_directory":       "", // Blank means the video directory.
		"ytdlp_output_ext":     "MKV",
		"metarr_output_ext":    "MP4",
		"crawl_freq":           "-5",
		"download_retries":     "-1",
		"max_concurrency":      "-2",
		"metarr_concurrency":   "-3",
		"metarr_max_cpu_usage": "150",
	} {
		form.Set(k, v)
	}
	setURLSettings(t, form, u, map[string]any{
		"ytdlp_output_ext":     "WEBM",
		"metarr_output_ext":    "MKV",
		"crawl_freq":           -5,
		"metarr_max_cpu_usage": 150,
	})
	id := addWebChannel(t, form)

	var got channelDetails
	fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
	for _, c := range []struct {
		what string
		read any
		want string
	}{
		{"json_directory", got.Settings["json_directory"], form.Get("video_directory")},
		{"ytdlp_output_ext", got.Settings["ytdlp_output_ext"], "mkv"},
		{"metarr_output_ext", got.Metarr["metarr_output_ext"], "mp4"},
		{"crawl_freq", got.Settings["crawl_freq"], ""},
		{"download_retries", got.Settings["download_retries"], ""},
		{"max_concurrency", got.Settings["max_concurrency"], ""},
		{"metarr_concurrency", got.Metarr["metarr_concurrency"], ""},
		{"metarr_max_cpu_usage", got.Metarr["metarr_max_cpu_usage"], "101"},
		{"per-URL ytdlp_output_ext", got.URLSettings[u]["settings"]["ytdlp_output_ext"], "webm"},
		{"per-URL metarr_output_ext", got.URLSettings[u]["metarr"]["metarr_output_ext"], "mkv"},
		{"per-URL crawl_freq", got.URLSettings[u]["settings"]["crawl_freq"], ""},
		{"per-URL metarr_max_cpu_usage", got.URLSettings[u]["metarr"]["metarr_max_cpu_usage"], "101"},
	} {
		if g := formValue(c.read); g != c.want {
			t.Errorf("%s reads back as %q, want %q", c.what, g, c.want)
		}
	}
}

// TestWebSettingsRefused tests that the channel form and per-URL settings refuse each invalid value.
func TestWebSettingsRefused(t *testing.T) {
	setupFuzzServer(t)
	missing := filepath.Join(fuzzServer.dir, "wiring", "missing.txt")
	for i, c := range []struct {
		key string
		bad any
	}{
		{"video_directory", "/media/{{nope}}"},
		{"ytdlp_output_ext", "xyz"},
		{"max_filesize", "lots"},
		{"from_date", "someday"},
		{"to_date", "someday"},
		{"filters", "nonsense"},
		{"move_ops", "nonsense"},
		{"filter_file", missing},
		{"move_ops_file", missing},
		{"metarr_output_ext", "xyz"},
		{"metarr_rename_style", "bogus"},
		{"metarr_gpu", "bogus"},
		{"metarr_purge_metafile", "bogus"},
		{"metarr_min_free_mem", "lots"},
		{"metarr_transcode_quality", "high"},
		{"metarr_transcode_audio_codecs", "bogus"},
		{"metarr_meta_ops", "nonsense"},
		{"metarr_filename_ops", "nonsense"},
		{"metarr_meta_ops_file", missing},
		{"metarr_filename_ops_file", missing},
	} {
		t.Run(c.key, func(t *testing.T) {
			name := fmt.Sprintf("Web Refused %d", i)
			u := fmt.Sprintf("https://example.com/@web-refused-%d", i)

			form := webChannelForm(t, name, u)
			form.Set(c.key, fmt.Sprint(c.bad))
			if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code < 400 {
				deleteChannelNamed(t, name)
				t.Errorf("channel form saved %s %q (%d)", c.key, c.bad, w.Code)
			}

			form = webChannelForm(t, name, u)
			setURLSettings(t, form, u, map[string]any{c.key: c.bad})
			if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code < 400 {
				deleteChannelNamed(t, name)
				t.Errorf("per-URL settings saved %s %q (%d)", c.key, c.bad, w.Code)
			}
		})
	}
}

// TestWebSettingsMissingFile tests that a file going missing only stops the edit form saving if the save changes it.
func TestWebSettingsMissingFile(t *testing.T) {
	setupFuzzServer(t)
	for _, c := range []struct {
		what   string
		perURL bool
		key    string
	}{
		{"channel filter file", false, "filter_file"},
		{"channel Metarr meta ops file", false, "metarr_meta_ops_file"},
		{"per-URL filter file", true, "filter_file"},
		{"per-URL Metarr meta ops file", true, "metarr_meta_ops_file"},
	} {
		t.Run(c.what, func(t *testing.T) {
			name := "Web Missing " + c.what
			u := "https://example.com/@web-missing-" + strings.ReplaceAll(c.what, " ", "-")
			path := wiringFile(t, strings.ReplaceAll(c.what, " ", "-")+".txt", "title:set:A\n")
			if c.key == "filter_file" {
				path = wiringFile(t, strings.ReplaceAll(c.what, " ", "-")+".txt", "title:contains:a:must\n")
			}
			form := webChannelForm(t, name, u)
			if c.perURL {
				setURLSettings(t, form, u, map[string]any{c.key: path})
			} else {
				form.Set(c.key, path)
			}
			id := addWebChannel(t, form)

			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			target := fmt.Sprintf("/api/v1/channels/%d/update", id)
			if w := sendForm(t, http.MethodPut, target, form); w.Code >= 400 {
				t.Errorf("saving unchanged with %s missing: %d %s", c.what, w.Code, w.Body.String())
			}

			// Changed to another missing file.
			moved := path + ".moved"
			if c.perURL {
				setURLSettings(t, form, u, map[string]any{c.key: moved})
			} else {
				form.Set(c.key, moved)
			}
			if w := sendForm(t, http.MethodPut, target, form); w.Code < 400 {
				t.Errorf("saved %s changed to a missing file", c.what)
			}
		})
	}
}

// TestParseURLSettingsNumbers tests that numeric per-URL settings sent as JSON numbers are kept.
func TestParseURLSettingsNumbers(t *testing.T) {
	const raw = `{"https://example.com/c":{
		"settings":{"max_concurrency":3,"crawl_freq":45,"download_retries":2,"paused":true},
		"metarr":{"metarr_concurrency":4,"metarr_max_cpu_usage":50.5}}}`

	parsed, err := parseURLSettings(raw)
	if err != nil {
		t.Fatalf("parseURLSettings: %v", err)
	}
	got, ok := parsed["https://example.com/c"]
	if !ok || got.Settings == nil || got.Metarr == nil {
		t.Fatalf("parsed = %+v, want settings and metarr for the URL", parsed)
	}
	if s := got.Settings; s.Concurrency != 3 || s.CrawlFreq != 45 || s.Retries != 2 || !s.Paused {
		t.Errorf("settings = concurrency %d, crawl freq %d, retries %d, paused %v; want 3, 45, 2, true",
			s.Concurrency, s.CrawlFreq, s.Retries, s.Paused)
	}
	if m := got.Metarr; m.Concurrency != 4 || m.MaxCPU != 50.5 {
		t.Errorf("metarr = concurrency %d, max CPU %v; want 4, 50.5", m.Concurrency, m.MaxCPU)
	}
}

// TestSplitAuthDetails tests that a JSON array keeps values containing spaces, and the old space-separated form still works.
func TestSplitAuthDetails(t *testing.T) {
	got, err := splitAuthDetails(`[{"username":"a b","password":"correct horse"},{"username":"c"}]`)
	if err != nil || len(got) != 2 || got[0] != `{"username":"a b","password":"correct horse"}` {
		t.Fatalf("splitAuthDetails(array) = %q, %v", got, err)
	}
	if got, err := splitAuthDetails(`{"username":"a"} {"username":"b"}`); err != nil || len(got) != 2 {
		t.Fatalf("splitAuthDetails(legacy) = %q, %v", got, err)
	}
}

// TestParseURLSettingsRejectsBadType tests that a wrongly typed value is an error rather than silently dropped.
func TestParseURLSettingsRejectsBadType(t *testing.T) {
	if _, err := parseURLSettings(`{"https://example.com/c":{"settings":{"max_concurrency":"three"}}}`); err == nil {
		t.Fatal("parseURLSettings accepted a string concurrency, want an error")
	}
}
