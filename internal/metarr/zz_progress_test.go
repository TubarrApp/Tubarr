package metarr

import (
	"os/exec"
	"strings"
	"testing"
)

// TestParseProgress tests Metarr's progress lines are read, with -1 for an unknown percentage, and others ignored.
func TestParseProgress(t *testing.T) {
	tests := []struct {
		line string
		want Progress
		ok   bool
	}{
		{"metarr progress: pct=25.0 frame=120 fps=29.9 speed=1.52x eta=95", Progress{Percent: 25, Frame: 120, FPS: 29.9, Speed: "1.52x", ETA: 95}, true},
		{"metarr progress: pct=-1.0 frame=200 fps=30.0 speed= eta=-1", Progress{Percent: -1, Frame: 200, FPS: 30, ETA: -1}, true},
		{"metarr progress: pct=10.0 frame=5 fps=1.0 speed=1x", Progress{Percent: 10, Frame: 5, FPS: 1, Speed: "1x", ETA: -1}, true}, // An older Metarr.
		{"final video path: /videos/a.mp4", Progress{}, false},
	}
	for _, tt := range tests {
		got, ok := parseProgress(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseProgress(%q) = %+v, %t; want %+v, %t", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

// TestRunWithProgress tests progress lines are recorded while Metarr runs and cleared after, leaving the rest of stdout.
func TestRunWithProgress(t *testing.T) {
	const videoID = 42
	script := `echo "metarr progress: pct=50.0 frame=10 fps=5.0 speed=1x"
read _
echo "final video path: /videos/a.mp4"`
	cmd := exec.Command("sh", "-c", script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var out string
	var runErr error
	go func() {
		out, runErr = runWithProgress(cmd, videoID)
		close(done)
	}()

	// Metarr waits on stdin after its progress line, so the progress is visible until it's let go.
	for VideoProgress(videoID) == nil {
		select {
		case <-done:
			t.Fatal("finished before its progress was recorded")
		default:
		}
	}
	if p := VideoProgress(videoID); p.Percent != 50 || p.Frame != 10 {
		t.Errorf("progress = %+v", p)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	<-done

	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.TrimSpace(out) != "final video path: /videos/a.mp4" {
		t.Errorf("stdout = %q, want only the path line", out)
	}
	if p := VideoProgress(videoID); p != nil {
		t.Errorf("progress %+v left after the run", p)
	}
}
