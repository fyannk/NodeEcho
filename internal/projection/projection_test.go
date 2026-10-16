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

package projection

import (
	"maps"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fyannk/nodeecho/internal/config"
)

const (
	uKey = config.DefaultUnschedulableKey
	tKey = config.DefaultTaintsKey
	zone = "topology.kubernetes.io/zone"
)

func rule() config.Rule {
	return config.Rule{
		Name:          "r",
		Unschedulable: &config.UnschedulableEcho{Key: uKey, Target: config.TargetLabels},
		Taints:        &config.TaintsEcho{Key: tKey, Copy: []string{"karpenter.sh/disrupted", "ToBeDeletedByClusterAutoscaler"}},
		Labels:        &config.LabelCopy{Copy: []string{zone, "rack.example.org/id"}, Target: config.TargetLabels},
		Annotations:   &config.AnnotationCopy{Copy: []string{"rack.example.org/position"}, Target: config.TargetAnnotations},
	}
}

func desired(t *testing.T, r config.Rule, n *corev1.Node, opts Options) Result {
	t.Helper()
	res, err := Desired(r, n, opts)
	if err != nil {
		t.Fatalf("Desired: %v", err)
	}
	return res
}

func TestHealthyNodeWritesOnlyCopies(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{zone: "a", "other": "x"}},
		Spec:       corev1.NodeSpec{Taints: []corev1.Taint{{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule}}},
	}
	res := desired(t, rule(), n, Options{})
	if want := map[string]string{zone: "a"}; !maps.Equal(res.Labels, want) {
		t.Errorf("labels = %v, want %v", res.Labels, want)
	}
	if len(res.Annotations) != 0 {
		t.Errorf("annotations = %v, want none: no unschedulable, no listed taint", res.Annotations)
	}
}

func TestCordonedAndTaintedNode(t *testing.T) {
	added := metav1.NewTime(time.Date(2026, 10, 16, 9, 12, 0, 0, time.UTC))
	n := &corev1.Node{Spec: corev1.NodeSpec{
		Unschedulable: true,
		Taints: []corev1.Taint{
			{Key: "ToBeDeletedByClusterAutoscaler", Value: "1760605920", Effect: corev1.TaintEffectNoSchedule},
			{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule},
			{Key: "karpenter.sh/disrupted", Effect: corev1.TaintEffectNoSchedule, TimeAdded: &added},
			{Key: "karpenter.sh/disrupted", Effect: corev1.TaintEffectNoExecute},
		},
	}}
	res := desired(t, rule(), n, Options{})
	if res.Labels[uKey] != "true" {
		t.Errorf("unschedulable label = %q, want \"true\"", res.Labels[uKey])
	}
	want := `[{"key":"ToBeDeletedByClusterAutoscaler","value":"1760605920","effect":"NoSchedule"},` +
		`{"key":"karpenter.sh/disrupted","effect":"NoSchedule","timeAdded":"2026-10-16T09:12:00Z"},` +
		`{"key":"karpenter.sh/disrupted","effect":"NoExecute"}]`
	if got := res.Annotations[tKey]; got != want {
		t.Errorf("taints annotation\n got %s\nwant %s", got, want)
	}
}

func TestUnschedulableAsAnnotation(t *testing.T) {
	r := rule()
	r.Unschedulable.Target = config.TargetAnnotations
	res := desired(t, r, &corev1.Node{Spec: corev1.NodeSpec{Unschedulable: true}}, Options{})
	if res.Annotations[uKey] != "true" || res.Labels[uKey] != "" {
		t.Errorf("labels=%v annotations=%v", res.Labels, res.Annotations)
	}
}

func TestCopiesKeepValuesAndUsePrefix(t *testing.T) {
	r := rule()
	r.Labels.Prefix = "node."
	r.Labels.Target = config.TargetAnnotations
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{"rack.example.org/id": ""},
		Annotations: map[string]string{"rack.example.org/position": "row 3, slot 12"},
	}}
	res := desired(t, r, n, Options{})
	want := map[string]string{"node.rack.example.org/id": "", "rack.example.org/position": "row 3, slot 12"}
	if !maps.Equal(res.Annotations, want) {
		t.Errorf("annotations = %v, want %v", res.Annotations, want)
	}
}

func TestInvalidLabelValueIsSkipped(t *testing.T) {
	r := rule()
	r.Annotations.Target = config.TargetLabels
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"rack.example.org/position": "row 3, slot 12"}}}
	res := desired(t, r, n, Options{})
	if _, ok := res.Labels["rack.example.org/position"]; ok {
		t.Fatal("an invalid label value must not be written")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Key != "rack.example.org/position" || !strings.Contains(res.Skipped[0].Reason, "label value") {
		t.Errorf("skipped = %+v", res.Skipped)
	}
}

func TestKubernetesManagedTopology(t *testing.T) {
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{zone: "a", "rack.example.org/id": "r1"}}}

	res := desired(t, rule(), n, Options{KubernetesCopiesTopology: true})
	if want := map[string]string{"rack.example.org/id": "r1"}; !maps.Equal(res.Labels, want) {
		t.Errorf("on 1.35+, zone is left to Kubernetes: labels = %v, want %v", res.Labels, want)
	}

	r := rule()
	r.Labels.ForceKubernetesManaged = true
	res = desired(t, r, n, Options{KubernetesCopiesTopology: true})
	if res.Labels[zone] != "a" {
		t.Errorf("forceKubernetesManaged must copy zone, labels = %v", res.Labels)
	}
}

func TestEmptyRuleBlocksWriteNothing(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{zone: "a"}},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	}
	res := desired(t, config.Rule{Name: "r"}, n, Options{})
	if len(res.Labels)+len(res.Annotations)+len(res.Skipped) != 0 {
		t.Errorf("a rule without blocks wrote %+v", res)
	}
}
