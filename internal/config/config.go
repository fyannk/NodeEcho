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

// Package config parses and validates the NodeEcho rules ConfigMap, as
// specified in web/docs/reference/contract.md ("Configuration").
package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

// Version is the only configuration format version this build accepts.
const Version = 1

// Default destination keys of the echoed Node spec fields.
const (
	DefaultUnschedulableKey = "nodeecho.fyannk.dev/unschedulable"
	DefaultTaintsKey        = "nodeecho.fyannk.dev/taints"
	DefaultEvictionKey      = "nodeecho.fyannk.dev/eviction-requested"
)

// Target names the pod metadata map a key is written to.
type Target string

// The two metadata maps of a pod.
const (
	TargetLabels      Target = "labels"
	TargetAnnotations Target = "annotations"
)

// Config is the content of the rules ConfigMap key.
type Config struct {
	Version int `json:"version"`
	// Rules is required, so that a forgotten or emptied field does not
	// remove every key: `rules: []` stops writing.
	Rules *[]Rule `json:"rules"`
}

// Rule selects pods and says what to echo onto them.
type Rule struct {
	Name              string                `json:"name"`
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
	PodSelector       *metav1.LabelSelector `json:"podSelector,omitempty"`
	NodeSelector      *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	Unschedulable     *UnschedulableEcho    `json:"unschedulable,omitempty"`
	Taints            *TaintsEcho           `json:"taints,omitempty"`
	Labels            *LabelCopy            `json:"labels,omitempty"`
	Annotations       *AnnotationCopy       `json:"annotations,omitempty"`
	Eviction          *EvictionMark         `json:"eviction,omitempty"`
}

// UnschedulableEcho echoes Node.spec.unschedulable.
type UnschedulableEcho struct {
	Key    string `json:"key,omitempty"`
	Target Target `json:"target,omitempty"`
}

// TaintsEcho echoes the Node taints whose key is listed in Copy.
type TaintsEcho struct {
	Copy []string `json:"copy"`
	Key  string   `json:"key,omitempty"`
}

// LabelCopy copies Node labels.
type LabelCopy struct {
	Copy                   []string `json:"copy"`
	Target                 Target   `json:"target,omitempty"`
	Prefix                 string   `json:"prefix,omitempty"`
	ForceKubernetesManaged bool     `json:"forceKubernetesManaged,omitempty"`
}

// AnnotationCopy copies Node annotations.
type AnnotationCopy struct {
	Copy   []string `json:"copy"`
	Target Target   `json:"target,omitempty"`
	Prefix string   `json:"prefix,omitempty"`
}

// Responses of the eviction webhook.
const (
	EvictionAllow = "allow"
	EvictionDeny  = "deny"
)

// EvictionMark records, when the eviction webhook is installed, the time
// NodeEcho received the first eviction request for a selected pod, and
// answers the request.
type EvictionMark struct {
	Key      string `json:"key,omitempty"`
	Response string `json:"response,omitempty"`
	Message  string `json:"message,omitempty"`
}

// RuleError reports a refused rule. The other rules keep running.
type RuleError struct {
	Index int
	Name  string
	Err   error
}

func (e RuleError) Error() string {
	return fmt.Sprintf("rule %d (%q): %v", e.Index, e.Name, e.Err)
}

// Parse decodes and validates a configuration. A malformed document or an
// unknown version returns an error and no rules: the caller keeps the last
// valid configuration. Otherwise it returns the valid rules, with defaults
// applied, and one RuleError per refused rule.
func Parse(data []byte) ([]Rule, []RuleError, error) {
	var cfg Config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("decoding configuration: %w", err)
	}
	if cfg.Version != Version {
		return nil, nil, fmt.Errorf("unsupported configuration version %d, want %d", cfg.Version, Version)
	}
	if cfg.Rules == nil {
		return nil, nil, errors.New("rules is required; set rules: [] to stop writing")
	}
	all := *cfg.Rules

	var refused []RuleError
	ok := make([]bool, len(all))
	for i := range all {
		r := &all[i]
		r.applyDefaults()
		if err := r.validate(); err != nil {
			refused = append(refused, RuleError{Index: i, Name: r.Name, Err: err})
			continue
		}
		ok[i] = true
	}

	// Names and echo keys shared between rules refuse every rule involved,
	// so the outcome does not depend on rule order.
	byName := map[string][]int{}
	byEchoKey := map[string][]int{}
	// An echo key is also refused as another rule's copy destination in the
	// same metadata map: the two would write different values.
	copiedTo := map[destination][]int{}
	for i, r := range all {
		if !ok[i] {
			continue
		}
		byName[r.Name] = append(byName[r.Name], i)
		for _, k := range r.EchoKeys() {
			byEchoKey[k] = append(byEchoKey[k], i)
		}
		for _, d := range r.destinations() {
			if !d.echo {
				copiedTo[d.key()] = append(copiedTo[d.key()], i)
			}
		}
	}
	for i, r := range all {
		if !ok[i] {
			continue
		}
		for _, d := range r.destinations() {
			if !d.echo {
				continue
			}
			for _, j := range copiedTo[d.key()] {
				if j != i {
					byEchoKey[d.Key] = append(byEchoKey[d.Key], j)
				}
			}
		}
	}
	shared := map[int]error{}
	for name, idx := range byName {
		if len(idx) > 1 {
			for _, i := range idx {
				shared[i] = fmt.Errorf("name %q is used by %d rules", name, len(idx))
			}
		}
	}
	for key, idx := range byEchoKey {
		// A rule may use one key name both as a label and as an annotation.
		slices.Sort(idx)
		idx = slices.Compact(idx)
		if len(idx) > 1 {
			for _, i := range idx {
				if _, done := shared[i]; !done {
					shared[i] = fmt.Errorf("key %q is used by %d rules", key, len(idx))
				}
			}
		}
	}
	for i, err := range shared {
		ok[i] = false
		refused = append(refused, RuleError{Index: i, Name: all[i].Name, Err: err})
	}
	sort.Slice(refused, func(a, b int) bool { return refused[a].Index < refused[b].Index })

	var rules []Rule
	for i, r := range all {
		if ok[i] {
			rules = append(rules, r)
		}
	}
	return rules, refused, nil
}

func (r *Rule) applyDefaults() {
	if u := r.Unschedulable; u != nil {
		if u.Key == "" {
			u.Key = DefaultUnschedulableKey
		}
		if u.Target == "" {
			u.Target = TargetLabels
		}
	}
	if t := r.Taints; t != nil && t.Key == "" {
		t.Key = DefaultTaintsKey
	}
	if l := r.Labels; l != nil && l.Target == "" {
		l.Target = TargetLabels
	}
	if a := r.Annotations; a != nil && a.Target == "" {
		a.Target = TargetAnnotations
	}
	if e := r.Eviction; e != nil {
		if e.Key == "" {
			e.Key = DefaultEvictionKey
		}
		if e.Response == "" {
			e.Response = EvictionAllow
		}
		if e.Response == EvictionDeny && e.Message == "" {
			e.Message = fmt.Sprintf("eviction refused by NodeEcho rule %s", r.Name)
		}
	}
}

// destination is a pod key a rule writes.
type destination struct {
	Target Target
	Key    string
	echo   bool // an echoed Node spec field, not a copy
}

func (d destination) key() destination { return destination{Target: d.Target, Key: d.Key} }

// destinations returns the pod keys the rule writes. Only meaningful after
// applyDefaults.
func (r *Rule) destinations() []destination {
	var out []destination
	if u := r.Unschedulable; u != nil {
		out = append(out, destination{u.Target, u.Key, true})
	}
	if t := r.Taints; t != nil {
		out = append(out, destination{TargetAnnotations, t.Key, true})
	}
	if l := r.Labels; l != nil {
		for _, k := range l.Copy {
			out = append(out, destination{l.Target, l.Prefix + k, false})
		}
	}
	if a := r.Annotations; a != nil {
		for _, k := range a.Copy {
			out = append(out, destination{a.Target, a.Prefix + k, false})
		}
	}
	if e := r.Eviction; e != nil {
		out = append(out, destination{TargetAnnotations, e.Key, true})
	}
	return out
}

// AnnotationKeys returns the pod annotation keys the rule writes.
func (r *Rule) AnnotationKeys() []string {
	var keys []string
	for _, d := range r.destinations() {
		if d.Target == TargetAnnotations {
			keys = append(keys, d.Key)
		}
	}
	return keys
}

// EchoKeys returns the keys of the echoed Node spec fields, which must be
// unique across rules.
func (r *Rule) EchoKeys() []string {
	var keys []string
	if r.Unschedulable != nil {
		keys = append(keys, r.Unschedulable.Key)
	}
	if r.Taints != nil {
		keys = append(keys, r.Taints.Key)
	}
	if r.Eviction != nil {
		keys = append(keys, r.Eviction.Key)
	}
	return keys
}

func (r *Rule) validate() error {
	var errs []error
	if r.Name == "" {
		errs = append(errs, errors.New("name is required"))
	} else if msgs := validation.IsDNS1123Label(r.Name); len(msgs) > 0 {
		errs = append(errs, fmt.Errorf("name %q: %v", r.Name, msgs))
	}

	if r.PodSelector == nil || (len(r.PodSelector.MatchLabels) == 0 && len(r.PodSelector.MatchExpressions) == 0) {
		errs = append(errs, errors.New("podSelector is required and must not be empty"))
	}
	for field, sel := range map[string]*metav1.LabelSelector{
		"namespaceSelector": r.NamespaceSelector,
		"podSelector":       r.PodSelector,
		"nodeSelector":      r.NodeSelector,
	} {
		if sel == nil {
			continue
		}
		if _, err := metav1.LabelSelectorAsSelector(sel); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", field, err))
		}
	}

	if r.Unschedulable == nil && r.Taints == nil && r.Labels == nil && r.Annotations == nil && r.Eviction == nil {
		errs = append(errs, errors.New("rule echoes nothing: set unschedulable, taints, labels, annotations or eviction"))
	}

	// Destination keys per pod metadata map, to detect collisions.
	dest := map[Target]map[string]string{TargetLabels: {}, TargetAnnotations: {}}
	add := func(t Target, key, from string) {
		if dest[t] == nil {
			return // invalid target, already reported
		}
		if prev, dup := dest[t][key]; dup {
			errs = append(errs, fmt.Errorf("%s and %s both write %s %q", prev, from, t, key))
			return
		}
		dest[t][key] = from
	}

	if u := r.Unschedulable; u != nil {
		errs = append(errs, checkTarget("unschedulable.target", u.Target)...)
		errs = append(errs, checkKey("unschedulable.key", u.Key)...)
		add(u.Target, u.Key, "unschedulable")
	}
	if t := r.Taints; t != nil {
		errs = append(errs, checkKey("taints.key", t.Key)...)
		errs = append(errs, checkCopy("taints.copy", t.Copy)...)
		add(TargetAnnotations, t.Key, "taints")
	}
	if l := r.Labels; l != nil {
		errs = append(errs, checkTarget("labels.target", l.Target)...)
		errs = append(errs, checkCopy("labels.copy", l.Copy)...)
		for _, k := range l.Copy {
			errs = append(errs, checkKey("labels destination", l.Prefix+k)...)
			add(l.Target, l.Prefix+k, "label "+k)
		}
	}
	if a := r.Annotations; a != nil {
		errs = append(errs, checkTarget("annotations.target", a.Target)...)
		errs = append(errs, checkCopy("annotations.copy", a.Copy)...)
		for _, k := range a.Copy {
			errs = append(errs, checkKey("annotations destination", a.Prefix+k)...)
			add(a.Target, a.Prefix+k, "annotation "+k)
		}
	}
	if e := r.Eviction; e != nil {
		errs = append(errs, checkKey("eviction.key", e.Key)...)
		add(TargetAnnotations, e.Key, "eviction")
		if e.Response != EvictionAllow && e.Response != EvictionDeny {
			errs = append(errs, fmt.Errorf("eviction.response: %q is not %q or %q", e.Response, EvictionAllow, EvictionDeny))
		}
		if e.Response == EvictionAllow && e.Message != "" {
			errs = append(errs, errors.New("eviction.message is only used with response: deny"))
		}
	}
	return errors.Join(errs...)
}

func checkTarget(field string, t Target) []error {
	if t != TargetLabels && t != TargetAnnotations {
		return []error{fmt.Errorf("%s: %q is not %q or %q", field, t, TargetLabels, TargetAnnotations)}
	}
	return nil
}

// checkKey validates a metadata key. Label and annotation keys share the
// same qualified-name syntax.
func checkKey(field, key string) []error {
	if msgs := validation.IsQualifiedName(key); len(msgs) > 0 {
		return []error{fmt.Errorf("%s %q: %v", field, key, msgs)}
	}
	return nil
}

func checkCopy(field string, keys []string) []error {
	if len(keys) == 0 {
		return []error{fmt.Errorf("%s must list at least one key", field)}
	}
	var errs []error
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			errs = append(errs, fmt.Errorf("%s lists %q twice", field, k))
		}
		seen[k] = true
		errs = append(errs, checkKey(field, k)...)
	}
	return errs
}
