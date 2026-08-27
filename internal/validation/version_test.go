package validation

import "testing"

func TestVersionResultProvenance(t *testing.T) {
	tests := []struct {
		name      string
		stamp     string
		toolchain string
		disagrees bool
	}{
		{name: "release agreement", stamp: "v0.8.0", toolchain: "v0.8.0"},
		{name: "development agreement", stamp: "dev", toolchain: "(devel)"},
		{name: "wrong release stamp", stamp: "v9.9.9", toolchain: "v0.8.0", disagrees: true},
		{name: "release claim on tarball build", stamp: "v0.8.0", toolchain: "(devel)", disagrees: true},
		{name: "unavailable build info", stamp: "dev", toolchain: "(unknown)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := versionResult(test.stamp, test.toolchain)
			if result.Version != test.stamp || result.ToolchainVersion != test.toolchain || result.VersionDisagreement != test.disagrees {
				t.Fatalf("version result = %#v", result)
			}
		})
	}
}
