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
