package models

import (
	"encoding/json"
	"testing"
)

// TestVideoJSONFlat tests that a video's metadata and file fields are written at the top level, and read back.
func TestVideoJSONFlat(t *testing.T) {
	v := Video{
		ID:          5,
		ChannelName: "Chan",
		Metadata:    VideoMetadata{Title: "T", Duration: 60, Raw: map[string]any{"x": 1}},
		Files:       VideoFiles{VideoFilePath: "/v.mp4", JSONFilePath: "/v.json", VideoDir: "/dir"},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	if err := json.Unmarshal(data, &flat); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"id": 5.0, "channel_name": "Chan", "title": "T", "duration": 60.0, "video_path": "/v.mp4", "json_path": "/v.json"} {
		if flat[key] != want {
			t.Errorf("%s = %v, want %v in %s", key, flat[key], want, data)
		}
	}
	for _, key := range []string{"Metadata", "Files", "Raw", "VideoDir"} {
		if _, ok := flat[key]; ok {
			t.Errorf("%s written in %s", key, data)
		}
	}

	var got Video
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || got.ChannelName != "Chan" || got.Metadata.Title != "T" || got.Metadata.Duration != 60 || got.Files.VideoFilePath != "/v.mp4" || got.Files.JSONFilePath != "/v.json" {
		t.Errorf("read back %+v", got)
	}
}

// TestAvailableVideoJSON tests that an available video keeps its crawl retries alongside the flat video fields.
func TestAvailableVideoJSON(t *testing.T) {
	v := AvailableVideo{
		Video:        Video{ID: 7, Metadata: VideoMetadata{Title: "T"}},
		CrawlRetries: CrawlRetries{RetriesOnCrawl: true, CrawlDLRetries: 2, CrawlDLRetriesLeft: 1},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	if err := json.Unmarshal(data, &flat); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"id": 7.0, "title": "T", "retries_on_crawl": true, "crawl_dl_retries_left": 1.0} {
		if flat[key] != want {
			t.Errorf("%s = %v, want %v in %s", key, flat[key], want, data)
		}
	}

	var got AvailableVideo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Metadata.Title != "T" || !got.RetriesOnCrawl || got.CrawlDLRetriesLeft != 1 || got.CrawlDLRetries != 0 {
		t.Errorf("read back %+v", got)
	}
}
