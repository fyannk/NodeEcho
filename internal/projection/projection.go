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

// Package projection computes the metadata a rule writes on a pod from the
// pod's Node, as specified in web/docs/reference/contract.md ("Keys"). It is pure: no API
// calls, no ownership decisions.
package projection

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/fyannk/nodeecho/internal/config"
)

// Result is the desired metadata of one rule on one pod. Keys absent from
// Labels and Annotations must be absent from the pod, as far as the rule
// owns them.
type Result struct {
	Labels      map[string]string
	Annotations map[string]string
	// Skipped lists configured keys that are not written, with the reason,
	// for Events and metrics.
	Skipped []Skip
}

// Skip is a configured key that is not written.
type Skip struct {
	Key    string
	Reason string
}

// Desired returns what rule writes on a pod bound to node. Whether a key is
// written is decided later, from the pod: a key another writer already
// holds is not NodeEcho's to write.
func Desired(rule config.Rule, node *corev1.Node) (Result, error) {
	res := Result{Labels: map[string]string{}, Annotations: map[string]string{}}
	set := func(t config.Target, key, value string) {
		if t == config.TargetLabels {
			if msgs := validation.IsValidLabelValue(value); len(msgs) > 0 {
				res.Skipped = append(res.Skipped, Skip{Key: key, Reason: "invalid label value: " + strings.Join(msgs, "; ")})
				return
			}
			res.Labels[key] = value
			return
		}
		res.Annotations[key] = value
	}

	if u := rule.Unschedulable; u != nil && node.Spec.Unschedulable {
		set(u.Target, u.Key, "true")
	}

	if t := rule.Taints; t != nil {
		listed := make(map[string]bool, len(t.Copy))
		for _, k := range t.Copy {
			listed[k] = true
		}
		var taints []corev1.Taint
		for _, taint := range node.Spec.Taints {
			if listed[taint.Key] {
				taints = append(taints, taint)
			}
		}
		if len(taints) > 0 {
			data, err := json.Marshal(taints)
			if err != nil {
				return Result{}, fmt.Errorf("encoding taints: %w", err)
			}
			set(config.TargetAnnotations, t.Key, string(data))
		}
	}

	if l := rule.Labels; l != nil {
		for _, k := range l.Copy {
			if v, ok := node.Labels[k]; ok {
				set(l.Target, l.Prefix+k, v)
			}
		}
	}

	if a := rule.Annotations; a != nil {
		for _, k := range a.Copy {
			if v, ok := node.Annotations[k]; ok {
				set(a.Target, a.Prefix+k, v)
			}
		}
	}

	return res, nil
}
