package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const ledgerFile = "RULE-FLOOR.md"

var headerCells = []string{"ID", "one-sentence rule", "enforced-by", "check", "red-proof", "hash"}

var (
	idRe      = regexp.MustCompile(`^[A-Z][A-Z0-9-]{0,30}[0-9]$`)
	hashRe    = regexp.MustCompile(`^[0-9a-f]{12}$`)
	sepCellRe = regexp.MustCompile(`^:?-{3,}:?$`)
)

// Row is one rule in the ledger. Check is "NONE" for declared-only rules,
// otherwise "<file> @ <profile>". Hash is "-" for declared rules, otherwise
// the first 12 hex chars of the sha256 of the tagged test body.
type Row struct {
	ID         string
	Rule       string
	EnforcedBy string
	Check      string
	RedProof   string
	Hash       string
}

func (r *Row) armed() bool { return r.Check != "NONE" }

type Ledger struct {
	Floor int
	Rows  []Row
	// RedProofs is the RED-PROOFS ratchet: the floor for the number of
	// armed rows whose red-proof cell is a real proof (not "-").
	// Monotonic like Floor — nothing the tool does lowers it.
	// HasRedProofs is false for a legacy ledger that has not adopted the
	// header yet (see `rulefloor redproofs --adopt`); every write
	// operation adopts it too, measuring the current count.
	RedProofs    int
	HasRedProofs bool
}

func ledgerPath(repo string) string { return filepath.Join(repo, ledgerFile) }

func (l *Ledger) find(id string) *Row {
	for i := range l.Rows {
		if l.Rows[i].ID == id {
			return &l.Rows[i]
		}
	}
	return nil
}

// raiseFloor lifts FLOOR to n. It never lowers it.
func (l *Ledger) raiseFloor(n int) {
	if n > l.Floor {
		l.Floor = n
	}
}

// measuredRedProofs counts armed rows whose red-proof cell carries a real
// proof — anything but the "-" placeholder. The tool cannot judge the
// text's quality; it ratchets the count.
func (l *Ledger) measuredRedProofs() int {
	n := 0
	for _, r := range l.Rows {
		if r.armed() && r.RedProof != "-" {
			n++
		}
	}
	return n
}

// maintainRedProofs adopts the RED-PROOFS header (legacy ledgers) or
// raises it to the measured count. It never lowers it: a hand-raised
// value stays, and check fails until the measurement catches up —
// exactly the FLOOR discipline.
func (l *Ledger) maintainRedProofs() {
	m := l.measuredRedProofs()
	if !l.HasRedProofs {
		l.RedProofs = m
		l.HasRedProofs = true
		return
	}
	if m > l.RedProofs {
		l.RedProofs = m
	}
}

func loadLedger(repo string) (*Ledger, error) {
	p := ledgerPath(repo)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fatalf("cannot read %s: %v (run \"rulefloor init\"?)", p, err)
	}
	l, perr := parseLedger(string(data))
	if perr != nil {
		return nil, fatalf("%s: %v", p, perr)
	}
	return l, nil
}

func saveLedger(repo string, l *Ledger) error {
	p := ledgerPath(repo)
	if err := os.WriteFile(p, []byte(l.serialize()), 0o644); err != nil {
		return fatalf("cannot write %s: %v", p, err)
	}
	return nil
}

func (l *Ledger) serialize() string {
	var b strings.Builder
	fmt.Fprintf(&b, "FLOOR: %d\n", l.Floor)
	if l.HasRedProofs {
		fmt.Fprintf(&b, "RED-PROOFS: %d\n", l.RedProofs)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "| %s |\n", strings.Join(headerCells, " | "))
	fmt.Fprintf(&b, "|%s\n", strings.Repeat("---|", len(headerCells)))
	for _, r := range l.Rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			r.ID, r.Rule, r.EnforcedBy, r.Check, r.RedProof, r.Hash)
	}
	return b.String()
}

func parseLedger(data string) (*Ledger, error) {
	l := &Ledger{}
	stage := 0 // 0=FLOOR line, 1=header, 2=separator, 3=rows
	seen := map[string]bool{}
	for i, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		ln := i + 1
		switch stage {
		case 0:
			num, ok := strings.CutPrefix(line, "FLOOR: ")
			if !ok {
				return nil, fmt.Errorf("line %d: expected \"FLOOR: N\", got %q", ln, line)
			}
			n, err := strconv.Atoi(strings.TrimSpace(num))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("line %d: invalid FLOOR value %q", ln, num)
			}
			l.Floor = n
			stage = 1
		case 1:
			// The optional RED-PROOFS ratchet line sits between FLOOR and
			// the table header. Absent on legacy ledgers.
			if num, ok := strings.CutPrefix(line, "RED-PROOFS: "); ok {
				if l.HasRedProofs {
					return nil, fmt.Errorf("line %d: duplicate RED-PROOFS line", ln)
				}
				n, err := strconv.Atoi(strings.TrimSpace(num))
				if err != nil || n < 0 {
					return nil, fmt.Errorf("line %d: invalid RED-PROOFS value %q", ln, num)
				}
				l.RedProofs = n
				l.HasRedProofs = true
				continue
			}
			cells, err := splitRowLine(line)
			if err != nil || len(cells) != len(headerCells) {
				return nil, fmt.Errorf("line %d: malformed header", ln)
			}
			for j := range cells {
				if cells[j] != headerCells[j] {
					return nil, fmt.Errorf("line %d: malformed header (column %d is %q, want %q)",
						ln, j+1, cells[j], headerCells[j])
				}
			}
			stage = 2
		case 2:
			cells, err := splitRowLine(line)
			if err != nil || len(cells) != len(headerCells) {
				return nil, fmt.Errorf("line %d: malformed separator", ln)
			}
			for _, c := range cells {
				if !sepCellRe.MatchString(c) {
					return nil, fmt.Errorf("line %d: malformed separator", ln)
				}
			}
			stage = 3
		case 3:
			cells, err := splitRowLine(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: malformed row: %v", ln, err)
			}
			if len(cells) != len(headerCells) {
				return nil, fmt.Errorf("line %d: malformed row: %d fields, want %d",
					ln, len(cells), len(headerCells))
			}
			for j, c := range cells {
				if c == "" {
					return nil, fmt.Errorf("line %d: missing field %q", ln, headerCells[j])
				}
			}
			r := Row{cells[0], cells[1], cells[2], cells[3], cells[4], cells[5]}
			if !idRe.MatchString(r.ID) {
				return nil, fmt.Errorf("line %d: invalid rule ID %q", ln, r.ID)
			}
			if seen[r.ID] {
				return nil, fmt.Errorf("line %d: duplicate rule ID %q", ln, r.ID)
			}
			seen[r.ID] = true
			if r.armed() {
				if _, _, err := splitCheck(r.Check); err != nil {
					return nil, fmt.Errorf("line %d: %v", ln, err)
				}
				if !hashRe.MatchString(r.Hash) {
					return nil, fmt.Errorf("line %d: invalid hash %q (want 12 hex chars)", ln, r.Hash)
				}
			} else if r.Hash != "-" {
				return nil, fmt.Errorf("line %d: declared row %s must have hash \"-\", got %q", ln, r.ID, r.Hash)
			}
			l.Rows = append(l.Rows, r)
		}
	}
	if stage != 3 {
		return nil, fmt.Errorf("truncated ledger: FLOOR line, header, and separator are required")
	}
	return l, nil
}

func splitRowLine(line string) ([]string, error) {
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil, fmt.Errorf("row must start and end with '|'")
	}
	parts := strings.Split(line, "|")
	cells := parts[1 : len(parts)-1]
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.TrimSpace(c)
	}
	return out, nil
}

// splitCheck parses "<file> @ <profile>" and validates the file path stays
// inside the repo.
func splitCheck(spec string) (file, profile string, err error) {
	if strings.Count(spec, " @ ") != 1 {
		return "", "", fmt.Errorf("check %q must be \"<file> @ <profile>\"", spec)
	}
	file, profile, _ = strings.Cut(spec, " @ ")
	file = strings.TrimSpace(file)
	profile = strings.TrimSpace(profile)
	if file == "" || profile == "" {
		return "", "", fmt.Errorf("check %q must be \"<file> @ <profile>\"", spec)
	}
	if !filepath.IsLocal(file) {
		return "", "", fmt.Errorf("check file %q must be a relative path inside the repo", file)
	}
	return file, profile, nil
}

// validCell rejects values that would corrupt the table.
func validCell(s, what string) error {
	if strings.TrimSpace(s) == "" {
		return fatalf("%s must not be empty", what)
	}
	if strings.ContainsAny(s, "|\n\r") {
		return fatalf("%s must be a single line without '|'", what)
	}
	return nil
}
