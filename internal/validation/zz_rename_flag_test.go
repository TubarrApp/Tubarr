package validation

import (
	"strings"
	"testing"
)

// TestValidateRenameFlagError tests that an invalid rename style's error names the style given and lists the valid ones.
func TestValidateRenameFlagError(t *testing.T) {
	err := ValidateRenameFlag("bogus")
	if err == nil {
		t.Fatal("accepted rename style bogus")
	}
	if msg := err.Error(); !strings.Contains(msg, `"bogus"`) || strings.Contains(msg, "map[") {
		t.Errorf("error %q should name %q and list the valid styles plainly", msg, "bogus")
	}
}
