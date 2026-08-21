package ledger

import "testing"

func TestLegacyExecutionPolicyCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		row     Row
		policy  ExecutionPolicy
		profile string
	}{
		{
			name:   "go unit executes by default",
			row:    Row{EnforcedBy: "go-test", Check: "rule_test.go @ unit"},
			policy: ExecutionExecute, profile: "unit",
		},
		{
			name:   "go non-unit remains explicit-run",
			row:    Row{EnforcedBy: "go-test", Check: "rule_test.go @ needs-db"},
			policy: ExecutionStatic, profile: "needs-db",
		},
		{
			name:   "profile name does not grant playwright execution",
			row:    Row{EnforcedBy: "playwright", Check: "e2e/rule.spec.ts @ unit"},
			policy: ExecutionStatic, profile: "unit",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := InterpretBinding(&test.row)
			if err != nil {
				t.Fatal(err)
			}
			if binding.Execution != test.policy || binding.Profile != test.profile || binding.File == "" || !binding.Legacy {
				t.Fatalf("binding = %+v", binding)
			}
		})
	}
}
