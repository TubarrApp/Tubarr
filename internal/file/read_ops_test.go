package file

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestReadOpLinesKeepsTrailingSpaces covers an operation value ending in a space, such
// as "title:keep-after:| ", surviving being read from a file.
func TestReadOpLinesKeepsTrailingSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.txt")
	content := "  title:keep-after:| \r\n\n   \n# comment\ntitle:append:!\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadOpLines(path)
	if err != nil {
		t.Fatalf("ReadOpLines: %v", err)
	}
	if want := []string{"title:keep-after:| ", "title:append:!"}; !slices.Equal(got, want) {
		t.Errorf("ReadOpLines = %q, want %q", got, want)
	}

	if got, _ := ReadFileLines(path); got[0] != "title:keep-after:|" {
		t.Errorf("ReadFileLines should still trim, got %q", got[0])
	}
}
