package parsing

import "testing"

// TestConvertConfigValueRejectsMapsAndLists tests that a map or list given for a string setting is rejected, rather than
// turned into text like "map[a:1]" (which a directory setting would then create as a folder).
func TestConvertConfigValueRejectsMapsAndLists(t *testing.T) {
	for _, v := range []any{map[string]any{"a": 1}, []any{"x"}, nil} {
		if got, ok := convertConfigValue[string](v); ok {
			t.Errorf("convertConfigValue[string](%#v) = %q, want rejected", v, got)
		}
	}
	if _, ok := convertConfigValue[[]string]([]any{"a", map[string]any{"b": 1}}); ok {
		t.Error("expected a list containing a map to be rejected as []string")
	}
	for v, want := range map[any]string{"dir": "dir", 123: "123", true: "true", 1.5: "1.5"} {
		if got, ok := convertConfigValue[string](v); !ok || got != want {
			t.Errorf("convertConfigValue[string](%#v) = %q, %v, want %q", v, got, ok, want)
		}
	}
}
