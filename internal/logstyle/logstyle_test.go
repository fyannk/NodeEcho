/*
Copyright © contributors to the NodeEcho project.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package logstyle

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The conventions, after the Kubernetes structured logging guidelines:
//   - the message is a string literal: what happened, starting with a
//     capital letter, without a trailing period or format verbs;
//   - everything variable goes in key-value pairs, whose keys are string
//     literals in lowerCamelCase.
var keyPattern = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// A call is a log call when its receiver mentions a logger: setupLog.Info,
// logger.Error, log.FromContext(ctx).V(1).Info.
var loggerPattern = regexp.MustCompile(`(?i)log`)

func TestLogCalls(t *testing.T) {
	fset := token.NewFileSet()
	calls := 0
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Info" && sel.Sel.Name != "Error") {
					return true
				}
				var recv bytes.Buffer
				_ = printer.Fprint(&recv, fset, sel.X)
				if !loggerPattern.MatchString(recv.String()) {
					return true
				}
				msgAt := 0
				if sel.Sel.Name == "Error" {
					msgAt = 1
				}
				if len(call.Args) <= msgAt {
					return true
				}
				calls++
				for _, problem := range check(call.Args[msgAt], call.Args[msgAt+1:]) {
					t.Errorf("%s: %s", fset.Position(call.Pos()), problem)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls < 10 {
		t.Fatalf("found only %d log calls: the scan is broken", calls)
	}
}

func check(msg ast.Expr, kv []ast.Expr) []string {
	var problems []string
	text, ok := literal(msg)
	switch {
	case !ok:
		problems = append(problems, "the message must be a string literal; put variable parts in key-value pairs")
	case text == "":
		problems = append(problems, "empty message")
	default:
		if first := []rune(text)[0]; !unicode.IsUpper(first) {
			problems = append(problems, "the message must start with a capital letter: "+strconv.Quote(text))
		}
		if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") {
			problems = append(problems, "the message must not end with punctuation: "+strconv.Quote(text))
		}
		if strings.Contains(text, "%") {
			problems = append(problems, "the message must not hold format verbs: "+strconv.Quote(text))
		}
	}
	if len(kv)%2 != 0 {
		problems = append(problems, "key-value arguments must come in pairs")
	}
	for i := 0; i < len(kv); i += 2 {
		key, ok := literal(kv[i])
		if !ok || !keyPattern.MatchString(key) {
			problems = append(problems, "keys must be lowerCamelCase string literals")
		}
	}
	return problems
}

func literal(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func TestCheckRejectsBadCalls(t *testing.T) {
	for src, want := range map[string]string{
		`"lower case"`:                 "capital letter",
		`"Ends with a period."`:        "punctuation",
		`"Has %s verbs"`:               "format verbs",
		`"Message " + name`:            "string literal",
		`"Odd pairs", "key"`:           "pairs",
		`"Bad key", "Snake_case", 1`:   "lowerCamelCase",
		`"Dynamic key", key, 1`:        "lowerCamelCase",
		`"Good message", "goodKey", 1`: "",
	} {
		expr, err := parser.ParseExpr("f(" + src + ")")
		if err != nil {
			t.Fatal(err)
		}
		args := expr.(*ast.CallExpr).Args
		problems := strings.Join(check(args[0], args[1:]), "; ")
		if (want == "") != (problems == "") || !strings.Contains(problems, want) {
			t.Errorf("%s: problems %q, want %q", src, problems, want)
		}
	}
}
