package logging

import (
	"bufio"
	"os"
	"sync"
)

// ringLogWriter is a fixed-size line-ring log writer.
type ringLogWriter struct {
	path     string
	maxLines int
	mu       sync.Mutex

	lines   []string // ring buffer
	nextPos int      // next write index
	full    bool     // ring fully occupied
}

// newRingLogWriter initializes a ring writer and loads existing data.
func newRingLogWriter(path string, maxLines int) (*ringLogWriter, error) {
	w := &ringLogWriter{
		path:     path,
		maxLines: maxLines,
		lines:    make([]string, maxLines),
	}

	// Load existing lines if file exists
	if _, err := os.Stat(path); err == nil {
		if err := w.loadExisting(); err != nil {
			return nil, err
		}
	} else {
		// Create empty file
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			return nil, err
		}
	}

	return w, nil
}

// loadExisting reads existing log file lines into the ring buffer.
func (w *ringLogWriter) loadExisting() error {
	f, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	tmp := make([]string, 0, w.maxLines)

	for scanner.Scan() {
		tmp = append(tmp, scanner.Text()+"\n") // keep newline
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// Only keep last maxLines
	if len(tmp) > w.maxLines {
		tmp = tmp[len(tmp)-w.maxLines:]
	}

	// reset ring
	for i := range w.lines {
		w.lines[i] = ""
	}

	// load into ring
	copy(w.lines, tmp)

	w.nextPos = len(tmp) % w.maxLines
	w.full = len(tmp) == w.maxLines

	return nil
}

// Write implements io.Writer for zerolog.
func (w *ringLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Store line in ring buffer
	w.lines[w.nextPos] = string(p)
	w.nextPos++
	if w.nextPos == w.maxLines {
		w.nextPos = 0
		w.full = true
	}

	// Flush entire buffer to disk
	return len(p), w.flushToDisk()
}

// flushToDisk writes the ring buffer in correct order.
func (w *ringLogWriter) flushToDisk() error {
	out := make([]byte, 0, 4096)

	if w.full {
		// from nextPos..end
		for i := w.nextPos; i < w.maxLines; i++ {
			if w.lines[i] != "" {
				out = append(out, w.lines[i]...)
			}
		}
		// from start..nextPos-1
		for i := 0; i < w.nextPos; i++ {
			if w.lines[i] != "" {
				out = append(out, w.lines[i]...)
			}
		}
	} else {
		// simple linear fill
		for i := 0; i < w.nextPos; i++ {
			if w.lines[i] != "" {
				out = append(out, w.lines[i]...)
			}
		}
	}

	return os.WriteFile(w.path, out, 0644)
}
