package validation

import "testing"

func TestVersionResultProvenance(t *testing.T) {
	tests := []struct {
		name      string
		stamp     string
		toolchain string
		disagrees bool
		agreement Status
	}{
		{name: "release agreement", stamp: "v0.8.0", toolchain: "v0.8.0", agreement: StatusPass},
		{name: "development agreement", stamp: "dev", toolchain: "(devel)", agreement: StatusPass},
		{name: "wrong release stamp", stamp: "v9.9.9", toolchain: "v0.8.0", disagrees: true, agreement: StatusFail},
		{name: "release claim on tarball build", stamp: "v0.8.0", toolchain: "(devel)", disagrees: true, agreement: StatusFail},
		{name: "unavailable build info", stamp: "dev", toolchain: "(unknown)", agreement: StatusCannotEvaluate},
		// The dangerous half: a RELEASE claim with no toolchain evidence.
		// This is the exact shape of a build from an extracted source
		// archive (no VCS, no module resolution) carrying a forged or
		// stale stamp — it must read cannot_evaluate, never a pass, and
		// the v1 boolean stays false because nothing is PROVEN either way.
		{name: "release claim with unavailable build info", stamp: "v0.8.0", toolchain: "(unknown)", agreement: StatusCannotEvaluate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := versionResult(test.stamp, test.toolchain)
			if result.Version != test.stamp || result.ToolchainVersion != test.toolchain || result.VersionDisagreement != test.disagrees {
				t.Fatalf("version result = %#v", result)
			}
			if result.VersionAgreement != test.agreement {
				t.Fatalf("version agreement = %q, want %q (%#v)", result.VersionAgreement, test.agreement, result)
			}
			if result.VersionDisagreement != (result.VersionAgreement == StatusFail) {
				t.Fatalf("v1 boolean must be true exactly when agreement is fail: %#v", result)
			}
		})
	}
}
