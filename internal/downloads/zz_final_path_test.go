package downloads

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/logger"

	"github.com/TubarrApp/gocommon/logging"
)

// TestExecuteAudioSource tests a source saved with an audio extension finishes, rather than failing once it's downloaded.
func TestExecuteAudioSource(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	audioPath := filepath.Join(t.TempDir(), "clip.m4a")
	if err := os.WriteFile(audioPath, []byte("not really audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs := fakeYTDLP(t, "echo '"+command.FinalPathPrefix+audioPath+"'")
	vd, _ := newTestDownloads(t.Context(), t, "https://audio.example/v", 3)

	if err := vd.Execute(); err != nil {
		t.Fatalf("got error %v, want a clean success", err)
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("ran yt-dlp %d times, want 1", n)
	}
	if vd.Video.Files.VideoFilePath != audioPath {
		t.Errorf("VideoFilePath = %q, want %q", vd.Video.Files.VideoFilePath, audioPath)
	}
}

// TestFinalPathLine tests only the line yt-dlp prints for the finished file is read as its path.
func TestFinalPathLine(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	for _, tc := range []struct{ name, line, want string }{
		{"video", command.FinalPathPrefix + "/videos/a [x].mp4", "/videos/a [x].mp4"},
		{"audio", command.FinalPathPrefix + "/videos/a [x].m4a", "/videos/a [x].m4a"},
		{"windows", command.FinalPathPrefix + `C:\videos\a [x].mp4`, `C:\videos\a [x].mp4`},
		{"other path output", "/tmp/other-download-output.mp4", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vd, _ := newTestDownloads(t.Context(), t, "https://example.com/v", 1)
			lines := make(chan string, 1)
			lines <- tc.line
			close(lines)
			if got := vd.scanVideoCmdOutput(lines).filename; got != tc.want {
				t.Errorf("filename = %q, want %q", got, tc.want)
			}
		})
	}
}
