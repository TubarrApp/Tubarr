package parsing_test

import (
	"fmt"
	"strings"
	"testing"
	"tubarr/internal/parsing"
)

// TestParseFilterOps tests the ParseFilterOps function for both must/any and non-must/any filters.
func TestParseFilterOps(t *testing.T) {
	// Must/Any filter test.
	mustAnyFilterOps := []string{
		"title:contains:cat:must",
		"title:omits:frogs:any",
		"date:omits:must",
		"duration:morethan:3600:must",
	}
	invalidMustAnyFilterOps := []string{
		"title:contains:cat",
		"title:omits:frogs",
		"date:omits",
		"duration:morethan:3600",
	}

	mustAnyFilters, err := parsing.ParseFilterOps(mustAnyFilterOps, true)
	if err != nil {
		t.Fatalf("ParseFilterOps returned an error: %v", err)
	}
	if _, err := parsing.ParseFilterOps(invalidMustAnyFilterOps, true); err == nil {
		t.Fatalf("ParseFilterOps should have returned an error for invalid must/any filters")
	}

	if len(mustAnyFilters) != len(mustAnyFilterOps) {
		t.Fatalf("Expected %d filters, got %d", len(mustAnyFilterOps), len(mustAnyFilters))
	}

	for i, mustAnyFilter := range mustAnyFilters {
		var actual string

		expected := mustAnyFilterOps[i]
		split := strings.Split(expected, ":")

		if len(split) == 4 {
			actual = fmt.Sprintf("%s:%s:%s:%s", mustAnyFilter.Field, mustAnyFilter.FilterType, mustAnyFilter.Value, mustAnyFilter.MustAny)
		} else {
			actual = fmt.Sprintf("%s:%s:%s", mustAnyFilter.Field, mustAnyFilter.FilterType, mustAnyFilter.MustAny)
		}
		if expected != actual {
			t.Errorf("Expected filter %q, got %q", expected, actual)
		}
	}

	// Non-Must/Any filter test.
	nonMustAnyFilterOps := []string{
		"title:contains:cat",
		"title:omits:frogs",
		"date:omits",
		"duration:morethan:3600",
	}
	invalidNonMustAnyFilterOps := []string{
		"title:contains:cat:must",
		"title:omits:frogs:any",
		"date:omits:must",
		"duration:morethan:3600:any",
	}

	noMustAnyFilters, err := parsing.ParseFilterOps(nonMustAnyFilterOps, false)
	if err != nil {
		t.Fatalf("ParseFilterOps returned an error: %v", err)
	}
	if _, err := parsing.ParseFilterOps(invalidNonMustAnyFilterOps, false); err == nil {
		t.Fatalf("ParseFilterOps should have returned an error for invalid non-must/any filters")
	}

	if len(noMustAnyFilters) != len(nonMustAnyFilterOps) {
		t.Fatalf("Expected %d filters, got %d", len(nonMustAnyFilterOps), len(noMustAnyFilters))
	}

	for i, noMustAnyFilter := range noMustAnyFilters {
		var actual string

		expected := nonMustAnyFilterOps[i]
		split := strings.Split(expected, ":")

		if len(split) == 3 {
			actual = fmt.Sprintf("%s:%s:%s", noMustAnyFilter.Field, noMustAnyFilter.FilterType, noMustAnyFilter.Value)
		} else {
			actual = fmt.Sprintf("%s:%s", noMustAnyFilter.Field, noMustAnyFilter.FilterType)
		}
		if expected != actual {
			t.Errorf("Expected filter %q, got %q", expected, actual)
		}
	}

}
