package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// vocabAllow are literals with a vocabulary's spelling that mean something
// else, by file (relative to the repository root) and value.
var vocabAllow = map[string][]string{
	// The fake CLI's script commands and its MCP status line.
	"internal/claude/fake/fake.go": {"ok", "ask", "fail"},
	// A word searched for in the CLI's login output.
	"internal/cli/claudecli/login.go": {"fail"},
	// The name of change_request's ask argument.
	"internal/engine/change_tools.go": {"ask"},
	// The name of the ask tool.
	"internal/engine/ask.go": {"ask"},
	// The name of the ask tool and change_request's ask argument in approvalDetail.
	"internal/front/notify.go": {"ask"},
	// The hub's event name and the front's env key for deploy progress.
	"internal/stream/hub.go":  {"deploy"},
	"internal/front/stream.go": {"deploy"},
	// GitHub's deployment state before the first status.
	"internal/github/deploy.go": {"pending"},
	// Per-check states of the PR detail view: pass, fail, running, skipped.
	"internal/github/detail.go": {"fail", "running"},
}

// #37: outside vocab.go and tests, no Go file spells a phase, a condition
// type or status, a decision kind or a task status as a raw string.
func TestNoRawVocabulary(t *testing.T) {
	words := map[string]bool{}
	add := func(xs ...string) {
		for _, x := range xs {
			words[x] = true
		}
	}
	add(Names(Phases())...)
	add(Names(CondStatuses())...)
	add(Names([]CondType{CondChecks, CondReview, CondConflict, CondDeploy, CondPushed, CondClean, CondApproval, CondMerge})...)
	add(Names([]DecisionKind{DecisionAsk, DecisionApproval, DecisionEffect})...)
	// Of the task statuses only running is Fyr's word; the CLI reports the
	// others, and "failed" or "completed" mean other things elsewhere.
	add(string(TaskRunning))
	prefixes := []string{condActionPrefix, condDeployPrefix}

	root := filepath.Join("..", "..")
	var bad []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "testdata" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if rel == "internal/core/vocab.go" {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, lit := range rawLiterals(f) {
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				hit := words[v]
				for _, p := range prefixes {
					hit = hit || strings.HasPrefix(v, p)
				}
				if hit && !slices.Contains(vocabAllow[rel], v) {
					bad = append(bad, fset.Position(lit.Pos()).String()+": "+lit.Value)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(bad) > 0 {
		t.Errorf("raw vocabulary literals; use the constants in internal/core/vocab.go:\n%s", strings.Join(bad, "\n"))
	}
}

// rawLiterals are the file's string literals, except map keys and indexes
// (wire field names) and the values of typed constants (a vocabulary of
// their own, like attention's State).
func rawLiterals(f *ast.File) []*ast.BasicLit {
	skip := map[ast.Expr]bool{}
	var out []*ast.BasicLit
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.Field:
			if x.Tag != nil {
				skip[x.Tag] = true
			}
		case *ast.KeyValueExpr:
			skip[x.Key] = true
		case *ast.IndexExpr:
			skip[x.Index] = true
		case *ast.GenDecl:
			if x.Tok == token.CONST {
				for _, s := range x.Specs {
					if vs := s.(*ast.ValueSpec); vs.Type != nil {
						for _, v := range vs.Values {
							skip[v] = true
						}
					}
				}
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING && !skip[x] {
				out = append(out, x)
			}
		}
		return true
	})
	return out
}

// #37: the phase list, the tool description and the error messages come from
// the constants.
func TestPhaseVocabulary(t *testing.T) {
	if got := OrList(Phases()); got != "working, review, landed, live, abandoned or reverted" {
		t.Fatalf("Phases: %s", got)
	}
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k"}, ByAgent, time.Time{})
	err := UpdateChange(th, c, ChangeUpdate{Phase: "shipped"}, ByAgent, time.Time{})
	if err == nil || !strings.Contains(err.Error(), OrList(Phases())) {
		t.Fatalf("phase error: %v", err)
	}
	err = UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "x", Status: "done"}}}, ByAgent, time.Time{})
	if err == nil || !strings.Contains(err.Error(), OrList(CondStatuses())) {
		t.Fatalf("status error: %v", err)
	}
}
