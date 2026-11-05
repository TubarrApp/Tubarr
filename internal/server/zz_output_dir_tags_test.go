package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/models"
)

// TestOutputDirectoryTagsOnSave tests which directories accept the output directory tags when a channel is saved.
func TestOutputDirectoryTagsOnSave(t *testing.T) {
	setupFuzzServer(t)
	dir := t.TempDir()
	const (
		channelTag = "{{channel_output_directory}}/Shorts"
		urlTag     = "{{channel_url_output_directory}}/Shorts"
		year       = "/{{year}}"
	)
	type fields = map[string]any

	tests := []struct {
		name        string
		video       string
		jsonDir     string
		output      string
		urlSettings fields
		want        bool
	}{
		// Video and JSON directories accept neither tag, nor a Metarr tag.
		{name: "channel video, channel tag", video: channelTag},
		{name: "channel video, URL tag", video: urlTag},
		{name: "channel JSON, channel tag", video: dir, jsonDir: channelTag},
		{name: "channel JSON, URL tag", video: dir, jsonDir: urlTag},
		{name: "channel JSON, Metarr tag", video: dir, jsonDir: dir + year},
		{name: "URL video, channel tag", video: dir, urlSettings: fields{"settings": fields{"video_directory": channelTag}}},
		{name: "URL video, URL tag", video: dir, urlSettings: fields{"settings": fields{"video_directory": urlTag}}},
		{name: "URL JSON, channel tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": channelTag}}},
		{name: "URL JSON, URL tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": urlTag}}},
		{name: "URL JSON, Metarr tag", video: dir, urlSettings: fields{"settings": fields{"json_directory": dir + year}}},

		// Metarr output directories accept both on a channel URL, and only the channel's on a channel.
		{name: "channel output, channel tag", video: dir, output: channelTag, want: true},
		{name: "channel output, URL tag", video: dir, output: urlTag},
		{name: "URL output, channel tag", video: dir, urlSettings: fields{"metarr": fields{"metarr_output_directory": channelTag}}, want: true},
		{name: "URL output, URL tag", video: dir, urlSettings: fields{"metarr": fields{"metarr_output_directory": urlTag}}, want: true},
		{name: "URL output, channel tag with a Metarr tag in the channel's", video: dir, output: dir + year, urlSettings: fields{"metarr": fields{"metarr_output_directory": channelTag}}, want: true},
	}
	for i, tt := range tests {
		name := fmt.Sprintf("tags%d", i)
		form := url.Values{
			"name":                           {name},
			"urls":                           {"https://example.com/@" + name},
			jsonkeys.SettingsVideoDirectory:  {tt.video},
			jsonkeys.SettingsJSONDirectory:   {tt.jsonDir},
			jsonkeys.MetarrOutputDirectory:   {tt.output},
			jsonkeys.SettingsCrawlFreq:       {"30"},
			jsonkeys.SettingsDownloadRetries: {"1"},
			jsonkeys.SettingsAutoDownload:    {"false"},
		}
		if tt.urlSettings != nil {
			b, err := json.Marshal(fields{"https://example.com/@" + name: tt.urlSettings})
			if err != nil {
				t.Fatal(err)
			}
			form.Set("url_settings", string(b))
		}
		w := sendForm(t, http.MethodPost, "/api/v1/channels/add", form)
		if saved := w.Code < 300; saved != tt.want {
			t.Errorf("%s: got %d %s, want saved: %t", tt.name, w.Code, w.Body.String(), tt.want)
		}
		if w.Code < 300 {
			deleteChannelNamed(t, name)
		}
	}
}

// TestDownloadAsOutputDirectoryTags tests only Download As's post-process directory accepts the output directory tags.
func TestDownloadAsOutputDirectoryTags(t *testing.T) {
	for _, tag := range []string{"{{channel_output_directory}}/Once", "{{channel_url_output_directory}}/Once"} {
		if _, err := parseOverrideLines(&models.OverrideLines{VideoDir: tag}, nil); err == nil {
			t.Errorf("video directory %q was accepted", tag)
		}
		if _, err := parseOverrideLines(&models.OverrideLines{JSONDir: tag}, nil); err == nil {
			t.Errorf("JSON directory %q was accepted", tag)
		}
		if o, err := parseOverrideLines(&models.OverrideLines{OutputDir: tag}, nil); err != nil || o.OutputDir != tag {
			t.Errorf("post-process directory %q: got %+v, %v", tag, o, err)
		}
	}
}
