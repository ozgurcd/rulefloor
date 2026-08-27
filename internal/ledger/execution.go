package ledger

import "github.com/ozgurcd/rulefloor/internal/model"

type ExecutionPolicy = model.ExecutionPolicy
type ReachabilityPolicy = model.ReachabilityPolicy
type Binding = model.Binding

const (
	ExecutionStatic   = model.ExecutionStatic
	ExecutionExecute  = model.ExecutionExecute
	ReachabilityExact = model.ReachabilityExact
)

func InterpretBinding(row *Row) (Binding, error) { return model.InterpretBinding(row) }
