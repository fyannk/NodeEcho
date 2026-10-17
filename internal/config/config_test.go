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

package config

import (
	"strings"
	"testing"
)

const header = "version: 1\nrules:\n"

func TestParseDefaults(t *testing.T) {
	rules, refused, err := Parse([]byte(header + `
- name: cnpg
  podSelector: {matchLabels: {cnpg.io/podRole: instance}}
  unschedulable: {}
  taints: {copy: [karpenter.sh/disrupted]}
  labels: {copy: [topology.kubernetes.io/zone]}
  annotations: {copy: [rack.example.org/position]}
`))
	if err != nil || len(refused) != 0 {
		t.Fatalf("Parse: err=%v refused=%v", err, refused)
	}
	r := rules[0]
	if r.Unschedulable.Key != DefaultUnschedulableKey || r.Unschedulable.Target != TargetLabels {
		t.Errorf("unschedulable defaults: %+v", r.Unschedulable)
	}
	if r.Taints.Key != DefaultTaintsKey {
		t.Errorf("taints key default: %q", r.Taints.Key)
	}
	if r.Labels.Target != TargetLabels || r.Annotations.Target != TargetAnnotations {
		t.Errorf("copy target defaults: labels=%q annotations=%q", r.Labels.Target, r.Annotations.Target)
	}
}

func TestParseWholeConfigErrors(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown version": "version: 2\nrules: []\n",
		"missing version": "rules: []\n",
		"unknown field":   header + "- name: a\n  bogus: true\n",
		"not yaml":        "version: [\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Parse([]byte(doc)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseRefusesInvalidRules(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	for name, tc := range map[string]struct{ rule, want string }{
		"missing name":        {"- " + sel[2:] + "  unschedulable: {}\n", "name is required"},
		"bad name":            {"- name: Not_DNS\n" + sel + "  unschedulable: {}\n", "name"},
		"missing selector":    {"- name: a\n  unschedulable: {}\n", "podSelector"},
		"empty selector":      {"- name: a\n  podSelector: {}\n  unschedulable: {}\n", "podSelector"},
		"bad selector":        {"- name: a\n  podSelector: {matchExpressions: [{key: app, operator: Bogus}]}\n  unschedulable: {}\n", "podSelector"},
		"echoes nothing":      {"- name: a\n" + sel, "echoes nothing"},
		"bad target":          {"- name: a\n" + sel + "  unschedulable: {target: spec}\n", "unschedulable.target"},
		"bad key":             {"- name: a\n" + sel + "  taints: {copy: [x], key: 'bad key'}\n", "taints.key"},
		"empty taint list":    {"- name: a\n" + sel + "  taints: {copy: []}\n", "taints.copy"},
		"duplicate in copy":   {"- name: a\n" + sel + "  labels: {copy: [zone, zone]}\n", "twice"},
		"bad prefix":          {"- name: a\n" + sel + "  labels: {copy: [zone], prefix: 'a b/'}\n", "labels destination"},
		"label vs annotation": {"- name: a\n" + sel + "  labels: {copy: [zone]}\n  annotations: {copy: [zone], target: labels}\n", "both write"},
		"echo vs copy":        {"- name: a\n" + sel + "  unschedulable: {key: zone}\n  labels: {copy: [zone]}\n", "both write"},
	} {
		t.Run(name, func(t *testing.T) {
			rules, refused, err := Parse([]byte(header + tc.rule + "- name: ok\n" + sel + "  unschedulable: {key: other.example.org/u}\n"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(refused) != 1 || !strings.Contains(refused[0].Error(), tc.want) {
				t.Fatalf("refused = %v, want one error containing %q", refused, tc.want)
			}
			if len(rules) != 1 || rules[0].Name != "ok" {
				t.Fatalf("the valid rule must keep running, got %+v", rules)
			}
		})
	}
}

func TestParseSharedNameOrKeyRefusesEveryRuleInvolved(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	for name, doc := range map[string]string{
		"same name":         "- name: a\n" + sel + "  unschedulable: {key: x.example.org/a}\n- name: a\n" + sel + "  unschedulable: {key: x.example.org/b}\n",
		"default echo key":  "- name: a\n" + sel + "  unschedulable: {}\n- name: b\n" + sel + "  unschedulable: {}\n",
		"same key, 2 kinds": "- name: a\n" + sel + "  unschedulable: {key: x.example.org/k, target: annotations}\n- name: b\n" + sel + "  taints: {copy: [t], key: x.example.org/k}\n",
	} {
		t.Run(name, func(t *testing.T) {
			rules, refused, err := Parse([]byte(header + doc))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(rules) != 0 || len(refused) != 2 {
				t.Fatalf("rules=%d refused=%v, want both refused", len(rules), refused)
			}
		})
	}
}

func TestParseSameCopyInTwoRulesIsAllowed(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	rules, refused, err := Parse([]byte(header +
		"- name: a\n" + sel + "  labels: {copy: [topology.kubernetes.io/zone]}\n" +
		"- name: b\n" + sel + "  labels: {copy: [topology.kubernetes.io/zone]}\n"))
	if err != nil || len(refused) != 0 || len(rules) != 2 {
		t.Fatalf("rules=%d refused=%v err=%v", len(rules), refused, err)
	}
}

func TestParseYAMLCornerCases(t *testing.T) {
	for name, tc := range map[string]struct {
		doc   string
		rules int
		fails bool
	}{
		"empty document":     {"", 0, true},
		"version as string":  {"version: \"1\"\nrules: []\n", 0, true},
		"version as float":   {"version: 1.5\nrules: []\n", 0, true},
		"null rules":         {"version: 1\nrules:\n", 0, true},
		"no rules field":     {"version: 1\n", 0, true},
		"empty rules":        {"version: 1\nrules: []\n", 0, false},
		"duplicate key":      {"version: 1\nversion: 1\nrules: []\n", 0, true},
		"duplicate rule key": {header + "- name: a\n  name: b\n  podSelector: {matchLabels: {a: b}}\n  unschedulable: {}\n", 0, true},
		"rules not a list":   {"version: 1\nrules: {}\n", 0, true},
		"null rule":          {header + "- null\n", 0, false},
		"json document":      {`{"version":1,"rules":[{"name":"a","podSelector":{"matchLabels":{"a":"b"}},"unschedulable":{}}]}`, 1, false},
	} {
		t.Run(name, func(t *testing.T) {
			rules, _, err := Parse([]byte(tc.doc))
			if (err != nil) != tc.fails {
				t.Fatalf("err = %v, want failure %v", err, tc.fails)
			}
			if len(rules) != tc.rules {
				t.Fatalf("rules = %d, want %d", len(rules), tc.rules)
			}
		})
	}
}

func TestParseKeyLengths(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	long := strings.Repeat("a", 63)
	for name, tc := range map[string]struct {
		rule string
		ok   bool
	}{
		"63-char name part":          {"  labels: {copy: [x.io/" + long + "]}\n", true},
		"64-char name part":          {"  labels: {copy: [x.io/" + long + "a]}\n", false},
		"prefix pushes it over":      {"  labels: {copy: [" + long + "], prefix: n.}\n", false},
		"prefix adds a domain":       {"  labels: {copy: [zone], prefix: node.example.org/}\n", true},
		"prefix after a domain":      {"  labels: {copy: [x.io/zone], prefix: node.}\n", true},
		"prefix yields two slashes":  {"  labels: {copy: [x.io/zone], prefix: node.io/}\n", false},
		"63-char rule name":          {"", true},
		"uppercase unschedulable":    {"  unschedulable: {key: X.io/Cordon}\n", false},
		"annotation name part valid": {"  annotations: {copy: [x.io/" + long + "]}\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			ruleName := "a"
			body := tc.rule
			if body == "" {
				ruleName = long
				body = "  unschedulable: {}\n"
			}
			rules, refused, err := Parse([]byte(header + "- name: " + ruleName + "\n" + sel + body))
			if err != nil {
				t.Fatal(err)
			}
			if got := len(rules) == 1; got != tc.ok {
				t.Fatalf("accepted = %v, want %v (refused %v)", got, tc.ok, refused)
			}
		})
	}
}

// FuzzParse checks that Parse never panics, that every accepted rule passes
// validation again, and that refused rules point at real indexes.
func FuzzParse(f *testing.F) {
	f.Add([]byte(header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  unschedulable: {}\n  taints: {copy: [t]}\n"))
	f.Add([]byte(header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: [zone], prefix: p.io/}\n  annotations: {copy: [x], target: labels}\n"))
	f.Add([]byte(header + "- name: a\n  podSelector: {matchExpressions: [{key: a, operator: Exists}]}\n  unschedulable: {target: annotations}\n- name: a\n"))
	f.Add([]byte("version: 1\nrules: []\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		rules, refused, err := Parse(data)
		if err != nil {
			if len(rules) != 0 || len(refused) != 0 {
				t.Fatal("an error must come without rules")
			}
			return
		}
		names := map[string]bool{}
		for i := range rules {
			r := rules[i]
			if err := r.validate(); err != nil {
				t.Fatalf("accepted rule fails validation: %v", err)
			}
			if names[r.Name] {
				t.Fatalf("duplicate name %q accepted", r.Name)
			}
			names[r.Name] = true
		}
		for _, e := range refused {
			if e.Index < 0 || e.Err == nil {
				t.Fatalf("bad refusal %+v", e)
			}
		}
	})
}

func TestParseEchoKeyAsAnotherRulesCopyRefusesBoth(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	for name, tc := range map[string]struct {
		doc     string
		refused int
	}{
		"echo label vs copied label": {"- name: a\n" + sel + "  unschedulable: {key: x.io/k}\n" +
			"- name: b\n" + sel + "  labels: {copy: [x.io/k]}\n", 2},
		"taints vs copied annotation with a prefix": {"- name: a\n" + sel + "  taints: {copy: [t], key: n.x.io/k}\n" +
			"- name: b\n" + sel + "  annotations: {copy: [x.io/k], prefix: n.}\n", 2},
		"different maps do not collide": {"- name: a\n" + sel + "  unschedulable: {key: x.io/k}\n" +
			"- name: b\n" + sel + "  labels: {copy: [x.io/k], target: annotations}\n", 0},
		"one rule, same name as label and annotation": {"- name: a\n" + sel +
			"  unschedulable: {key: x.io/k}\n  taints: {copy: [t], key: x.io/k}\n", 0},
	} {
		t.Run(name, func(t *testing.T) {
			_, refused, err := Parse([]byte(header + tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			if len(refused) != tc.refused {
				t.Fatalf("refused = %v, want %d", refused, tc.refused)
			}
		})
	}
}

func TestParseWholeDocumentTypeErrors(t *testing.T) {
	// Unknown fields and wrong types refuse the whole document, not one rule.
	for name, doc := range map[string]string{
		"misspelt field":         header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: [z], prefx: p.}\n",
		"copy is not a list":     header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: zone}\n",
		"target is not a string": header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: [z], target: [labels]}\n",
		"removed field":          header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: [z], forceKubernetesManaged: true}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Parse([]byte(doc)); err == nil {
				t.Fatal("expected a whole-document error")
			}
		})
	}
	// A number in a selector reads as its string.
	rules, refused, err := Parse([]byte(header + "- name: a\n  podSelector: {matchLabels: {tier: 1}}\n  unschedulable: {}\n"))
	if err != nil || len(refused) != 0 || rules[0].PodSelector.MatchLabels["tier"] != "1" {
		t.Fatalf("numeric selector value: rules=%+v err=%v", rules, err)
	}
	// An empty expression list is an empty selector.
	_, refused, err = Parse([]byte(header + "- name: a\n  podSelector: {matchExpressions: []}\n  unschedulable: {}\n"))
	if err != nil || len(refused) != 1 {
		t.Fatalf("empty matchExpressions: refused=%v err=%v", refused, err)
	}
	_, refused, err = Parse([]byte(header + "- name: a\n  podSelector: {matchLabels: {app: db}}\n  labels: {copy: [\"\"]}\n"))
	if err != nil || len(refused) != 1 {
		t.Fatalf("empty copy key: refused=%v err=%v", refused, err)
	}
}

func TestParseEviction(t *testing.T) {
	const sel = "  podSelector: {matchLabels: {app: db}}\n"
	rules, refused, err := Parse([]byte(header + "- name: a\n" + sel + "  eviction: {}\n" +
		"- name: b\n" + sel + "  eviction: {key: x.io/evicted, response: deny}\n"))
	if err != nil || len(refused) != 0 {
		t.Fatalf("refused=%v err=%v", refused, err)
	}
	if e := rules[0].Eviction; e.Key != DefaultEvictionKey || e.Response != EvictionAllow || e.Message != "" {
		t.Errorf("defaults = %+v", e)
	}
	if e := rules[1].Eviction; e.Response != EvictionDeny || e.Message != "eviction refused by NodeEcho rule b" {
		t.Errorf("deny defaults = %+v", e)
	}
	if got := rules[1].AnnotationKeys(); len(got) != 1 || got[0] != "x.io/evicted" {
		t.Errorf("annotation keys = %v", got)
	}

	for name, tc := range map[string]struct{ doc, want string }{
		"bad response":       {"- name: a\n" + sel + "  eviction: {response: maybe}\n", "eviction.response"},
		"message with allow": {"- name: a\n" + sel + "  eviction: {message: no}\n", "only used with response: deny"},
		"bad key":            {"- name: a\n" + sel + "  eviction: {key: 'bad key'}\n", "eviction.key"},
		"key used twice":     {"- name: a\n" + sel + "  eviction: {key: x.io/k}\n  annotations: {copy: [x.io/k]}\n", "both write"},
	} {
		t.Run(name, func(t *testing.T) {
			_, refused, err := Parse([]byte(header + tc.doc))
			if err != nil || len(refused) != 1 || !strings.Contains(refused[0].Error(), tc.want) {
				t.Fatalf("refused=%v err=%v, want %q", refused, err, tc.want)
			}
		})
	}

	// Two rules marking the same key would race: both are refused.
	_, refused, err = Parse([]byte(header + "- name: a\n" + sel + "  eviction: {}\n- name: b\n" + sel + "  eviction: {}\n"))
	if err != nil || len(refused) != 2 {
		t.Fatalf("shared eviction key: refused=%v err=%v", refused, err)
	}
}
