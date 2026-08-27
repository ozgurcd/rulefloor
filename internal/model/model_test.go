package model

import "testing"

func TestExplicitExecutionPolicyOverridesLegacyProfileMeaning(t *testing.T) {
	tests := []struct {
		name       string
		row        Row
		want       ExecutionPolicy
		wantLegacy bool
	}{
		{name: "legacy unit", row: Row{EnforcedBy: "go-test", Check: "rule_test.go @ unit"}, want: ExecutionExecute, wantLegacy: true},
		{name: "legacy non-unit", row: Row{EnforcedBy: "go-test", Check: "rule_test.go @ integration"}, want: ExecutionStatic, wantLegacy: true},
		{name: "explicit static unit", row: Row{EnforcedBy: "go-test", Check: "rule_test.go @ unit", ExecutionPolicy: ExecutionStatic}, want: ExecutionStatic},
		{name: "explicit execute non-unit", row: Row{EnforcedBy: "go-test", Check: "rule_test.go @ integration", ExecutionPolicy: ExecutionExecute}, want: ExecutionExecute},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := InterpretBinding(&test.row)
			if err != nil {
				t.Fatal(err)
			}
			if binding.Execution != test.want || binding.Legacy != test.wantLegacy {
				t.Fatalf("binding = %+v, want execution=%s legacy=%t", binding, test.want, test.wantLegacy)
			}
		})
	}
}

func TestInvalidPersistedExecutionPolicyIsRejected(t *testing.T) {
	_, err := InterpretBinding(&Row{
		EnforcedBy: "go-test", Check: "rule_test.go @ unit", ExecutionPolicy: "sometimes",
	})
	if err == nil {
		t.Fatal("invalid persisted execution policy was accepted")
	}
}
