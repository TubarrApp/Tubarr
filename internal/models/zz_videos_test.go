package models

import (
	"io"
	"testing"

	"tubarr/internal/domain/logger"
)

// TestStoreFilenamesFromMetarr tests that the last reported video path wins, so a path reported mid-run survives a
// failure before the final one.
func TestStoreFilenamesFromMetarr(t *testing.T) {
	logger.Pl.Console = io.Discard

	tests := []struct {
		name, out, wantVideo, wantJSON string
		wantErr                        bool
	}{
		{"final", "final video path: /out/v.mp4\nfinal json path: /out/v.json\n", "/out/v.mp4", "/out/v.json", false},
		{"current then final", "current video path: /in/v.mp4\nfinal video path: /out/v.mp4\nfinal json path: /out/v.json\n", "/out/v.mp4", "/out/v.json", false},
		{"current only", "current video path: /in/v.mp4\n", "/in/v.mp4", "/in/v.json", false},
		{"metafile purged", "final video path: /out/v.mp4\nfinal json path: \n", "/out/v.mp4", "", false},
		{"none", "some other output\n", "/in/v.webm", "/in/v.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &Video{VideoFilePath: "/in/v.webm", JSONFilePath: "/in/v.json"}
			err := v.StoreFilenamesFromMetarr(tt.out)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if v.VideoFilePath != tt.wantVideo || v.JSONFilePath != tt.wantJSON {
				t.Errorf("paths = %q, %q; want %q, %q", v.VideoFilePath, v.JSONFilePath, tt.wantVideo, tt.wantJSON)
			}
		})
	}
}
