package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/jsonkeys"
	"tubarr/internal/domain/paths"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/repo"
)

// TestThumbnailUpload tests uploading, serving and cleaning up Download As thumbnails.
func TestThumbnailUpload(t *testing.T) {
	setupFuzzServer(t)
	oldDir := paths.ThumbnailDir
	paths.ThumbnailDir = filepath.Join(t.TempDir(), "thumbnails")
	t.Cleanup(func() { paths.ThumbnailDir = oldDir })

	form := url.Values{
		"name":                           {"Thumbnail Channel"},
		"urls":                           {"https://example.com/@thumbs"},
		jsonkeys.SettingsVideoDirectory:  {filepath.Join(fuzzServer.dir, "videos")},
		jsonkeys.SettingsJSONDirectory:   {filepath.Join(fuzzServer.dir, "json")},
		jsonkeys.SettingsMaxConcurrency:  {"1"},
		jsonkeys.SettingsCrawlFreq:       {"30"},
		jsonkeys.SettingsDownloadRetries: {"1"},
		jsonkeys.SettingsAutoDownload:    {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	fuzzServer.handler.ServeHTTP(w, req)
	if w.Code >= 300 {
		t.Fatalf("adding channel: %d %s", w.Code, w.Body.String())
	}
	s, err := repo.InitStores(fuzzServer.db)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := s.ChannelStore().GetChannelModel(consts.QChanName, "Thumbnail Channel", true)
	if err != nil {
		t.Fatal(err)
	}
	videos := []*models.Video{{ChannelURLID: c.URLModels[0].ID, ChannelURL: c.URLModels[0].URL, URL: "https://example.com/v/thumb"}}
	if _, err := s.VideoStore().AddAvailableVideos(videos, c.ID); err != nil {
		t.Fatal(err)
	}
	videoID := videos[0].ID

	upload := func(id int64, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("thumbnail", "cover")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/videos/%d/thumbnail", id), &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, req)
		return w
	}
	savedPath := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		var got struct{ Path string }
		if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		return got.Path
	}

	png := savedPath(upload(videoID, []byte("\x89PNG\r\n\x1a\n rest of image")))
	if png != filepath.Join(paths.ThumbnailDir, fmt.Sprintf("%d.png", videoID)) {
		t.Errorf("PNG saved to %q", png)
	}
	jpg := savedPath(upload(videoID, []byte("\xFF\xD8\xFF\xE0 rest of image")))
	if _, err := os.Stat(png); !os.IsNotExist(err) {
		t.Errorf("earlier PNG %q kept after a JPEG replaced it", png)
	}
	if w := upload(videoID, []byte("not an image")); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text upload: got %d, want 415", w.Code)
	}
	if w := upload(999999, []byte("\xFF\xD8\xFF\xE0")); w.Code != http.StatusNotFound {
		t.Errorf("upload for an unknown video: got %d, want 404", w.Code)
	}

	// A thumbnail must exist, and can't be stripped at the same time.
	for name, lines := range map[string]models.OverrideLines{
		"missing file":     {Thumbnail: filepath.Join(paths.ThumbnailDir, "nope.png")},
		"set and stripped": {Thumbnail: jpg, StripThumbnail: true},
	} {
		if _, err := parseOverrideLines(&lines, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	o, err := parseOverrideLines(&models.OverrideLines{Thumbnail: jpg}, nil)
	if err != nil || o.Thumbnail != jpg {
		t.Fatalf("uploaded thumbnail override = %+v, %v", o, err)
	}

	// Only thumbnails a video's overrides use are kept.
	if err := s.VideoStore().SetVideoOverrides([]int64{videoID}, o); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(paths.ThumbnailDir, "424242.png")
	if err := os.WriteFile(orphan, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	overrides, err := s.VideoStore().ThumbnailOverrides()
	if err != nil {
		t.Fatal(err)
	}
	file.RemoveUnusedThumbnails(overrides)
	if _, err := os.Stat(jpg); err != nil {
		t.Errorf("thumbnail in use was removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("unused thumbnail was kept")
	}

	// The thumbnail is served for the pages, which are told the video has one, and a stripped one isn't shown.
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		fuzzServer.handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	if w := get(fmt.Sprintf("/api/v1/videos/%d/thumbnail", videoID)); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("getting the thumbnail: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	shownThumbnail := func() models.Video {
		t.Helper()
		var listed []models.AvailableVideo
		if err := json.Unmarshal(get("/api/v1/videos/available").Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		for _, v := range listed {
			if v.ID == videoID {
				return v.Video
			}
		}
		t.Fatalf("video %d isn't listed as available", videoID)
		return models.Video{}
	}
	if v := shownThumbnail(); !v.CustomThumbnail {
		t.Errorf("video with a custom thumbnail listed without custom_thumbnail: %+v", v)
	}
	if err := s.VideoStore().SetVideoOverrides([]int64{videoID}, &models.VideoOverrides{StripThumbnail: true}); err != nil {
		t.Fatal(err)
	}
	if v := shownThumbnail(); v.CustomThumbnail || v.ThumbnailURL != "" {
		t.Errorf("video with a stripped thumbnail still shows one: %+v", v)
	}
	if w := get(fmt.Sprintf("/api/v1/videos/%d/thumbnail", videoID)); w.Code != http.StatusNotFound {
		t.Errorf("getting a stripped thumbnail: %d, want 404", w.Code)
	}
}
