package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
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

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// TestThumbnailUpload checks thumbnail uploading works.
func TestThumbnailUpload(t *testing.T) {
	setupFuzzServer(t)
	oldDir := paths.UploadedThumbnailDir
	paths.UploadedThumbnailDir = filepath.Join(t.TempDir(), "thumbnails")
	t.Cleanup(func() { paths.UploadedThumbnailDir = oldDir })

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

	encodeImage := func(t *testing.T, format string, width, height int) []byte {
		t.Helper()

		img := image.NewRGBA(image.Rect(0, 0, width, height))
		var buf bytes.Buffer
		var err error

		switch format {
		case "png":
			err = png.Encode(&buf, img)
		case "jpg":
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
		case "gif":
			err = gif.Encode(&buf, img, nil)
		default:
			t.Fatalf("unsupported test image format %q", format)
		}
		if err != nil {
			t.Fatal(err)
		}

		return buf.Bytes()
	}

	upload := func(id int64, filename string, data []byte) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("thumbnail", filename)
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

	// Valid PNG, GIF and JPEG uploads must succeed.
	pngData := encodeImage(t, "png", 2, 2)
	pngPath := savedPath(upload(videoID, "cover.png", pngData))
	if pngPath != filepath.Join(paths.UploadedThumbnailDir, fmt.Sprintf("%d.png", videoID)) {
		t.Errorf("PNG saved to %q", pngPath)
	}

	gifData := encodeImage(t, "gif", 2, 2)
	gifPath := savedPath(upload(videoID, "cover.gif", gifData))
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("earlier PNG %q kept after a GIF replaced it", pngPath)
	}
	if gifPath != filepath.Join(paths.UploadedThumbnailDir, fmt.Sprintf("%d.gif", videoID)) {
		t.Errorf("GIF saved to %q", gifPath)
	}

	jpgData := encodeImage(t, "jpg", 2, 2)
	jpg := savedPath(upload(videoID, "cover.jpg", jpgData))
	if _, err := os.Stat(gifPath); !os.IsNotExist(err) {
		t.Errorf("earlier GIF %q kept after a JPEG replaced it", gifPath)
	}
	if jpg != filepath.Join(paths.UploadedThumbnailDir, fmt.Sprintf("%d.jpg", videoID)) {
		t.Errorf("JPEG saved to %q", jpg)
	}

	// Invalid image data must fail.
	if w := upload(videoID, "cover.png", []byte("not an image")); w.Code != http.StatusBadRequest {
		t.Errorf("text upload: got %d, want 400", w.Code)
	}

	// The file extension must match the actual image format.
	if w := upload(videoID, "cover.jpg", pngData); w.Code != http.StatusBadRequest {
		t.Errorf("mismatched image extension: got %d, want 400", w.Code)
	}

	// Unsupported extensions must fail.
	for _, filename := range []string{"cover.webp", "cover.bmp", "cover"} {
		t.Run("unsupported extension "+filename, func(t *testing.T) {
			if w := upload(videoID, filename, pngData); w.Code != http.StatusBadRequest {
				t.Errorf("upload with filename %q: got %d, want 400", filename, w.Code)
			}
		})
	}

	// Images exceeding the maximum file size must fail.
	t.Run("maximum filesize", func(t *testing.T) {
		data := encodeImage(t, "png", 2, 2)
		data = append(data, make([]byte, sharedconsts.ImageUploadMaxBytes-len(data)+1)...)

		if w := upload(videoID, "cover.png", data); w.Code != http.StatusBadRequest {
			t.Errorf("oversized upload: got %d, want 400", w.Code)
		}
	})

	// Images exceeding the pixel limit must fail.
	t.Run("maximum pixels", func(t *testing.T) {
		width := uint32(sharedconsts.ImageUploadMaxPixels + 1)

		// Build a PNG header with an oversized width and a valid IHDR CRC.
		data := []byte{
			0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
			0x00, 0x00, 0x00, 0x0d,
			'I', 'H', 'D', 'R',
			byte(width >> 24), byte(width >> 16), byte(width >> 8), byte(width),
			0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00,
		}
		crc := crc32.ChecksumIEEE(data[12:])
		data = append(data,
			byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc),
		)

		if w := upload(videoID, "cover.png", data); w.Code != http.StatusBadRequest {
			t.Errorf("oversized image dimensions: got %d, want 400", w.Code)
		}
	})

	if w := upload(999999, "cover.jpg", jpgData); w.Code != http.StatusNotFound {
		t.Errorf("upload for an unknown video: got %d, want 404", w.Code)
	}

	// A thumbnail must exist, and can't be stripped at the same time.
	for name, lines := range map[string]models.OverrideLines{
		"missing file":     {Thumbnail: filepath.Join(paths.UploadedThumbnailDir, "nope.png")},
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
	orphan := filepath.Join(paths.UploadedThumbnailDir, "424242.png")
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
