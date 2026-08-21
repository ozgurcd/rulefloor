package main

import "github.com/ozgurcd/rulefloor/internal/ledger"

const ledgerFile = ledger.Filename

type Row = ledger.Row
type Ledger ledger.Ledger

func ledgerPath(repo string) string { return ledger.Path(repo) }

func (l *Ledger) find(id string) *Row { return (*ledger.Ledger)(l).Find(id) }

func (l *Ledger) raiseFloor(n int) { (*ledger.Ledger)(l).RaiseFloor(n) }

func (l *Ledger) measuredRedProofs() int { return (*ledger.Ledger)(l).MeasuredRedProofs() }

func (l *Ledger) maintainRedProofs() { (*ledger.Ledger)(l).MaintainRedProofs() }

func (l *Ledger) effectiveCount() int { return (*ledger.Ledger)(l).EffectiveCount() }

func (l *Ledger) isRepairedFixture(id string) bool {
	return (*ledger.Ledger)(l).IsRepairedFixture(id)
}

func loadLedger(repo string) (*Ledger, error) {
	model, err := ledger.Load(repo)
	if err != nil {
		return nil, fatalf("%v", err)
	}
	return (*Ledger)(model), nil
}

func saveLedger(repo string, model *Ledger) error {
	if err := ledger.Save(repo, (*ledger.Ledger)(model)); err != nil {
		return fatalf("%v", err)
	}
	return nil
}

func (l *Ledger) serialize() string { return (*ledger.Ledger)(l).Serialize() }

func parseLedger(data string) (*Ledger, error) {
	model, err := ledger.Parse(data)
	return (*Ledger)(model), err
}

func splitCheck(spec string) (string, string, error) { return ledger.SplitCheck(spec) }

func validCell(value, what string) error {
	if err := ledger.ValidCell(value, what); err != nil {
		return fatalf("%v", err)
	}
	return nil
}
