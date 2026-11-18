/*
Copyright 2026 Deutsche Telekom AG.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package docs holds the test that keeps logging-reference.md in step with the code.
package docs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const docPath = "logging-reference.md"

// klogPrefixes maps the name prefixes of the klog functions that write a line
// (Infof, InfoDepth, ErrorS, Exitf, ...) to the level column of the catalog.
var klogPrefixes = map[string]string{
	"Info": "info", "Warning": "warning", "Error": "error", "Fatal": "fatal", "Exit": "fatal",
}

// klogLevel returns the catalog level of the klog function name and the index
// of its message argument; ok is false for functions that write no line, such
// as Flush, V or Enabled.
func klogLevel(name string) (level string, msgIdx int, ok bool) {
	for prefix, l := range klogPrefixes {
		if strings.HasPrefix(name, prefix) {
			level, ok = l, true
		}
	}
	if strings.Contains(name, "Depth") {
		msgIdx++ // depth comes first
	}
	if strings.HasPrefix(name, "ErrorS") {
		msgIdx++ // err comes before the message
	}
	return level, msgIdx, ok
}

// wrappers are functions that log their format argument through klog: name to
// level and index of the format argument. Their own bodies are not scanned.
var wrappers = map[string]struct {
	level  string
	msgIdx int
}{
	"debugLog": {"info", 1}, // pkg/controller/cleanup_linux.go
}

var (
	validPersonalData = []string{"none", "tenant-pii", "customer-data", "staff-id", "content"}
	validDataSubject  = []string{"none", "operator", "tenant-user", "end-customer"}
	vLevelRe          = regexp.MustCompile(`^klog\.V\((\d+)\)$`)
)

// TestLoggingReference fails when a klog call in cmd/ or pkg/ is missing from
// the message catalog of logging-reference.md, the catalog lists a call
// the code no longer makes, a call has no literal message, or a row has no
// valid personal-data class and data subject.
func TestLoggingReference(t *testing.T) {
	code := map[string]bool{}
	for _, dir := range []string{"../cmd", "../pkg"} {
		for _, c := range scan(t, dir) {
			code[c] = true
		}
	}
	doc := catalog(t)
	for _, c := range sortedKeys(code) {
		if _, ok := doc[c]; !ok {
			t.Errorf("%s: catalog lacks the log call %s; add a row", docPath, c)
		}
	}
	for _, c := range sortedKeys(doc) {
		if !code[c] {
			t.Errorf("%s: catalog lists %s, which the code no longer logs; remove the row", docPath, c)
		}
	}
}

// scan returns "| <level> | `<message>` |" for every klog call in the non-test Go files under dir.
func scan(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok {
				_, isWrapper := wrappers[fn.Name.Name]
				return !isWrapper
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			key, err := logCall(call)
			if err != nil {
				t.Errorf("%s: %v", fset.Position(call.Pos()), err)
			} else if key != "" {
				out = append(out, key)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// logCall returns the catalog key of call if it is a klog call.
func logCall(call *ast.CallExpr) (string, error) {
	if id, ok := call.Fun.(*ast.Ident); ok {
		if w, ok := wrappers[id.Name]; ok {
			return key(id.Name, w.level, call.Args, w.msgIdx)
		}
		return "", nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	level, msgIdx, ok := klogLevel(sel.Sel.Name)
	if !ok {
		return "", nil
	}
	switch recv := types.ExprString(sel.X); {
	case recv == "klog":
	case vLevelRe.MatchString(recv):
		if level == "info" {
			level = "V(" + vLevelRe.FindStringSubmatch(recv)[1] + ")"
		}
	case strings.HasPrefix(recv, "klog.V("):
		return "", fmt.Errorf("klog call %s.%s needs a literal V level", recv, sel.Sel.Name)
	default:
		return "", nil
	}
	return key("klog."+sel.Sel.Name, level, call.Args, msgIdx)
}

// key returns the catalog key of a log call of fn at level with the message in args[msgIdx].
func key(fn, level string, args []ast.Expr, msgIdx int) (string, error) {
	if len(args) <= msgIdx {
		return "", fmt.Errorf("%s has no message", fn)
	}
	lit, ok := args[msgIdx].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("%s has a non-literal message %s; use a literal format string", fn, types.ExprString(args[msgIdx]))
	}
	msg, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", err
	}
	return "| " + level + " | " + code(msg) + " |", nil
}

// code renders msg the way the catalog writes it: in backticks, with | escaped and a trailing newline dropped.
func code(msg string) string {
	return "`" + strings.ReplaceAll(strings.TrimSuffix(msg, "\n"), "|", `\|`) + "`"
}

// catalog returns the rows of the message catalog, keyed by their level and
// message columns, and checks the personal-data columns of each row.
func catalog(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "<!-- BEGIN MESSAGE CATALOG -->", "<!-- END MESSAGE CATALOG -->"
	doc := string(raw)
	i, j := strings.Index(doc, begin), strings.Index(doc, end)
	if i < 0 || j < i {
		t.Fatalf("%s needs the markers %q and %q", docPath, begin, end)
	}
	rows := map[string]bool{}
	for _, line := range strings.Split(doc[i+len(begin):j], "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Level ") {
			continue
		}
		// | level | `message` | arguments | personal data | data subject |
		cols := splitRow(line)
		if len(cols) != 5 {
			t.Errorf("%s: row %q has %d columns, want 5", docPath, line, len(cols))
			continue
		}
		key := "| " + cols[0] + " | " + cols[1] + " |"
		if rows[key] {
			t.Errorf("%s: duplicate row %s", docPath, key)
		}
		rows[key] = true
		if !slices.Contains(validPersonalData, cols[3]) {
			t.Errorf("%s: row %s has personal data %q, want one of %v", docPath, key, cols[3], validPersonalData)
		}
		if !slices.Contains(validDataSubject, cols[4]) {
			t.Errorf("%s: row %s has data subject %q, want one of %v", docPath, key, cols[4], validDataSubject)
		}
		if (cols[3] == "none") != (cols[4] == "none") {
			t.Errorf("%s: row %s: personal data %q and data subject %q must both be none or both be set", docPath, key, cols[3], cols[4])
		}
	}
	return rows
}

// splitRow splits a Markdown table row at the | that are not escaped.
func splitRow(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |")
	var cols []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteString(`\|`)
			i++
		case strings.HasPrefix(line[i:], " | "):
			cols = append(cols, cur.String())
			cur.Reset()
			i += 2
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cols, cur.String())
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestLogCall(t *testing.T) {
	for src, want := range map[string]string{
		`klog.Errorf("failed: %v", err)`:        "| error | `failed: %v` |",
		`klog.V(8).Infof("x %s\n", a)`:          "| V(8) | `x %s` |",
		`klog.Warning("a|b")`:                   "| warning | `a\\|b` |",
		`debugLog(p, "cleanup: %s", n)`:         "| info | `cleanup: %s` |",
		`logger.Info("not klog")`:               "",
		`klog.Infof(format, args...)`:           "error: non-literal message",
		`klog.V(n).Info("dynamic")`:             "error: needs a literal V level",
		`klog.V(lvl).Enabled()`:                 "",
		`klog.Flush()`:                          "",
		`klog.InfoDepth(1, "depth")`:            "| info | `depth` |",
		`klog.ErrorSDepth(1, err, "s", "k", v)`: "| error | `s` |",
		`klog.ErrorS(err, "structured")`:        "| error | `structured` |",
		`klog.Exitf("exit %v", err)`:            "| fatal | `exit %v` |",
		`klog.V(2).InfoS("vs", "k", v)`:         "| V(2) | `vs` |",
	} {
		expr, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatal(err)
		}
		got, err := logCall(expr.(*ast.CallExpr))
		if err != nil {
			got = "error: " + err.Error()
		}
		if (strings.HasPrefix(want, "error: ") && !strings.Contains(got, strings.TrimPrefix(want, "error: "))) || (!strings.HasPrefix(want, "error: ") && got != want) {
			t.Errorf("%s: got %q, want %q", src, got, want)
		}
	}
}
