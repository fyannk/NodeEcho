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

// Package controller holds the NodeEcho controllers: the rules ConfigMap
// loader and the pod projector.
package controller

import (
	"reflect"
	"slices"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/fyannk/nodeecho/internal/config"
)

// Rule is a validated rule with its selectors compiled.
type Rule struct {
	config.Rule
	pods, namespaces, nodes labels.Selector
}

// FieldManager is the server-side apply field manager of the rule.
func (r *Rule) FieldManager() string { return fieldManagerPrefix + r.Name }

const fieldManagerPrefix = "nodeecho/"

func compile(r config.Rule) (Rule, error) {
	c := Rule{Rule: r}
	var err error
	if c.pods, err = selector(r.PodSelector); err != nil {
		return Rule{}, err
	}
	if c.namespaces, err = selector(r.NamespaceSelector); err != nil {
		return Rule{}, err
	}
	if c.nodes, err = selector(r.NodeSelector); err != nil {
		return Rule{}, err
	}
	return c, nil
}

func selector(s *metav1.LabelSelector) (labels.Selector, error) {
	if s == nil {
		return labels.Everything(), nil
	}
	return metav1.LabelSelectorAsSelector(s)
}

// Store holds the active rules. Until a first valid configuration is loaded,
// Loaded is false and the pod projector does nothing, so that an empty rule
// set never removes keys at startup.
type Store struct {
	mu     sync.RWMutex
	loaded bool
	rules  []Rule
}

// Loaded reports whether a valid configuration has been loaded.
func (s *Store) Loaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded
}

// Rules returns the active rules. The slice must not be modified.
func (s *Store) Rules() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rules
}

// CopiesAnnotations reports whether an active rule copies Node annotations.
func (s *Store) CopiesAnnotations() bool {
	return slices.ContainsFunc(s.Rules(), func(r Rule) bool { return r.Annotations != nil })
}

// Set replaces the active rules with valid. A refused rule whose name was
// active keeps its last valid version, so that a typo in a running rule does
// not remove its keys from every pod, unless a valid rule now uses one of its
// echo keys; it returns the names kept that way. It reports whether the
// active rules changed.
func (s *Store) Set(valid []config.Rule, refused []config.RuleError) (changed bool, kept []string, err error) {
	next := make([]Rule, 0, len(valid))
	names := map[string]bool{}
	echoKeys := map[string]bool{}
	for _, r := range valid {
		for _, k := range r.EchoKeys() {
			echoKeys[k] = true
		}
		c, err := compile(r)
		if err != nil {
			return false, nil, err
		}
		next = append(next, c)
		names[r.Name] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	refusedNames := map[string]bool{}
	for _, e := range refused {
		refusedNames[e.Name] = true
	}
	for _, prev := range s.rules {
		if refusedNames[prev.Name] && !names[prev.Name] && !slices.ContainsFunc(prev.EchoKeys(), func(k string) bool { return echoKeys[k] }) {
			next = append(next, prev)
			names[prev.Name] = true
			kept = append(kept, prev.Name)
		}
	}

	changed = !s.loaded || !sameRules(s.rules, next)
	s.rules = next
	s.loaded = true
	return changed, kept, nil
}

// sameRules compares two rule sets by name, ignoring order: the order of
// the rules does not change what they write.
func sameRules(a, b []Rule) bool {
	if len(a) != len(b) {
		return false
	}
	byName := make(map[string]*config.Rule, len(a))
	for i := range a {
		byName[a[i].Name] = &a[i].Rule
	}
	for i := range b {
		prev, ok := byName[b[i].Name]
		if !ok || !reflect.DeepEqual(*prev, b[i].Rule) {
			return false
		}
	}
	return true
}
