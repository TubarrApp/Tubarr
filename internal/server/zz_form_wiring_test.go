package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"tubarr/internal/blocking"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// The web pages name every field and request key in plain HTML and JavaScript, separately from the handlers. These
// tests fail when the two drift apart.

const (
	wiringURLA = "https://example.com/@wiring-a"
	wiringURLB = "https://example.com/@wiring-b"
)

// perURLFixedKeys are the per-URL fields the channel pages send whether or not custom settings are on.
var perURLFixedKeys = []string{"paused", "use_global_cookies", "auto_download"}

// perURLMetarrBools are the per-URL Metarr choices the channel pages send as booleans, with the custom settings.
var perURLMetarrBools = []string{"metarr_purge_original_video"}

// ---- Page parsing ----

// webRoot is the repository's web folder.
func webRoot() string {
	return filepath.Join(pkgDir, "..", "..", "web")
}

// readWebFile returns a file from the web folder.
func readWebFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(webRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// readBuiltPage returns a page as it's served, with its includes filled in.
func readBuiltPage(t *testing.T, name string) string {
	t.Helper()
	built, err := fillIncludes(webRoot(), []byte(readWebFile(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(built)
}

// webPages returns every page and partial in the web folder, by path relative to it.
func webPages(t *testing.T) map[string]string {
	t.Helper()
	pages := map[string]string{}
	for _, pattern := range []string{"*.html", filepath.Join("partials", "*.html")} {
		matches, err := filepath.Glob(filepath.Join(webRoot(), pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(webRoot(), m)
			if err != nil {
				t.Fatal(err)
			}
			pages[rel] = readWebFile(t, rel)
		}
	}
	return pages
}

// binding is an x-model binding: the model path it writes, and whether it writes a number.
type binding struct {
	path   string
	number bool
}

var xModelRe = regexp.MustCompile(`x-model((?:\.[a-z]+)*)="([\w.]+)"`)

// bindings returns a page's x-model bindings.
func bindings(src string) []binding {
	var out []binding
	for _, m := range xModelRe.FindAllStringSubmatch(src, -1) {
		out = append(out, binding{path: m[2], number: slices.Contains(strings.Split(m[1], "."), "number")})
	}
	return out
}

// boundKeys returns the keys bound directly under prefix, and whether each binds as a number.
func boundKeys(bs []binding, prefix string) map[string]bool {
	keys := map[string]bool{}
	for _, b := range bs {
		if k, ok := strings.CutPrefix(b.path, prefix); ok && !strings.Contains(k, ".") {
			keys[k] = keys[k] || b.number
		}
	}
	return keys
}

var objectKeyRe = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*:`)

// objectKeys returns the top-level keys of the JavaScript object literal opened by marker (ending in "{"), the first
// one after anchor. The models it reads hold one key per line.
func objectKeys(t *testing.T, src, anchor, marker string) []string {
	t.Helper()
	start := strings.Index(src, anchor)
	if start < 0 {
		t.Fatalf("page has no %q", anchor)
	}
	i := strings.Index(src[start:], marker)
	if i < 0 {
		t.Fatalf("page has no %q after %q", marker, anchor)
	}
	open := start + i + len(marker) - 1
	depth, end := 0, -1
	for j := open; j < len(src) && end < 0; j++ {
		switch src[j] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		if depth == 0 {
			end = j
		}
	}
	if end < 0 {
		t.Fatalf("object %q after %q never closes", marker, anchor)
	}

	var keys []string
	depth = 0
	for _, line := range strings.Split(src[open+1:end], "\n") {
		if m := objectKeyRe.FindStringSubmatch(line); m != nil && depth == 0 {
			keys = append(keys, m[1])
		}
		depth += strings.Count(line, "{") + strings.Count(line, "[") - strings.Count(line, "}") - strings.Count(line, "]")
	}
	return keys
}

// arrayStrings returns the strings in the JavaScript array assigned to const name.
func arrayStrings(t *testing.T, src, name string) []string {
	t.Helper()
	m := regexp.MustCompile(`const ` + name + ` = \[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("page has no array %q", name)
	}
	var out []string
	for _, s := range regexp.MustCompile(`'(\w+)'`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, s[1])
	}
	return out
}

// perURLKeys returns a channel page's per-URL model keys, checking the add page's two copies agree.
func perURLKeys(t *testing.T, page, src string) []string {
	t.Helper()
	if page == "edit-channel.html" {
		return objectKeys(t, src, "getEmptyCustomSettings() {", "return {")
	}
	first := objectKeys(t, src, "formData: {", "custom: {")
	reportDiff(t, page, "first URL's per-URL model", first, "per-URL model in addURL()", objectKeys(t, src, "addURL() {", "custom: {"))
	return first
}

// jsonTags returns the JSON names of a struct's fields.
func jsonTags(v any) []string {
	var tags []string
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		if name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			tags = append(tags, name)
		}
	}
	return tags
}

// fieldKinds returns the kind of each struct field by JSON name, looking through pointers.
func fieldKinds(vs ...any) map[string]reflect.Kind {
	kinds := map[string]reflect.Kind{}
	for _, v := range vs {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			kinds[name] = ft.Kind()
		}
	}
	return kinds
}

// reportDiff reports keys found in only one of two lists.
func reportDiff(t *testing.T, page, aName string, a []string, bName string, b []string) {
	t.Helper()
	for _, k := range a {
		if !slices.Contains(b, k) {
			t.Errorf("%s: %q is in the %s but not the %s", page, k, aName, bName)
		}
	}
	for _, k := range b {
		if !slices.Contains(a, k) {
			t.Errorf("%s: %q is in the %s but not the %s", page, k, bName, aName)
		}
	}
}

// without returns list without key.
func without(list []string, key string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(k string) bool { return k == key })
}

// ---- Requests ----

// sendRequest sends a request to the shared test server.
func sendRequest(t *testing.T, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	return w
}

// sendForm sends a URL-encoded form, as the pages' URLSearchParams bodies are.
func sendForm(t *testing.T, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return sendRequest(t, method, target, "application/x-www-form-urlencoded", form.Encode())
}

// sendJSON sends a JSON body.
func sendJSON(t *testing.T, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return sendRequest(t, method, target, "application/json", string(data))
}

// fetchJSON decodes a JSON GET response into v, failing unless the API answered it.
func fetchJSON(t *testing.T, target string, v any) {
	t.Helper()
	w := sendRequest(t, http.MethodGet, target, "", "")
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET %s = %d %q: %s", target, w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
}

// wiringFile writes a file under the test directory's wiring folder and returns its path.
func wiringFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(fuzzServer.dir, "wiring", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// deleteChannelNamed deletes a channel through the API.
func deleteChannelNamed(t *testing.T, name string) {
	t.Helper()
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", name).Scan(&id); err != nil {
		t.Errorf("channel %q wasn't added: %v", name, err)
		return
	}
	if w := sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", ""); w.Code != http.StatusNoContent {
		t.Errorf("deleting channel %q = %d %s", name, w.Code, w.Body.String())
	}
}

// ---- Values ----

// normalizeLines drops blank lines and carriage returns, keeping trailing spaces (an operation's value may end in one).
func normalizeLines(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSuffix(line, "\r"); strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// formValue renders a value read back from the API as the page shows it in its form.
func formValue(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return normalizeLines(v)
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			parts = append(parts, formValue(p))
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprint(v)
}

// sameFormValue reports whether a value read back matches the one sent, where a zero value may be left out.
func sameFormValue(sent, got string) bool {
	sent = normalizeLines(sent)
	return sent == got || (got == "" && (sent == "false" || sent == "0"))
}

// sameSet reports whether two lists hold the same strings in any order.
func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// notificationsJSON renders notifications as the channel pages send them, each a channel URL (blank for all) and notify URL.
func notificationsJSON(rows ...[2]string) string {
	ns := make([]models.Notification, 0, len(rows))
	for _, r := range rows {
		ns = append(ns, models.Notification{ChannelURL: r[0], NotifyURL: r[1]})
	}
	data, _ := json.Marshal(ns)
	return string(data)
}

// notificationKeys renders notifications sent as JSON, or read back, comparably as "channel URL notify URL".
func notificationKeys[T string | []models.Notification](v T) []string {
	var ns []models.Notification
	switch v := any(v).(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &ns)
	case []models.Notification:
		ns = v
	}
	keys := make([]string, 0, len(ns))
	for _, n := range ns {
		keys = append(keys, n.ChannelURL+" "+n.NotifyURL)
	}
	return keys
}

// urlSettingValue types a per-URL value as the channel pages put it in their url_settings JSON.
func urlSettingValue(key, v string, number bool) any {
	switch {
	case slices.Contains(perURLFixedKeys, key) || slices.Contains(perURLMetarrBools, key):
		return v == "true"
	case key == "ytdlp_preferred_video_codecs" || key == "ytdlp_preferred_audio_codecs":
		return strings.Split(v, "\n")
	case number:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return v
}

// formSamples holds a value for every channel page field, for adding a channel then editing it. Values are as the
// pages send them, chosen to pass validation and read back unchanged. Per-URL settings use them the other way round.
func formSamples(t *testing.T) map[string][2]string {
	t.Helper()
	sub := func(name string) string {
		dir := filepath.Join(fuzzServer.dir, "wiring", name)
		if err := os.MkdirAll(dir, 0o755); err != nil { // Folders must exist to be saved.
			t.Fatal(err)
		}
		return dir
	}
	file := func(name, content string) string { return wiringFile(t, name, content) }
	auth := func(user, pass, login string) string {
		data, err := json.Marshal([]map[string]string{{"channel_url": wiringURLA, "username": user, "password": pass, "login_url": login}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return map[string][2]string{
		// Request fields outside the models.
		"name":         {"Wiring Add", "Wiring Edit"},
		"auth_details": {auth("user-a", "pass-a", "https://example.com/login"), auth("user-b", "pass-b", "https://example.com/login-b")},
		"notifications": {
			notificationsJSON([2]string{"", "http://plex.local:32400/library/sections/1/refresh?X-Plex-Token=abc"}, [2]string{wiringURLB, "http://notify.local/hook"}),
			notificationsJSON([2]string{"", "http://jellyfin.local/Library/Refresh"}, [2]string{"", "http://bare.local/hook"}),
		},

		// Settings.
		"video_directory":              {sub("videos-a"), sub("videos-b")},
		"json_directory":               {sub("json-a"), sub("json-b")},
		"download_concurrency":         {"2", "3"},
		"crawl_freq":                   {"45", "60"},
		"download_retries":             {"2", "4"},
		"max_filesize":                 {"100m", "2g"},
		"from_date":                    {"20240101", "20240201"},
		"to_date":                      {"20241231", "20250131"},
		"filter_file":                  {file("filters-a.txt", "title:contains:cat:must\n"), file("filters-b.txt", "title:contains:dog:must\n")},
		"filters":                      {"title:contains:cat:must\ntitle:omits:dog:any", "description:contains:fish:must"},
		"move_ops_file":                {file("moves-a.txt", "title:contains:cats||/cats\n"), file("moves-b.txt", "title:contains:dogs||/dogs\n")},
		"move_ops":                     {"title:contains:cats: the | movie||" + sub("cats"), "title:omits:dogs||" + sub("dogs")},
		"ytdlp_output_ext":             {"mp4", "mkv"},
		"cookies_from_browser":         {"firefox", "chrome"},
		"external_downloader":          {"aria2c", "axel"},
		"external_downloader_args":     {"-x 4", "-n 2"},
		"extra_ytdlp_video_args":       {"--no-mtime", "--no-part"},
		"extra_ytdlp_meta_args":        {"--no-mtime", "--no-part"},
		"extra_ytdlp_crawl_args":       {"--playlist-end 50", "--playlist-end 20"},
		"use_global_cookies":           {"true", "false"},
		"paused":                       {"true", "false"},
		"auto_download":                {"false", "true"},
		"channel_config_file":          {file("config-a.yaml", "channel-name: Wiring\n"), file("config-b.yaml", "channel-name: Wiring\n")},
		"ytdlp_preferred_video_codecs": {"av1\nvp9", "h264"},
		"ytdlp_preferred_audio_codecs": {"opus", "aac\nopus"},

		// Metarr.
		"metarr_output_directory":                 {sub("out-a") + "/{{year}}", sub("out-b")},
		"metarr_output_ext":                       {"mp4", "mkv"},
		"metarr_rename_style":                     {"spaces", "underscores"},
		"metarr_concurrency":                      {"2", "3"},
		"metarr_max_cpu_usage":                    {"50", "75"},
		"metarr_min_free_mem":                     {"1G", "500M"},
		"metarr_gpu":                              {"auto", "auto"},
		"metarr_transcode_gpu_node":               {file("render-a", ""), file("render-b", "")},
		"metarr_transcode_video_transcode_codecs": {"h264:hevc", "vp9:h264"},
		"metarr_transcode_audio_codecs":           {"aac:opus", "flac:mp3"},
		"metarr_transcode_quality":                {"23", "28"},
		"metarr_extra_ffmpeg_args":                {"-threads 2", "-threads 4"},
		"metarr_filename_ops":                     {"prefix:[CAT] ", "suffix: [DOG]"},
		"metarr_filename_ops_file":                {file("filename-ops-a.txt", "prefix:[A] \n"), file("filename-ops-b.txt", "prefix:[B] \n")},
		"metarr_filtered_filename_ops":            {"title:contains:cat||prefix:[CAT] ", "title:contains:dog||suffix: [DOG]"},
		"metarr_filtered_filename_ops_file":       {file("filtered-filename-ops-a.txt", "title:contains:a||prefix:[A] \n"), file("filtered-filename-ops-b.txt", "title:contains:b||prefix:[B] \n")},
		"metarr_meta_ops":                         {"title:set:Cat Title", "artist:set:Dog Band"},
		"metarr_meta_ops_file":                    {file("meta-ops-a.txt", "title:set:A\n"), file("meta-ops-b.txt", "title:set:B\n")},
		"metarr_filtered_meta_ops":                {"title:contains:ep 5 | cat: finale||artist:set:Cat", "title:contains:dog||artist:set:Dog"},
		"metarr_filtered_meta_ops_file":           {file("filtered-meta-ops-a.txt", "title:contains:a||artist:set:A\n"), file("filtered-meta-ops-b.txt", "title:contains:b||artist:set:B\n")},
		"metarr_purge_metafile":                   {"json", "all"},
		"metarr_purge_original_video":             {"false", "true"},
	}
}

// ---- Channel forms ----

// TestChannelFormModels tests that the add and edit pages bind, hold and send the field names the server reads and
// returns.
func TestChannelFormModels(t *testing.T) {
	settingsTags := jsonTags(settingsResponse{})
	metarrTags := jsonTags(metarrArgsResponse{})
	urlTags := slices.Concat(jsonTags(settingsInput{}), jsonTags(metarrArgsInput{}))
	urlKinds := fieldKinds(settingsInput{}, metarrArgsInput{})
	if !slices.Contains(jsonTags(channelDetailResponse{}), "channel_config_file") {
		t.Error("channel details don't include channel_config_file, so the edit page can't show it")
	}

	for _, page := range []string{"add-channel.html", "edit-channel.html"} {
		src := readBuiltPage(t, page)
		bs := bindings(src)
		topKeys := objectKeys(t, src, "channelForm()", "formData: {")
		settingsKeys := objectKeys(t, src, "formData: {", "settings: {")
		metarrKeys := objectKeys(t, src, "formData: {", "metarr: {")
		urlKeys := perURLKeys(t, page, src)
		urlFields := slices.Concat(arrayStrings(t, src, "settingsFields"), arrayStrings(t, src, "metarrFields"), perURLFixedKeys, perURLMetarrBools)

		for _, model := range []struct {
			prefix string
			keys   []string
		}{
			{"formData.", topKeys},
			{"formData.settings.", settingsKeys},
			{"formData.metarr.", metarrKeys},
			{"urlEntry.custom.", urlKeys},
		} {
			bound := boundKeys(bs, model.prefix)
			for k := range bound {
				if !slices.Contains(model.keys, k) {
					t.Errorf("%s: a field is bound to %s%s, which isn't in that model, so it never loads or sends", page, model.prefix, k)
				}
			}
			if model.prefix == "formData." {
				continue // Its settings, metarr and urls entries hold the other fields.
			}
			for _, k := range model.keys {
				if _, ok := bound[k]; !ok {
					t.Errorf("%s: model key %s%s has no field", page, model.prefix, k)
				}
			}
		}
		reportDiff(t, page, "per-URL field lists", urlFields, "per-URL model", urlKeys)
		reportDiff(t, page, "settings model", without(settingsKeys, "channel_config_file"), "settings the server returns", settingsTags)
		reportDiff(t, page, "Metarr model", metarrKeys, "Metarr settings the server returns", metarrTags)
		reportDiff(t, page, "per-URL model", urlKeys, "per-URL settings the server reads", urlTags)

		// Per-URL settings are sent as JSON, so a field binding a number must reach a number field, and others not.
		for k, number := range boundKeys(bs, "urlEntry.custom.") {
			kind, ok := urlKinds[k]
			if !ok {
				continue // Reported above.
			}
			if numeric := kind == reflect.Int || kind == reflect.Float64; number != numeric {
				t.Errorf("%s: per-URL field %q binds as a number: %t, but the server reads it as %s", page, k, number, kind)
			}
		}

		// Notifications are sent as JSON.
		if !regexp.MustCompile(`payload\.notifications = notificationsJSON\(`).MatchString(src) {
			t.Errorf("%s: notifications aren't sent as JSON", page)
		}

		// Profiles are chosen by ID, the channel's sent as a form field and each URL's in its url_settings entry.
		for _, path := range []string{"formData.profile_id", "urlEntry.profile_id"} {
			if !slices.ContainsFunc(bs, func(b binding) bool { return b.path == path }) {
				t.Errorf("%s: no field is bound to %s", page, path)
			}
		}
		if !regexp.MustCompile(`payload\.profile_id = this\.formData\.profile_id`).MatchString(src) {
			t.Errorf("%s: the channel's profile isn't sent", page)
		}
		if !regexp.MustCompile(`\.profile_id = Number\(urlEntry\.profile_id\)`).MatchString(src) {
			t.Errorf("%s: URL profiles aren't sent", page)
		}
	}
}

// TestChannelListPagesRead tests the channel list pages only read fields the channel list returns.
func TestChannelListPagesRead(t *testing.T) {
	returned := map[string][]string{
		"":         slices.Concat(jsonTags(channelResponse{}), []string{"expanded", "videos"}), // The home page sets these.
		"settings": jsonTags(settingsResponse{}),
		"metarr":   jsonTags(metarrArgsResponse{}),
	}
	read := regexp.MustCompile(`(?:^|[^\w-])channel\.(?:(settings|metarr)\??\.)?([a-z_]+)`)
	for _, page := range []string{"list-channels.html", "index.html"} {
		for _, m := range read.FindAllStringSubmatch(readWebFile(t, page), -1) {
			if !slices.Contains(returned[m[1]], m[2]) {
				t.Errorf("%s reads channel.%s, which the channel list doesn't return", page, strings.TrimPrefix(m[1]+"."+m[2], "."))
			}
		}
	}
}

// channelDetails is a channel as the edit page loads it.
type channelDetails struct {
	Name              string                       `json:"name"`
	ProfileID         int64                        `json:"profile_id"`
	URLs              []string                     `json:"urls"`
	ChannelConfigFile string                       `json:"channel_config_file"`
	Settings          map[string]any               `json:"settings"`
	Metarr            map[string]any               `json:"metarr"`
	AuthDetails       []authDetailResponse         `json:"auth_details"`
	URLSettings       map[string]urlSettingsDetail `json:"url_settings"`
	Notifications     []models.Notification        `json:"notifications"`
}

// urlSettingsDetail is a channel URL's own settings and profile, as the edit page loads them.
type urlSettingsDetail struct {
	ProfileID int64          `json:"profile_id"`
	Settings  map[string]any `json:"settings"`
	Metarr    map[string]any `json:"metarr"`
}

// TestChannelFormRoundTrip adds a channel the way the add page does, then edits it the way the edit page does, with
// every field filled, and checks each value reads back.
func TestChannelFormRoundTrip(t *testing.T) {
	setupFuzzServer(t)
	samples := formSamples(t)
	used := map[string]bool{}

	// Profiles for the channel and URL B to choose, deleted after the channel.
	profileIDs := make([]int64, 2)
	for i, name := range []string{"Wiring Round Trip A", "Wiring Round Trip B"} {
		if w := sendForm(t, http.MethodPost, "/api/v1/profiles/add", url.Values{"name": {name}}); w.Code != http.StatusCreated {
			t.Fatalf("adding profile %q = %d %s", name, w.Code, w.Body.String())
		}
		if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", name).Scan(&profileIDs[i]); err != nil {
			t.Fatal(err)
		}
		profileID := profileIDs[i]
		t.Cleanup(func() {
			sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", "")
		})
	}

	var id int64
	for step, page := range []string{"add-channel.html", "edit-channel.html"} {
		src := readBuiltPage(t, page)
		settingsKeys := objectKeys(t, src, "formData: {", "settings: {")
		metarrKeys := objectKeys(t, src, "formData: {", "metarr: {")
		urlSettingsFields := slices.Concat(arrayStrings(t, src, "settingsFields"), perURLFixedKeys)
		urlMetarrFields := slices.Concat(arrayStrings(t, src, "metarrFields"), perURLMetarrBools)
		urlNumbers := boundKeys(bindings(src), "urlEntry.custom.")

		value := func(key string, perURL bool) string {
			s, ok := samples[key]
			if !ok {
				t.Fatalf("%s: field %q has no value in formSamples: add one so its wiring is tested", page, key)
			}
			used[key] = true
			if perURL {
				return s[1-step]
			}
			return s[step]
		}

		// Built as submitForm builds it.
		form := url.Values{}
		for _, k := range slices.Concat(settingsKeys, metarrKeys) {
			switch v := value(k, false); {
			case k == "paused":
				if v == "true" {
					form.Set(k, v)
				}
			case v != "":
				form.Set(k, v)
			}
		}
		form.Set("profile_id", strconv.FormatInt(profileIDs[step], 10))
		urlSettings, urlMetarr := map[string]any{}, map[string]any{}
		for _, k := range urlSettingsFields {
			urlSettings[k] = urlSettingValue(k, value(k, true), urlNumbers[k])
		}
		for _, k := range urlMetarrFields {
			urlMetarr[k] = urlSettingValue(k, value(k, true), urlNumbers[k])
		}
		perURL, err := json.Marshal(map[string]any{wiringURLB: map[string]any{"profile_id": profileIDs[1-step], "settings": urlSettings, "metarr": urlMetarr}})
		if err != nil {
			t.Fatal(err)
		}
		form.Set("url_settings", string(perURL))
		form.Set("name", value("name", false))
		form.Set("urls", wiringURLA+" "+wiringURLB)
		form.Set("auth_details", value("auth_details", false))
		form.Set("notifications", value("notifications", false))

		var w *httptest.ResponseRecorder
		if step == 0 {
			w = sendForm(t, http.MethodPost, "/api/v1/channels/add", form)
		} else {
			w = sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form)
		}
		if w.Code >= 300 {
			t.Fatalf("%s: saving = %d %s", page, w.Code, w.Body.String())
		}
		if step == 0 {
			if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", value("name", false)).Scan(&id); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", "") })
		}

		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		check := func(what, key, sent string, read any) {
			t.Helper()
			if g := formValue(read); !sameFormValue(sent, g) {
				t.Errorf("%s: %s %q was sent as %q but reads back as %q", page, what, key, sent, g)
			}
		}
		for _, k := range settingsKeys {
			if k == "channel_config_file" {
				check("channel field", k, value(k, false), got.ChannelConfigFile)
				continue
			}
			check("setting", k, value(k, false), got.Settings[k])
		}
		for _, k := range metarrKeys {
			check("Metarr setting", k, value(k, false), got.Metarr[k])
		}
		for _, k := range urlSettingsFields {
			check("per-URL setting", k, value(k, true), got.URLSettings[wiringURLB].Settings[k])
		}
		for _, k := range urlMetarrFields {
			check("per-URL Metarr setting", k, value(k, true), got.URLSettings[wiringURLB].Metarr[k])
		}
		check("channel field", "name", value("name", false), got.Name)
		if got.ProfileID != profileIDs[step] {
			t.Errorf("%s: the channel's profile was sent as %d but reads back as %d", page, profileIDs[step], got.ProfileID)
		}
		if p := got.URLSettings[wiringURLB].ProfileID; p != profileIDs[1-step] {
			t.Errorf("%s: URL B's profile was sent as %d but reads back as %d", page, profileIDs[1-step], p)
		}
		if !sameSet(got.URLs, []string{wiringURLA, wiringURLB}) {
			t.Errorf("%s: URLs read back as %v", page, got.URLs)
		}
		if want := notificationKeys(value("notifications", false)); !sameSet(notificationKeys(got.Notifications), want) {
			t.Errorf("%s: notifications were sent as %q but read back as %+v", page, want, got.Notifications)
		}
		var sentAuth []map[string]string
		if err := json.Unmarshal([]byte(value("auth_details", false)), &sentAuth); err != nil {
			t.Fatal(err)
		}
		if len(got.AuthDetails) != 1 || got.AuthDetails[0].ChannelURL != wiringURLA || got.AuthDetails[0].Username != sentAuth[0]["username"] ||
			got.AuthDetails[0].LoginURL != sentAuth[0]["login_url"] || !got.AuthDetails[0].HasPassword {
			t.Errorf("%s: login was sent as %v but reads back as %+v", page, sentAuth, got.AuthDetails)
		}
	}
	for key := range samples {
		if !used[key] {
			t.Errorf("formSamples has %q, which no channel page field uses", key)
		}
	}
}

// profileDetails is a profile as the profiles page lists it.
type profileDetails struct {
	ID       int64          `json:"id"`
	Name     string         `json:"name"`
	Settings map[string]any `json:"settings"`
	Metarr   map[string]any `json:"metarr"`
}

// TestProfileFormRoundTrip adds a profile the way the profiles page does, then edits it, with every field filled, and
// checks each value reads back.
func TestProfileFormRoundTrip(t *testing.T) {
	setupFuzzServer(t)
	samples := formSamples(t)
	src := readBuiltPage(t, "profiles.html")
	settingsKeys := objectKeys(t, src, "function emptyProfileForm()", "settings: {")
	metarrKeys := objectKeys(t, src, "function emptyProfileForm()", "metarr: {")

	// Every model key has a field, and every field a model key, other than the channel-only ones the page hides.
	bs := bindings(src)
	for _, model := range []struct {
		prefix string
		keys   []string
		hidden []string
	}{
		{"formData.settings.", settingsKeys, []string{"paused", "channel_config_file"}},
		{"formData.metarr.", metarrKeys, nil},
	} {
		bound := boundKeys(bs, model.prefix)
		for k := range bound {
			if !slices.Contains(model.keys, k) && !slices.Contains(model.hidden, k) {
				t.Errorf("profiles.html: a field is bound to %s%s, which isn't in that model, so it never loads or sends", model.prefix, k)
			}
		}
		for _, k := range model.keys {
			if _, ok := bound[k]; !ok {
				t.Errorf("profiles.html: model key %s%s has no field", model.prefix, k)
			}
		}
	}
	reportDiff(t, "profiles.html", "settings model", settingsKeys, "settings the server returns", without(jsonTags(settingsResponse{}), "paused"))
	reportDiff(t, "profiles.html", "Metarr model", metarrKeys, "Metarr settings the server returns", jsonTags(metarrArgsResponse{}))

	var id int64
	for step, name := range []string{"Wiring Profile A", "Wiring Profile B"} {
		value := func(key string) string {
			s, ok := samples[key]
			if !ok {
				t.Fatalf("profiles.html: field %q has no value in formSamples: add one so its wiring is tested", key)
			}
			return s[step]
		}

		// Built as save builds it.
		form := url.Values{"name": {name}}
		for _, k := range slices.Concat(settingsKeys, metarrKeys) {
			if v := value(k); v != "" {
				form.Set(k, v)
			}
		}
		var w *httptest.ResponseRecorder
		if step == 0 {
			w = sendForm(t, http.MethodPost, "/api/v1/profiles/add", form)
		} else {
			w = sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/profiles/%d/update", id), form)
		}
		if w.Code >= 300 {
			t.Fatalf("%s: saving = %d %s", name, w.Code, w.Body.String())
		}
		if step == 0 {
			if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", name).Scan(&id); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/profiles/%d/delete", id), "", "") })
		}

		var profiles []profileDetails
		fetchJSON(t, "/api/v1/profiles/all", &profiles)
		i := slices.IndexFunc(profiles, func(p profileDetails) bool { return p.ID == id })
		if i < 0 {
			t.Fatalf("%s: saved profile %d isn't listed", name, id)
		}
		got := profiles[i]
		if got.Name != name {
			t.Errorf("profile name was sent as %q but reads back as %q", name, got.Name)
		}
		for _, k := range settingsKeys {
			if g := formValue(got.Settings[k]); !sameFormValue(value(k), g) {
				t.Errorf("%s: setting %q was sent as %q but reads back as %q", name, k, value(k), g)
			}
		}
		for _, k := range metarrKeys {
			if g := formValue(got.Metarr[k]); !sameFormValue(value(k), g) {
				t.Errorf("%s: Metarr setting %q was sent as %q but reads back as %q", name, k, value(k), g)
			}
		}
	}
}

// TestEditChangesURLPassword tests that a new password entered on the edit page replaces the saved one.
func TestEditChangesURLPassword(t *testing.T) {
	setupFuzzServer(t)
	base := url.Values{
		"name":                 {"Password Change"},
		"urls":                 {"https://example.com/@password"},
		"video_directory":      {filepath.Join(fuzzServer.dir, "videos")},
		"download_concurrency": {"1"}, "crawl_freq": {"30"}, "download_retries": {"1"},
	}
	authFor := func(pass string) string {
		return `[{"channel_url":"https://example.com/@password","username":"user","password":"` + pass + `","login_url":"https://example.com/login"}]`
	}
	form := base
	form.Set("auth_details", authFor("old-pass"))
	if w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form); w.Code != http.StatusCreated {
		t.Fatalf("adding = %d %s", w.Code, w.Body.String())
	}
	defer deleteChannelNamed(t, "Password Change")

	cs, err := repo.GetChannelStore(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	saved := func() string {
		c, _, err := cs.GetChannelModel(consts.QChanName, "Password Change", false)
		if err != nil {
			t.Fatal(err)
		}
		return c.URLModels[0].Password
	}
	if got := saved(); got != "old-pass" {
		t.Fatalf("saved password = %q, want old-pass", got)
	}

	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", "Password Change").Scan(&id); err != nil {
		t.Fatal(err)
	}
	edit := func(pass string) {
		form.Set("auth_details", authFor(pass))
		if w := sendForm(t, http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id), form); w.Code != http.StatusOK {
			t.Fatalf("editing = %d %s", w.Code, w.Body.String())
		}
	}
	edit("new-pass")
	if got := saved(); got != "new-pass" {
		t.Errorf("after entering a new password, the saved one is %q", got)
	}
	edit("") // Passwords aren't sent to the edit page, so a blank one keeps the saved one.
	if got := saved(); got != "new-pass" {
		t.Errorf("after saving with a blank password, the saved one is %q", got)
	}
}

// ---- Other page requests ----

// pageRequest is a request a page makes, other than the channel forms' add and edit.
type pageRequest struct {
	path   string             // As written in the page, with ${...} parts as {}.
	keys   []string           // Body or query keys the page sends, which must appear near the path in a page using it.
	replay func(t *testing.T) // Sends the request as the page does and checks the server used it. Nil when another test covers it.
}

var (
	apiPathRe      = regexp.MustCompile("[`'\"](/api/v1/[^`'\"]*)[`'\"]")
	templatePartRe = regexp.MustCompile(`\$\{[^}]*\}`)
)

// keysNearPath reports whether some quoted occurrence of path in src has every key nearby, as a whole word.
func keysNearPath(src, path string, keys []string) bool {
	for _, loc := range regexp.MustCompile("[`'\"]"+regexp.QuoteMeta(path)+"[`'\"]").FindAllStringIndex(src, -1) {
		window := src[max(0, loc[0]-1200):min(len(src), loc[1]+800)]
		if !slices.ContainsFunc(keys, func(k string) bool {
			return !regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(k) + `([^\w-]|$)`).MatchString(window)
		}) {
			return true
		}
	}
	return false
}

// routed returns a replay checking the API itself answers a request with want (0 for any status below 500), rather
// than it falling through to the page server.
func routed(method, path string, want int) func(t *testing.T) {
	return func(t *testing.T) {
		w := sendRequest(t, method, path, "", "")
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || (want != 0 && w.Code != want) || w.Code >= 500 {
			t.Errorf("%s %s = %d %q %s, want %d from the API", method, path, w.Code, w.Header().Get("Content-Type"), w.Body.String(), want)
		}
	}
}

// readsJSON returns a replay checking a GET answers with JSON.
func readsJSON(path string) func(t *testing.T) {
	return func(t *testing.T) {
		var v any
		fetchJSON(t, path, &v)
	}
}

// strictJSON returns a replay sending body, then checking an unknown key added to it is refused.
func strictJSON(method, path, body string, want int) func(t *testing.T) {
	return func(t *testing.T) {
		if w := sendRequest(t, method, path, "application/json", body); w.Code != want {
			t.Errorf("%s %s with %s = %d %s, want %d", method, path, body, w.Code, w.Body.String(), want)
		}
		checkUnknownKeyRefused(t, method, path, body)
	}
}

// checkUnknownKeyRefused checks a JSON body with an unknown key added is refused, naming the key, so a renamed key
// can't be silently dropped.
func checkUnknownKeyRefused(t *testing.T, method, path, body string) {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	fields["unknown_key"] = 1
	if w := sendJSON(t, method, path, fields); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unknown_key") {
		t.Errorf("%s %s with an unknown key = %d %s, want 400 naming it", method, path, w.Code, w.Body.String())
	}
}

// limitSetting returns a replay setting a concurrency limit as the pages do, reading it back, then restoring it.
func limitSetting(path string) func(t *testing.T) {
	return func(t *testing.T) {
		var before, after struct {
			Limit int `json:"limit"`
		}
		fetchJSON(t, path, &before)
		if w := sendForm(t, http.MethodPut, path, url.Values{"limit": {"7"}}); w.Code != http.StatusOK {
			t.Fatalf("setting %s = %d %s", path, w.Code, w.Body.String())
		}
		fetchJSON(t, path, &after)
		if after.Limit != 7 {
			t.Errorf("%s set to 7 reads back as %d", path, after.Limit)
		}
		sendForm(t, http.MethodPut, path, url.Values{"limit": {strconv.Itoa(before.Limit)}})
	}
}

// fuzzChannelURLID returns the ID of the shared test channel's URL.
func fuzzChannelURLID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := fuzzServer.db.QueryRow("SELECT id FROM channel_urls WHERE channel_id = 1 AND url = 'https://example.com/@fuzz'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// setScrapeConfig sets the scrape config file as the Site Rules page does, cleared when the test ends.
func setScrapeConfig(t *testing.T) string {
	t.Helper()
	path := wiringFile(t, "scrape-rules.toml", "[[sites]]\ndomain = \"wiring.example\"\n\n  [[sites.selectors]]\n  field = \"title\"\n  selector = \"h1\"\n")
	if w := sendForm(t, http.MethodPut, "/api/v1/settings/scrape-config-file", url.Values{"path": {path}}); w.Code != http.StatusOK {
		t.Fatalf("setting the scrape config file = %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { sendForm(t, http.MethodPut, "/api/v1/settings/scrape-config-file", url.Values{"path": {""}}) })
	return path
}

// pageRequests lists every request the pages make, with how to replay it.
func pageRequests() []pageRequest {
	const (
		get, post, put, del = http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete
	)
	return []pageRequest{
		// Login requests, replayed against the real login manager in serverauth.
		{path: "/api/v1/auth/login", keys: []string{"username", "password"}},
		{path: "/api/v1/auth/logout"},
		{path: "/api/v1/auth/setup", keys: []string{"username", "password"}},
		{path: "/api/v1/auth/status"},
		{path: "/api/v1/settings/auth", keys: []string{"auth_mode", "skip_login", "allowed_hosts", "trusted_proxies"}},
		{path: "/api/v1/settings/auth/api-key"},
		{path: "/api/v1/settings/auth/password", keys: []string{"current_password", "username", "new_password"}},

		// Channel forms, replayed by TestChannelFormRoundTrip.
		{path: "/api/v1/channels/add"},
		{path: "/api/v1/channels/{}/update"},

		// Profile form, replayed by TestProfileFormRoundTrip.
		{path: "/api/v1/profiles/add", keys: []string{"name"}},
		{path: "/api/v1/profiles/{}/update", keys: []string{"name"}},
		{path: "/api/v1/profiles/all", replay: readsJSON("/api/v1/profiles/all")},
		{path: "/api/v1/profiles/{}/delete", replay: func(t *testing.T) {
			if w := sendForm(t, post, "/api/v1/profiles/add", url.Values{"name": {"Wiring Delete"}}); w.Code != http.StatusCreated {
				t.Fatalf("adding a profile = %d %s", w.Code, w.Body.String())
			}
			var profileID int64
			if err := fuzzServer.db.QueryRow("SELECT id FROM profiles WHERE name = ?", "Wiring Delete").Scan(&profileID); err != nil {
				t.Fatal(err)
			}
			if w := sendRequest(t, del, fmt.Sprintf("/api/v1/profiles/%d/delete", profileID), "", ""); w.Code != http.StatusNoContent {
				t.Fatalf("deleting a profile = %d %s", w.Code, w.Body.String())
			}
			var count int
			if err := fuzzServer.db.QueryRow("SELECT COUNT(*) FROM profiles WHERE id = ?", profileID).Scan(&count); err != nil || count != 0 {
				t.Errorf("deleted profile still saved (count %d, %v)", count, err)
			}
		}},

		{path: "/api/v1/blocked-domains", replay: readsJSON("/api/v1/blocked-domains")},
		{path: "/api/v1/blocked-domains/{}?context={}", keys: []string{"context"}, replay: func(t *testing.T) {
			if err := blocking.BlockDomain(fuzzServer.db, "wiring.example", vars.BlockContextCookie); err != nil {
				t.Fatal(err)
			}
			if w := sendRequest(t, del, "/api/v1/blocked-domains/wiring.example?context=cookie", "", ""); w.Code != http.StatusOK {
				t.Fatalf("unblocking = %d %s", w.Code, w.Body.String())
			}
			if blocked, _, _ := blocking.IsBlocked("wiring.example", vars.BlockContextCookie); blocked {
				t.Error("domain is still blocked")
			}
		}},
		{path: "/api/v1/blocked-domains/{}", replay: func(t *testing.T) {
			for _, blockCtx := range []vars.BlockContext{vars.BlockContextCookie, vars.BlockContextAuth} {
				if err := blocking.BlockDomain(fuzzServer.db, "wiring.example", blockCtx); err != nil {
					t.Fatal(err)
				}
			}
			if w := sendRequest(t, del, "/api/v1/blocked-domains/wiring.example", "", ""); w.Code != http.StatusOK {
				t.Fatalf("unblocking all contexts = %d %s", w.Code, w.Body.String())
			}
			if _, ok := blocking.GetAllBlockedDomains()["wiring.example"]; ok {
				t.Error("domain is still blocked")
			}
		}},
		{path: "/api/v1/channels/{}/{}", keys: []string{"stop-queue", "abort"}, replay: func(t *testing.T) {
			routed(post, "/api/v1/channels/1/stop-queue", http.StatusConflict)(t) // Routed, with nothing running.
			routed(post, "/api/v1/channels/1/abort", http.StatusConflict)(t)
		}},
		{path: "/api/v1/channels/{}", replay: readsJSON("/api/v1/channels/1")},
		{path: "/api/v1/channels/activity", replay: readsJSON("/api/v1/channels/activity")},
		{path: "/api/v1/channels/add-from-directory", keys: []string{"add_from_config_dir"}, replay: func(t *testing.T) {
			path := wiringFile(t, filepath.Join("config-dir", "channel.yaml"), "channel-name: Wiring From Folder\nchannel-urls:\n  - https://example.com/@wiring-folder\nvideo-directory: "+filepath.Join(fuzzServer.dir, "videos")+"\n")
			if w := sendForm(t, post, "/api/v1/channels/add-from-directory", url.Values{"add_from_config_dir": {filepath.Dir(path)}}); w.Code != http.StatusCreated {
				t.Fatalf("adding from a folder = %d %s", w.Code, w.Body.String())
			}
			deleteChannelNamed(t, "Wiring From Folder")
		}},
		{path: "/api/v1/channels/add-from-file", keys: []string{"add_from_config_file"}, replay: func(t *testing.T) {
			path := wiringFile(t, "channel.yaml", "channel-name: Wiring From File\nchannel-urls:\n  - https://example.com/@wiring-file\nvideo-directory: "+filepath.Join(fuzzServer.dir, "videos")+"\n")
			if w := sendForm(t, post, "/api/v1/channels/add-from-file", url.Values{"add_from_config_file": {path}}); w.Code != http.StatusCreated {
				t.Fatalf("adding from a file = %d %s", w.Code, w.Body.String())
			}
			deleteChannelNamed(t, "Wiring From File")
		}},
		{path: "/api/v1/channels/all", replay: readsJSON("/api/v1/channels/all")},
		{path: "/api/v1/channels/{}/all-videos", replay: readsJSON("/api/v1/channels/1/all-videos")},
		{path: "/api/v1/channels/{}/override-ops/{}?video={}", keys: []string{"video"}, replay: func(t *testing.T) {
			readsJSON(fmt.Sprintf("/api/v1/channels/1/override-ops/%d", fuzzChannelURLID(t)))(t)
		}},
		{path: "/api/v1/videos/{}/thumbnail", replay: routed(post, "/api/v1/videos/999999/thumbnail", http.StatusNotFound)}, // Uploads: TestThumbnailUpload.
		{path: "/api/v1/channels/{}/override-ops/{}/file?{}", keys: []string{"box", "path"}, replay: func(t *testing.T) {
			path := wiringFile(t, "meta-ops.txt", "album:set:Wiring\n")
			readsJSON(fmt.Sprintf("/api/v1/channels/1/override-ops/%d/file?box=meta_ops&path=%s", fuzzChannelURLID(t), url.QueryEscape(path)))(t)
		}},
		{path: "/api/v1/channels/{}/available", keys: []string{"action", "video_ids", "overrides", "metarr", "auth"}, replay: func(t *testing.T) {
			vs := repo.GetVideoStore(fuzzServer.db)
			const videoURL = "https://example.com/v/wiring-available"
			if _, err := vs.AddAvailableVideos([]*models.Video{{ChannelURLID: fuzzChannelURLID(t), ChannelURL: "https://example.com/@fuzz", URL: videoURL, Title: "Wiring"}}, 1); err != nil {
				t.Fatal(err)
			}
			var videoID int64
			if err := fuzzServer.db.QueryRow("SELECT id FROM videos WHERE url = ?", videoURL).Scan(&videoID); err != nil {
				t.Fatal(err)
			}
			strictJSON(post, "/api/v1/channels/1/available", fmt.Sprintf(`{"action":"dismiss","video_ids":[%d]}`, videoID), http.StatusAccepted)(t)
			var status string
			if err := fuzzServer.db.QueryRow("SELECT status FROM downloads WHERE video_id = ?", videoID).Scan(&status); err != nil || status != string(consts.DLStatusIgnored) {
				t.Errorf("dismissed video's status = %q (%v), want %q", status, err, consts.DLStatusIgnored)
			}
		}},
		{path: "/api/v1/channels/{}/crawl", keys: []string{"url_ids"}, replay: routed(post, "/api/v1/channels/999999/crawl", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/delete", replay: routed(del, "/api/v1/channels/999999/delete", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/delete-videos", keys: []string{"urls[]", "delete_files"}, replay: func(t *testing.T) {
			vs := repo.GetVideoStore(fuzzServer.db)
			cuID := fuzzChannelURLID(t)
			addFinished := func(videoURL string) string {
				path := wiringFile(t, filepath.Base(videoURL)+".mp4", "video")
				v := &models.Video{ChannelURLID: cuID, ChannelURL: "https://example.com/@fuzz", URL: videoURL, Title: "Wiring", VideoFilePath: path}
				v.MarkVideoAsComplete()
				if _, err := vs.AddVideo(v, 1, cuID); err != nil {
					t.Fatal(err)
				}
				if err := vs.UpdateVideo(v, 1); err != nil {
					t.Fatal(err)
				}
				return path
			}
			deleteVideo := func(videoURL string, deleteFiles bool) {
				form := url.Values{"urls[]": {videoURL}}
				if deleteFiles {
					form.Set("delete_files", "true")
				}
				if w := sendForm(t, del, "/api/v1/channels/1/delete-videos", form); w.Code != http.StatusOK {
					t.Fatalf("deleting %s = %d %s", videoURL, w.Code, w.Body.String())
				}
			}
			kept := addFinished("https://example.com/v/wiring-keep")
			deleteVideo("https://example.com/v/wiring-keep", false)
			if _, err := os.Stat(kept); err != nil {
				t.Errorf("deleting a video without delete_files removed its file: %v", err)
			}
			removed := addFinished("https://example.com/v/wiring-remove")
			deleteVideo("https://example.com/v/wiring-remove", true)
			if _, err := os.Stat(removed); !os.IsNotExist(err) {
				t.Errorf("deleting a video with delete_files kept its file (stat error: %v)", err)
			}
		}},
		{path: "/api/v1/channels/{}/download-urls", keys: []string{"videos", "url", "channel_url_id", "auth"}, replay: func(t *testing.T) {
			w := sendJSON(t, post, "/api/v1/channels/1/download-urls", map[string]any{"videos": []models.ManualVideo{{URL: "not a url"}}})
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not a url") {
				t.Errorf("downloading an invalid URL = %d %s, want 400 naming it", w.Code, w.Body.String())
			}
			badAuth := []models.ManualVideo{{URL: "https://example.com/v/auth", Auth: &models.ManualAuth{Username: "user", LoginURL: "not a url"}}}
			if w := sendJSON(t, post, "/api/v1/channels/1/download-urls", map[string]any{"videos": badAuth}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "authentication") {
				t.Errorf("downloading with an invalid login URL = %d %s, want 400", w.Code, w.Body.String())
			}
			goodAuth := []models.ManualVideo{{URL: "https://example.com/v/auth", Auth: &models.ManualAuth{Username: "user", LoginURL: "https://example.com/login"}}}
			if w := sendJSON(t, post, "/api/v1/channels/1/available-urls", map[string]any{"videos": goodAuth}); w.Code != http.StatusBadRequest {
				t.Errorf("sending a video with authentication to Available = %d %s, want 400 as it isn't stored", w.Code, w.Body.String())
			}
			checkUnknownKeyRefused(t, post, "/api/v1/channels/1/download-urls", `{"videos":[{"url":"not a url"}]}`)
		}},
		{path: "/api/v1/channels/{}/available-urls", keys: []string{"videos", "url", "channel_url_id"}, replay: func(t *testing.T) {
			const videoURL = "https://example.com/v/wiring-sent"
			body := fmt.Sprintf(`{"videos":[{"url":%q,"channel_url_id":%d}]}`, videoURL, fuzzChannelURLID(t))
			var got struct {
				Videos   []models.Video `json:"videos"`
				Existing []string       `json:"existing"`
			}
			for i, want := range []int{1, 0} { // Sending it again finds it already in the channel.
				w := sendRequest(t, post, "/api/v1/channels/1/available-urls", "application/json", body)
				if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil || len(got.Videos) != want || len(got.Existing) != 1-want {
					t.Fatalf("sending to Available (try %d) = %d %s, want %d added", i+1, w.Code, w.Body.String(), want)
				}
			}
			var status string
			if err := fuzzServer.db.QueryRow("SELECT d.status FROM downloads d JOIN videos v ON v.id = d.video_id WHERE v.url = ?", videoURL).Scan(&status); err != nil || status != string(consts.DLStatusAvailable) {
				t.Errorf("sent video's status = %q (%v), want %q", status, err, consts.DLStatusAvailable)
			}
			checkUnknownKeyRefused(t, post, "/api/v1/channels/1/available-urls", body)
		}},
		{path: "/api/v1/channels/{}/ignore-crawl", replay: routed(post, "/api/v1/channels/999999/ignore-crawl", http.StatusNotFound)},
		{path: "/api/v1/channels/{}/latest-downloads", replay: readsJSON("/api/v1/channels/1/latest-downloads")},
		{path: "/api/v1/channels/{}/notification-seen", replay: routed(post, "/api/v1/channels/1/notification-seen", http.StatusOK)},
		{path: "/api/v1/channels/{}/run-metarr", keys: []string{"video_ids", "overrides", "metarr", "rerun"}, replay: strictJSON(post, "/api/v1/channels/999999/run-metarr", `{"video_ids":[1]}`, http.StatusNotFound)},
		{path: "/api/v1/channels/{}/stop-metarr", keys: []string{"video_ids"}, replay: strictJSON(post, "/api/v1/channels/1/stop-metarr", `{"video_ids":[999999]}`, http.StatusConflict)},
		{path: "/api/v1/channels/{}/toggle-pause", replay: func(t *testing.T) {
			var first, second struct {
				Paused bool `json:"paused"`
			}
			for _, v := range []any{&first, &second} {
				w := sendRequest(t, post, "/api/v1/channels/1/toggle-pause", "", "")
				if w.Code != http.StatusOK {
					t.Fatalf("toggling pause = %d %s", w.Code, w.Body.String())
				}
				if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
					t.Fatal(err)
				}
			}
			if first.Paused == second.Paused {
				t.Error("toggling pause twice didn't change it")
			}
		}},
		{path: "/api/v1/downloads", replay: readsJSON("/api/v1/downloads")},
		{path: "/api/v1/downloads/cancel", keys: []string{"video_ids"}, replay: strictJSON(post, "/api/v1/downloads/cancel", `{"video_ids":[999999]}`, http.StatusNotFound)},
		{path: "/api/v1/logs", replay: routed(get, "/api/v1/logs", 0)},
		{path: "/api/v1/logs/level", replay: readsJSON("/api/v1/logs/level")},
		{path: "/api/v1/logs/level/{}", replay: func(t *testing.T) {
			var before, after struct {
				Level int `json:"level"`
			}
			fetchJSON(t, "/api/v1/logs/level", &before)
			if w := sendRequest(t, post, "/api/v1/logs/level/3", "", ""); w.Code != http.StatusOK {
				t.Fatalf("setting the log level = %d %s", w.Code, w.Body.String())
			}
			fetchJSON(t, "/api/v1/logs/level", &after)
			if after.Level != 3 {
				t.Errorf("log level set to 3 reads back as %d", after.Level)
			}
			sendRequest(t, post, fmt.Sprintf("/api/v1/logs/level/%d", before.Level), "", "")
		}},
		{path: "/api/v1/logs/metarr", replay: routed(get, "/api/v1/logs/metarr", http.StatusOK)},
		{path: "/api/v1/post-processing", replay: readsJSON("/api/v1/post-processing")},
		{path: "/api/v1/settings/crawl-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/crawl-concurrency")},
		{path: "/api/v1/settings/global-download-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/global-download-concurrency")},
		{path: "/api/v1/settings/post-process-concurrency", keys: []string{"limit"}, replay: limitSetting("/api/v1/settings/post-process-concurrency")},
		{path: "/api/v1/settings/domain-limits", keys: []string{"hostname", "download", "crawl"}, replay: func(t *testing.T) {
			if w := sendForm(t, put, "/api/v1/settings/domain-limits", url.Values{"hostname": {"media.wiring-limit.example"}, "download": {"2"}, "crawl": {"1"}}); w.Code != http.StatusOK {
				t.Fatalf("setting a domain limit = %d %s", w.Code, w.Body.String())
			}
			defer sendRequest(t, del, "/api/v1/settings/domain-limits/wiring-limit.example", "", "")
			var limits map[string]models.DomainLimit
			fetchJSON(t, "/api/v1/settings/domain-limits", &limits)
			if got := limits["wiring-limit.example"]; got != (models.DomainLimit{Download: 2, Crawl: 1}) {
				t.Errorf("domain limit reads back as %+v", got)
			}
		}},
		{path: "/api/v1/settings/domain-limits/{}", replay: func(t *testing.T) {
			sendForm(t, put, "/api/v1/settings/domain-limits", url.Values{"hostname": {"media.wiring-remove.example"}, "download": {"2"}})
			if w := sendRequest(t, del, "/api/v1/settings/domain-limits/media.wiring-remove.example", "", ""); w.Code != http.StatusOK {
				t.Fatalf("removing a domain limit = %d %s", w.Code, w.Body.String())
			}
			var limits map[string]models.DomainLimit
			fetchJSON(t, "/api/v1/settings/domain-limits", &limits)
			if _, ok := limits["wiring-remove.example"]; ok {
				t.Error("removing a domain limit by the name it was entered as left it saved")
			}
		}},
		{path: "/api/v1/settings/flaresolverr-url", keys: []string{"url"}, replay: func(t *testing.T) {
			if w := sendForm(t, put, "/api/v1/settings/flaresolverr-url", url.Values{"url": {"http://localhost:8191"}}); w.Code != http.StatusOK {
				t.Fatalf("setting the FlareSolverr URL = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/flaresolverr-url", url.Values{"url": {""}})
			var got struct {
				URL string `json:"url"`
			}
			fetchJSON(t, "/api/v1/settings/flaresolverr-url", &got)
			if got.URL != "http://localhost:8191" {
				t.Errorf("FlareSolverr URL reads back as %q", got.URL)
			}
		}},
		{path: "/api/v1/settings/flaresolverr-url/test", keys: []string{"url"}, replay: func(t *testing.T) {
			// Nothing listens on port 1, so the test fails at connecting: proof the URL was read.
			if w := sendForm(t, post, "/api/v1/settings/flaresolverr-url/test", url.Values{"url": {"http://127.0.0.1:1"}}); w.Code != http.StatusBadGateway {
				t.Errorf("testing an unreachable FlareSolverr = %d %s, want 502", w.Code, w.Body.String())
			}
		}},
		{path: "/api/v1/settings/scrape-config-file", keys: []string{"path"}, replay: func(t *testing.T) {
			path := setScrapeConfig(t)
			var got struct {
				Path string `json:"path"`
			}
			fetchJSON(t, "/api/v1/settings/scrape-config-file", &got)
			if got.Path != path {
				t.Errorf("scrape config file reads back as %q, want %q", got.Path, path)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/content", keys: []string{"content", "version"}, replay: func(t *testing.T) {
			setScrapeConfig(t)
			var current struct {
				Content string `json:"content"`
				Version string `json:"version"`
			}
			fetchJSON(t, "/api/v1/settings/scrape-config-file/content", &current)
			if w := sendForm(t, put, "/api/v1/settings/scrape-config-file/content", url.Values{"content": {current.Content}, "version": {current.Version}}); w.Code != http.StatusOK {
				t.Errorf("saving the scrape config = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, put, "/api/v1/settings/scrape-config-file/content", url.Values{"content": {current.Content}, "version": {"stale"}}); w.Code != http.StatusConflict {
				t.Errorf("saving the scrape config with a stale version = %d, want 409", w.Code)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/reload", replay: routed(post, "/api/v1/settings/scrape-config-file/reload", http.StatusOK)},
		{path: "/api/v1/settings/scrape-config-file/sites", replay: readsJSON("/api/v1/settings/scrape-config-file/sites")},
		{path: "/api/v1/settings/scrape-config-file/test", keys: []string{"url"}, replay: func(t *testing.T) {
			// No rules match this address, so nothing is fetched.
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test", url.Values{"url": {"http://127.0.0.1:1/video"}}); w.Code != http.StatusOK {
				t.Errorf("testing scrape rules = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test", nil); w.Code != http.StatusBadRequest {
				t.Errorf("testing scrape rules without a URL = %d, want 400", w.Code)
			}
		}},
		{path: "/api/v1/settings/scrape-config-file/test-crawl", keys: []string{"url"}, replay: func(t *testing.T) {
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test-crawl", url.Values{"url": {"http://127.0.0.1:1/channel"}}); w.Code != http.StatusOK {
				t.Errorf("testing crawl rules = %d %s", w.Code, w.Body.String())
			}
			if w := sendForm(t, post, "/api/v1/settings/scrape-config-file/test-crawl", nil); w.Code != http.StatusBadRequest {
				t.Errorf("testing crawl rules without a URL = %d, want 400", w.Code)
			}
		}},
		{path: "/api/v1/settings/tls", keys: []string{"cert", "key"}, replay: func(t *testing.T) {
			if os.Getenv(consts.EnvWebTLSCert) != "" {
				t.Skip("certificate set by " + consts.EnvWebTLSCert)
			}
			pem, err := generateSelfSigned("localhost")
			if err != nil {
				t.Fatal(err)
			}
			path := wiringFile(t, "cert.pem", string(pem))
			if w := sendForm(t, put, "/api/v1/settings/tls", url.Values{"cert": {path}, "key": {""}}); w.Code != http.StatusOK {
				t.Fatalf("setting the certificate = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/tls", url.Values{"cert": {""}, "key": {""}})
			var got struct {
				Cert string `json:"cert"`
			}
			fetchJSON(t, "/api/v1/settings/tls", &got)
			if got.Cert != path {
				t.Errorf("certificate reads back as %q, want %q", got.Cert, path)
			}
		}},
		{path: "/api/v1/settings/tls/enabled", keys: []string{"enabled"}, replay: func(t *testing.T) {
			if os.Getenv(consts.EnvWebHTTPS) != "" {
				t.Skip("HTTPS set by " + consts.EnvWebHTTPS)
			}
			if w := sendForm(t, put, "/api/v1/settings/tls/enabled", url.Values{"enabled": {"true"}}); w.Code != http.StatusOK {
				t.Fatalf("turning HTTPS on = %d %s", w.Code, w.Body.String())
			}
			defer sendForm(t, put, "/api/v1/settings/tls/enabled", url.Values{"enabled": {"false"}})
			var got struct {
				Enabled bool `json:"enabled"`
			}
			fetchJSON(t, "/api/v1/settings/tls", &got)
			if !got.Enabled {
				t.Error("HTTPS turned on reads back as off")
			}
		}},
		{path: "/api/v1/videos/available", replay: readsJSON("/api/v1/videos/available")},
		{path: "/api/v1/videos/available?ignored=1", replay: readsJSON("/api/v1/videos/available?ignored=1")},
	}
}

// TestPageRequestsWired tests that every API request the pages make is listed in pageRequests, sends the keys listed
// with it, and reaches a route that uses them.
func TestPageRequestsWired(t *testing.T) {
	setupFuzzServer(t)

	found := map[string][]string{} // Path -> pages requesting it.
	sources := map[string]string{}
	for name, src := range webPages(t) {
		src = templatePartRe.ReplaceAllString(src, "{}")
		sources[name] = src
		for _, m := range apiPathRe.FindAllStringSubmatch(src, -1) {
			if !slices.Contains(found[m[1]], name) {
				found[m[1]] = append(found[m[1]], name)
			}
		}
	}

	requests := pageRequests()
	listed := map[string]bool{}
	for _, r := range requests {
		listed[r.path] = true
	}
	for path, names := range found {
		if !listed[path] {
			t.Errorf("%v request %s, which pageRequests doesn't list: add it so its wiring is tested", names, path)
		}
	}

	for _, r := range requests {
		names, ok := found[r.path]
		if !ok {
			t.Errorf("pageRequests lists %s, which no page requests", r.path)
			continue
		}
		if len(r.keys) > 0 && !slices.ContainsFunc(names, func(n string) bool { return keysNearPath(sources[n], r.path, r.keys) }) {
			t.Errorf("%v don't send all of %v with %s", names, r.keys, r.path)
		}
		if r.replay != nil {
			t.Run(strings.TrimPrefix(r.path, "/api/v1/"), r.replay)
		}
	}
}
