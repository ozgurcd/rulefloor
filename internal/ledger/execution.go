package ledger

type ExecutionPolicy string

const (
	ExecutionStatic  ExecutionPolicy = "static"
	ExecutionExecute ExecutionPolicy = "execute"
)

type Binding struct {
	File      string
	Kind      string
	Profile   string
	Execution ExecutionPolicy
	Legacy    bool
}

// InterpretBinding preserves the historical ledger contract while exposing
// execution as an explicit internal policy. The legacy profile-name rule is
// confined here; checking and validation consume Binding.Execution instead.
func InterpretBinding(row *Row) (Binding, error) {
	file, profile, err := SplitCheck(row.Check)
	if err != nil {
		return Binding{}, err
	}
	binding := Binding{
		File:      file,
		Kind:      row.EnforcedBy,
		Profile:   profile,
		Execution: ExecutionStatic,
		Legacy:    true,
	}
	if row.EnforcedBy == "go-test" && profile == "unit" {
		binding.Execution = ExecutionExecute
	}
	return binding, nil
}
