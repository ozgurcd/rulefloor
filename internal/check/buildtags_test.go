package check

import "testing"

func TestValidateBuildTags(t *testing.T) {
	for _, tags := range []string{"", "integration", "integration,linux", "go1.27", "feature_2"} {
		if err := ValidateBuildTags(tags); err != nil {
			t.Fatalf("ValidateBuildTags(%q): %v", tags, err)
		}
	}
	for _, tags := range []string{",integration", "integration,", "integration,,linux", "integration linux", "integration;echo", "../../escape", "-race"} {
		if err := ValidateBuildTags(tags); err == nil {
			t.Fatalf("ValidateBuildTags(%q) accepted invalid input", tags)
		}
	}
}
