package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ozgurcd/rulefloor/internal/extract"
	"github.com/ozgurcd/rulefloor/internal/ledger"
	"github.com/ozgurcd/rulefloor/internal/model"
	"github.com/ozgurcd/rulefloor/internal/repository"
)

type Issue struct {
	Code    string
	Message string
}

type StaticEvaluation struct {
	Binding model.Binding
	Kind    extract.Kind
	Ref     *extract.Ref
	Issues  []Issue
}

func EvaluateSource(registry *extract.Registry, row *model.Row, source string) (StaticEvaluation, error) {
	binding, err := model.InterpretBinding(row)
	if err != nil {
		return StaticEvaluation{}, err
	}
	extractor, err := registry.ForFile(binding.File)
	if err != nil {
		return StaticEvaluation{}, err
	}
	evaluation := StaticEvaluation{Binding: binding, Kind: extractor.Kind()}
	if row.EnforcedBy != string(evaluation.Kind) {
		evaluation.Issues = append(evaluation.Issues, Issue{
			Code:    "enforced_by_mismatch",
			Message: fmt.Sprintf("enforced-by is %q but check file implies %q", row.EnforcedBy, evaluation.Kind),
		})
	}
	ref, err := extractor.Extract(source, row.ID)
	if err != nil {
		return evaluation, err
	}
	evaluation.Ref = ref
	if ref.Modifier == "skip" {
		evaluation.Issues = append(evaluation.Issues, Issue{Code: "test_skipped", Message: ".skip is set on the tagged test"})
	}
	if ref.Modifier == "only" {
		evaluation.Issues = append(evaluation.Issues, Issue{Code: "test_restricted", Message: ".only is set on the tagged test"})
	}
	if ref.GoSkips && evaluation.Kind == extract.KindGoTest && binding.Execution == ledger.ExecutionExecute {
		evaluation.Issues = append(evaluation.Issues, Issue{Code: "test_skipped", Message: "tagged Go test calls t.Skip"})
	}
	if fingerprint := ref.Fingerprint(); fingerprint != row.Hash {
		evaluation.Issues = append(evaluation.Issues, Issue{
			Code:    "hash_mismatch",
			Message: fmt.Sprintf("hash mismatch: ledger %s, actual %s (body changed; review, then rehash)", row.Hash, fingerprint),
		})
	}
	return evaluation, nil
}

func SupportsExecution(kind extract.Kind) bool { return kind == extract.KindGoTest }

func ValidateExecutionProfile(binding model.Binding, requested string) error {
	if requested != "" && requested != binding.Profile {
		return fmt.Errorf("requested profile %q does not match declared profile %q", requested, binding.Profile)
	}
	if binding.Execution == model.ExecutionStatic && requested == "" {
		return fmt.Errorf("execute mode requires --profile %s", binding.Profile)
	}
	return nil
}

var skipDirectories = map[string]bool{".git": true, "node_modules": true, "vendor": true, "testdata": true}

type LocatedTag struct {
	Path string
	Tag  extract.Tag
}

func DiscoverRepository(registry *extract.Registry, repo string) ([]LocatedTag, error) {
	root, err := repository.CanonicalRoot(repo)
	if err != nil {
		return nil, fmt.Errorf("CANNOT-EVALUATE: orphan scan: %v", err)
	}
	var discovered []LocatedTag
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skipDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		extractor, err := registry.ForFile(relative)
		if err != nil || !extractor.DiscoversFile(relative) {
			return nil
		}
		resolved, err := repository.ConfinedRegularFile(root, relative)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return err
		}
		tags, err := extractor.Discover(string(data))
		if err != nil {
			return fmt.Errorf("%s: %w", relative, err)
		}
		for _, tag := range tags {
			discovered = append(discovered, LocatedTag{Path: relative, Tag: tag})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("CANNOT-EVALUATE: orphan scan: %v", err)
	}
	return discovered, nil
}

func ScanOrphans(registry *extract.Registry, repo string, model *ledger.Ledger) ([]string, error) {
	known := map[string]bool{}
	for _, row := range model.Rows {
		known[row.ID] = true
	}
	discovered, err := DiscoverRepository(registry, repo)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, located := range discovered {
		if !ledger.ValidID(located.Tag.ID) {
			problems = append(problems, fmt.Sprintf("%s: invalid RULE tag %q", located.Path, located.Tag.ID))
			continue
		}
		if !known[located.Tag.ID] {
			problems = append(problems, fmt.Sprintf("%s: orphan tag %s (no ledger row)", located.Path, located.Tag.ID))
		}
	}
	return problems, nil
}

type CommandRunner interface {
	Run(context.Context, string, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	return command.CombinedOutput()
}

type ExecutionStatus string

const (
	ExecutionPass           ExecutionStatus = "pass"
	ExecutionFail           ExecutionStatus = "fail"
	ExecutionCannotEvaluate ExecutionStatus = "cannot_evaluate"
)

type Execution struct {
	Status    ExecutionStatus
	Performed bool
	Reason    string
	Message   string
}

func RunGoTest(ctx context.Context, runner CommandRunner, testFile, functionName, tags string) Execution {
	args := []string{"test", "-count=1", "-v", "-run", "^" + functionName + "$"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, ".")
	output, err := runner.Run(ctx, filepath.Dir(testFile), "go", args...)
	return interpretGoTestResult(ctx, output, err, functionName, true)
}

func ValidateBuildTags(tags string) error {
	if tags == "" {
		return nil
	}
	if len(tags) > 1024 {
		return fmt.Errorf("--tags exceeds 1024 bytes")
	}
	values := strings.Split(tags, ",")
	if len(values) > 64 {
		return fmt.Errorf("--tags contains more than 64 build tags")
	}
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("--tags contains an empty build tag")
		}
		for _, character := range value {
			if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '.' {
				return fmt.Errorf("--tags contains invalid build tag %q", value)
			}
		}
	}
	return nil
}

func BoundedMessage(message string) string {
	const limit = 4096
	message = strings.TrimSpace(message)
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "..."
}

func BoundedDiagnostic(output, fallback string) string {
	if strings.TrimSpace(output) == "" {
		output = fallback
	}
	return BoundedMessage(output)
}

func ParsePlaywrightReport(path string) (map[string][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("CANNOT-EVALUATE: cannot read report %s: %v", path, err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("CANNOT-EVALUATE: report %s is not valid JSON: %v", path, err)
	}
	out := map[string][]string{}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case []any:
			for _, element := range value {
				walk(element)
			}
		case map[string]any:
			title, hasTitle := value["title"].(string)
			tests, hasTests := value["tests"].([]any)
			if hasTitle && hasTests {
				tags, _ := extract.NewJavaScript(extract.KindPlaywright).Discover("test(" + fmt.Sprintf("%q", title) + ", () => {})")
				for _, testValue := range tests {
					test, ok := testValue.(map[string]any)
					if !ok {
						continue
					}
					status := testFinalStatus(test)
					for _, tag := range tags {
						out[tag.ID] = append(out[tag.ID], status)
					}
				}
			}
			for _, nested := range value {
				walk(nested)
			}
		}
	}
	walk(root)
	return out, nil
}

func testFinalStatus(test map[string]any) string {
	if results, ok := test["results"].([]any); ok && len(results) > 0 {
		if last, ok := results[len(results)-1].(map[string]any); ok {
			if status, ok := last["status"].(string); ok {
				return status
			}
		}
	}
	if status, ok := test["status"].(string); ok {
		return status
	}
	return "unknown"
}
