package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// channelFormBools are the channel page fields set by checkboxes and dropdowns, which can't be blank or hold free text.
var channelFormBools = []string{"paused", "use_global_cookies", "auto_download"}

// channelFormFields are a channel page's field names, read from the page.
type channelFormFields struct {
	settings, metarr       []string
	urlSettings, urlMetarr []string
	urlNumbers             map[string]bool
}

// readChannelFormFields reads a channel page's field names.
func readChannelFormFields(t *testing.T, page string) channelFormFields {
	t.Helper()
	src := readWebFile(t, page)
	return channelFormFields{
		settings:    objectKeys(t, src, "formData: {", "settings: {"),
		metarr:      objectKeys(t, src, "formData: {", "metarr: {"),
		urlSettings: slices.Concat(arrayStrings(t, src, "settingsFields"), perURLFixedKeys),
		urlMetarr:   arrayStrings(t, src, "metarrFields"),
		urlNumbers:  boundKeys(bindings(src), "urlEntry.custom."),
	}
}

// fuzzedChannelForm is a channel form built from fuzz input, with the value each field was sent as.
type fuzzedChannelForm struct {
	form    url.Values
	sent    map[string]string // Main form fields, by key.
	sentURL map[string]string // Per-URL fields for wiringURLB, by key.
	junk    map[string]bool   // Fields sent the fuzzed text, whose read back value isn't checked.
}

// buildFuzzedChannelForm builds a form as the page's submitForm does, each field taking from choices one of: blank, the
// first sample, the second sample, or junk.
func buildFuzzedChannelForm(f channelFormFields, samples map[string][2]string, choices []byte, junk string) fuzzedChannelForm {
	out := fuzzedChannelForm{form: url.Values{}, sent: map[string]string{}, sentURL: map[string]string{}, junk: map[string]bool{}}
	i := 0
	pick := func(key string) (string, bool) {
		c := byte(0)
		if len(choices) > 0 {
			c = choices[i%len(choices)]
		}
		i++
		s := samples[key]
		if slices.Contains(channelFormBools, key) {
			return s[c%2], false
		}
		switch c % 4 {
		case 1:
			return s[0], false
		case 2:
			return s[1], false
		case 3:
			return junk, true
		}
		return "", false
	}

	for _, k := range slices.Concat(f.settings, f.metarr) {
		v, isJunk := pick(k)
		out.sent[k], out.junk[k] = v, isJunk
		switch {
		case k == "auto_download":
			out.form.Set(k, v)
		case k == "paused" || k == "use_global_cookies":
			if v == "true" {
				out.form.Set(k, v)
			}
		case v != "":
			out.form.Set(k, v)
		}
	}

	urlSettings, urlMetarr := map[string]any{}, map[string]any{}
	for _, k := range slices.Concat(f.urlSettings, f.urlMetarr) {
		v, isJunk := pick(k)
		if v == "" {
			continue // The page leaves out blank per-URL fields, so they inherit the channel's.
		}
		out.sentURL[k], out.junk["url:"+k] = v, isJunk
		if slices.Contains(f.urlSettings, k) {
			urlSettings[k] = urlSettingValue(k, v, f.urlNumbers[k])
		} else {
			urlMetarr[k] = urlSettingValue(k, v, f.urlNumbers[k])
		}
	}
	perURL, _ := json.Marshal(map[string]any{wiringURLB: map[string]any{"settings": urlSettings, "metarr": urlMetarr}})
	out.form.Set("url_settings", string(perURL))

	for _, k := range []string{"name", "auth_details", "notifications"} {
		v, isJunk := pick(k)
		out.sent[k], out.junk[k] = v, isJunk
		if v != "" {
			out.form.Set(k, v)
		}
	}
	out.form.Set("urls", wiringURLA+" "+wiringURLB)
	return out
}

// editPageForm builds the form the edit page sends when a channel is loaded and saved without changes.
func editPageForm(f channelFormFields, got channelDetails) url.Values {
	form := url.Values{}
	for _, k := range f.settings {
		v := got.Settings[k]
		if k == "channel_config_file" {
			v = got.ChannelConfigFile
		}
		switch s := formValue(v); {
		case k == "auto_download":
			form.Set(k, strconv.FormatBool(s != "false"))
		case k == "paused" || k == "use_global_cookies":
			if s == "true" {
				form.Set(k, s)
			}
		case s != "":
			form.Set(k, s)
		}
	}
	for _, k := range f.metarr {
		if s := formValue(got.Metarr[k]); s != "" && (k != "metarr_max_cpu_usage" || s != "0") {
			form.Set(k, s)
		}
	}

	perURL := map[string]any{}
	for u, sections := range got.URLSettings {
		settings, metarr := map[string]any{}, map[string]any{}
		for k, v := range sections["settings"] {
			if v != nil && v != "" && slices.Contains(f.urlSettings, k) {
				settings[k] = v
			}
		}
		for k, v := range sections["metarr"] {
			switch {
			case v == nil || v == "" || !slices.Contains(f.urlMetarr, k):
			case k == "metarr_max_cpu_usage" && v == float64(0):
			case k == "metarr_transcode_video_transcode_codecs" || k == "metarr_transcode_audio_codecs":
				metarr[k] = formValue(v) // The page shows Metarr codec lists as text, and sends them so.
			default:
				metarr[k] = v
			}
		}
		if len(settings) > 0 || len(metarr) > 0 {
			perURL[u] = map[string]any{"settings": settings, "metarr": metarr}
		}
	}
	data, _ := json.Marshal(perURL)
	form.Set("url_settings", string(data))

	var auth []map[string]string
	for _, a := range got.AuthDetails {
		if a.Username != "" && a.LoginURL != "" {
			auth = append(auth, map[string]string{"channel_url": a.ChannelURL, "username": a.Username, "password": "", "login_url": a.LoginURL})
		}
	}
	if len(auth) > 0 {
		data, _ = json.Marshal(auth)
		form.Set("auth_details", string(data))
	}
	form.Set("name", got.Name)
	form.Set("urls", strings.Join(got.URLs, " "))
	if len(got.Notifications) > 0 {
		data, _ = json.Marshal(got.Notifications)
		form.Set("notifications", string(data))
	}
	return form
}

// shownValue renders a value read back as the edit page shows it, where an unset Metarr CPU limit reads back as 0.
func shownValue(key string, v any) string {
	s := formValue(v)
	if key == "metarr_max_cpu_usage" && s == "0" {
		return ""
	}
	return s
}

// FuzzChannelForm saves fuzzed combinations of channel page fields, as the add or edit page sends them, and checks:
// nothing panics or fails with a server error, a rejected save changes nothing, an accepted save reads back as sent
// (a blank field reading back blank), and loading the channel on the edit page then saving it unchanged is accepted
// and changes nothing.
func FuzzChannelForm(f *testing.F) {
	for _, edit := range []bool{false, true} {
		for _, choices := range [][]byte{{0}, {1}, {2}, {0, 1}, {0, 2}, {1, 2}, {2, 1, 0}, {3, 1}, {3, 2, 1, 0}} {
			for _, junk := range []string{"", " ", "0", "-1", "abc", "99999999999999999999", "a|b", "{{year}}", "<x>", "\n\n", "título"} {
				f.Add(edit, choices, junk)
			}
		}
	}
	f.Fuzz(func(t *testing.T, edit bool, choices []byte, junk string) {
		setupFuzzServer(t)
		if pathOutsideDir(junk) {
			return // Junk can't name a path outside the test directory.
		}
		samples := formSamples(t)
		page := "add-channel.html"
		if edit {
			page = "edit-channel.html"
		}
		fields := readChannelFormFields(t, page)
		fuzzed := buildFuzzedChannelForm(fields, samples, choices, junk)

		save := func(what string, form url.Values, id int64) (code int, body string) {
			t.Helper()
			fuzzServer.logMu.Lock()
			fuzzServer.logBuf.Reset()
			fuzzServer.logMu.Unlock()
			method, target := http.MethodPost, "/api/v1/channels/add"
			if id != 0 {
				method, target = http.MethodPut, fmt.Sprintf("/api/v1/channels/%d/update", id)
			}
			w := sendForm(t, method, target, form)
			fuzzServer.logMu.Lock()
			logged := fuzzServer.logBuf.String()
			fuzzServer.logMu.Unlock()
			if strings.Contains(logged, "panic") {
				t.Fatalf("%s panicked with %q:\n%s", what, form.Encode(), logged)
			}
			if w.Code >= 500 {
				t.Errorf("%s failed with a server error %d %s, form %q", what, w.Code, strings.TrimSpace(w.Body.String()), form.Encode())
			}
			return w.Code, strings.TrimSpace(w.Body.String())
		}
		channelID := func(name string) int64 {
			var id int64
			_ = fuzzServer.db.QueryRow("SELECT id FROM channels WHERE name = ?", name).Scan(&id)
			return id
		}
		deleteChannel := func(id int64) {
			if w := sendRequest(t, http.MethodDelete, fmt.Sprintf("/api/v1/channels/%d/delete", id), "", ""); w.Code != http.StatusNoContent {
				t.Errorf("deleting the fuzzed channel = %d %s", w.Code, w.Body.String())
			}
		}

		// The edit page edits a channel added from the first samples.
		var id int64
		if edit {
			base := buildFuzzedChannelForm(readChannelFormFields(t, "add-channel.html"), samples, []byte{1}, "")
			base.form.Set("name", "Fuzz Form Edit")
			if code, body := save("adding the channel to edit", base.form, 0); code >= 300 {
				t.Fatalf("adding the channel to edit = %d %s", code, body)
			}
			id = channelID("Fuzz Form Edit")
			defer deleteChannel(id)
		}

		before := dbSnapshot(t)
		code, body := save(page, fuzzed.form, id)
		if code >= 300 {
			if after := dbSnapshot(t); after != before {
				t.Errorf("%s: a rejected save (%d %s) still changed stored data, form %q\nbefore:\n%s\nafter:\n%s", page, code, body, fuzzed.form.Encode(), before, after)
			}
			return
		}
		if strings.TrimSpace(fuzzed.sent["video_directory"]) == "" {
			t.Errorf("%s: saving without the required video directory was accepted, form %q", page, fuzzed.form.Encode())
		}
		if !edit && strings.TrimSpace(fuzzed.sent["name"]) == "" {
			t.Errorf("%s: adding a channel without the required name was accepted, form %q", page, fuzzed.form.Encode())
		}
		if !edit {
			if id = channelID(fuzzed.sent["name"]); id == 0 {
				t.Fatalf("%s: the add was accepted but no channel is named %q", page, fuzzed.sent["name"])
			}
			defer deleteChannel(id)
		}

		// Each field reads back as sent, other than junk, which validation may normalize.
		var got channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &got)
		check := func(key, sent string, read any) {
			t.Helper()
			if g := shownValue(key, read); !sameFormValue(sent, g) {
				t.Errorf("%s: %q was sent as %q but reads back as %q, form %q", page, key, sent, g, fuzzed.form.Encode())
			}
		}
		for _, k := range fields.settings {
			if fuzzed.junk[k] {
				continue
			}
			switch {
			case k == "channel_config_file":
				check(k, fuzzed.sent[k], got.ChannelConfigFile)
			case k == "json_directory" && fuzzed.sent[k] == "":
				check(k, formValue(got.Settings["video_directory"]), got.Settings[k]) // A blank JSON directory is the video directory.
			default:
				check(k, fuzzed.sent[k], got.Settings[k])
			}
		}
		for _, k := range fields.metarr {
			if !fuzzed.junk[k] {
				check(k, fuzzed.sent[k], got.Metarr[k])
			}
		}
		for k, v := range fuzzed.sentURL {
			if fuzzed.junk["url:"+k] {
				continue
			}
			section, inherited := "metarr", got.Metarr[k]
			if slices.Contains(fields.urlSettings, k) {
				section, inherited = "settings", got.Settings[k]
			}
			// Per-URL settings matching the channel's aren't stored, so the URL then reads the channel's.
			read := got.URLSettings[wiringURLB][section][k]
			if shownValue(k, read) == "" && v != "false" { // False is always left out, so is never inherited.
				read = inherited
			}
			check("per-URL "+k, v, read)
		}
		if !fuzzed.junk["name"] && fuzzed.sent["name"] != "" && got.Name != fuzzed.sent["name"] {
			t.Errorf("%s: name was sent as %q but reads back as %q", page, fuzzed.sent["name"], got.Name)
		}
		if !fuzzed.junk["notifications"] {
			if want := notificationKeys(fuzzed.sent["notifications"]); !sameSet(notificationKeys(got.Notifications), want) {
				t.Errorf("%s: notifications were sent as %q but read back as %+v", page, want, got.Notifications)
			}
		}

		// Loading the channel on the edit page and saving it unchanged must work, and change nothing.
		resave := editPageForm(readChannelFormFields(t, "edit-channel.html"), got)
		if code, body := save("saving the edit page unchanged", resave, id); code >= 300 {
			t.Fatalf("%s: loading the saved channel on the edit page and saving it unchanged was rejected: %d %s\nform %q",
				page, code, body, resave.Encode())
		}
		var again channelDetails
		fetchJSON(t, fmt.Sprintf("/api/v1/channels/%d", id), &again)
		if !reflect.DeepEqual(got, again) {
			a, _ := json.MarshalIndent(got, "", " ")
			b, _ := json.MarshalIndent(again, "", " ")
			t.Errorf("%s: saving the edit page unchanged changed the channel:\nbefore: %s\nafter: %s", page, a, b)
		}
	})
}
