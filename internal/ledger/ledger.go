package ledger

import (
	"fmt"
	"github.com/ozgurcd/rulefloor/internal/repository"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const Filename = "RULE-FLOOR.md"

var headerCells = []string{"ID", "one-sentence rule", "enforced-by", "check", "red-proof", "hash"}

var (
	idRe      = regexp.MustCompile(`^[A-Z][A-Z0-9-]{0,30}[0-9]$`)
	hashRe    = regexp.MustCompile(`^[0-9a-f]{12}$`)
	sepCellRe = regexp.MustCompile(`^:?-{3,}:?$`)
)

type Row struct {
	ID         string
	Rule       string
	EnforcedBy string
	Check      string
	RedProof   string
	Hash       string
}

func (r *Row) Armed() bool { return r.Check != "NONE" }

type Ledger struct {
	Floor            int
	Rows             []Row
	RedProofs        int
	HasRedProofs     bool
	RepairedFixtures []string
}

func Path(repo string) string { return filepath.Join(repo, Filename) }

func (l *Ledger) Find(id string) *Row {
	for i := range l.Rows {
		if l.Rows[i].ID == id {
			return &l.Rows[i]
		}
	}
	return nil
}

func (l *Ledger) IsRepairedFixture(id string) bool {
	for _, repaired := range l.RepairedFixtures {
		if repaired == id {
			return true
		}
	}
	return false
}

func (l *Ledger) EffectiveCount() int { return len(l.Rows) + len(l.RepairedFixtures) }

func (l *Ledger) RaiseFloor(n int) {
	if n > l.Floor {
		l.Floor = n
	}
}

func (l *Ledger) MeasuredRedProofs() int {
	n := 0
	for _, r := range l.Rows {
		if r.Armed() && r.RedProof != "-" {
			n++
		}
	}
	return n
}

func (l *Ledger) MaintainRedProofs() {
	m := l.MeasuredRedProofs()
	if !l.HasRedProofs {
		l.RedProofs = m
		l.HasRedProofs = true
		return
	}
	if m > l.RedProofs {
		l.RedProofs = m
	}
}

func Load(repo string) (*Ledger, error) {
	p := Path(repo)
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v", p, err)
	}
	resolved, err := repository.ConfinedRegularFile(root, Filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v (run \"rulefloor init\"?)", p, err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %v (run \"rulefloor init\"?)", p, err)
	}
	l, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %v", p, err)
	}
	return l, nil
}

func Save(repo string, l *Ledger) error {
	p := Path(repo)
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return fmt.Errorf("cannot write %s: %v", p, err)
	}
	resolved, err := repository.ConfinedWritePath(root, Filename)
	if err != nil {
		return fmt.Errorf("cannot write %s: %v", p, err)
	}
	if err := os.WriteFile(resolved, []byte(l.Serialize()), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %v", p, err)
	}
	return nil
}

func (l *Ledger) Serialize() string {
	var b strings.Builder
	fmt.Fprintf(&b, "FLOOR: %d\n", l.Floor)
	if l.HasRedProofs {
		fmt.Fprintf(&b, "RED-PROOFS: %d\n", l.RedProofs)
	}
	if len(l.RepairedFixtures) > 0 {
		fmt.Fprintf(&b, "REPAIRED-FIXTURES: %s\n", strings.Join(l.RepairedFixtures, ","))
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

func Parse(data string) (*Ledger, error) {
	l := &Ledger{}
	stage := 0
	seen := map[string]bool{}
	repairedSeen := map[string]bool{}
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
			if values, ok := strings.CutPrefix(line, "REPAIRED-FIXTURES: "); ok {
				if len(l.RepairedFixtures) > 0 {
					return nil, fmt.Errorf("line %d: duplicate REPAIRED-FIXTURES line", ln)
				}
				for _, value := range strings.Split(values, ",") {
					id := strings.TrimSpace(value)
					if !ValidID(id) {
						return nil, fmt.Errorf("line %d: invalid repaired fixture ID %q", ln, id)
					}
					if repairedSeen[id] {
						return nil, fmt.Errorf("line %d: duplicate repaired fixture ID %q", ln, id)
					}
					repairedSeen[id] = true
					l.RepairedFixtures = append(l.RepairedFixtures, id)
				}
				continue
			}
			cells, err := splitRowLine(line)
			if err != nil || len(cells) != len(headerCells) {
				return nil, fmt.Errorf("line %d: malformed header", ln)
			}
			for j := range cells {
				if cells[j] != headerCells[j] {
					return nil, fmt.Errorf("line %d: malformed header (column %d is %q, want %q)", ln, j+1, cells[j], headerCells[j])
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
				return nil, fmt.Errorf("line %d: malformed row: %d fields, want %d", ln, len(cells), len(headerCells))
			}
			for j, c := range cells {
				if c == "" {
					return nil, fmt.Errorf("line %d: missing field %q", ln, headerCells[j])
				}
			}
			r := Row{cells[0], cells[1], cells[2], cells[3], cells[4], cells[5]}
			if !ValidID(r.ID) {
				return nil, fmt.Errorf("line %d: invalid rule ID %q", ln, r.ID)
			}
			if seen[r.ID] {
				return nil, fmt.Errorf("line %d: duplicate rule ID %q", ln, r.ID)
			}
			if repairedSeen[r.ID] {
				return nil, fmt.Errorf("line %d: rule ID %q is also recorded as a repaired fixture", ln, r.ID)
			}
			seen[r.ID] = true
			if _, err := ParseProof(r.RedProof); err != nil {
				return nil, fmt.Errorf("line %d: invalid red-proof: %v", ln, err)
			}
			if r.Armed() {
				if _, _, err := SplitCheck(r.Check); err != nil {
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

func SplitCheck(spec string) (file, profile string, err error) {
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

func ValidID(id string) bool { return idRe.MatchString(id) }

func ValidCell(s, what string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s must not be empty", what)
	}
	if strings.ContainsAny(s, "|\n\r") {
		return fmt.Errorf("%s must be a single line without '|'", what)
	}
	return nil
}
