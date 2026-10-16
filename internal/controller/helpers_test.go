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

package controller

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/fyannk/nodeecho/internal/config"
)

// Unit tests of the reconciler helpers. They need no API server.

func managed(manager, op, fields string) metav1.ManagedFieldsEntry {
	e := metav1.ManagedFieldsEntry{Manager: manager, Operation: metav1.ManagedFieldsOperationType(op), FieldsType: "FieldsV1"}
	if fields != "" {
		e.FieldsV1 = metav1.NewFieldsV1(fields)
	}
	return e
}

func TestOwnedKeys(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{ManagedFields: []metav1.ManagedFieldsEntry{
		managed("nodeecho/db", "Apply", `{"f:metadata":{"f:labels":{"f:a.io/x":{},"f:b":{}},"f:annotations":{"f:c.io/y":{}}}}`),
		// Same keys under another manager, an Update by the same manager
		// name, and a broken entry: none of them count.
		managed("nodeecho/other", "Apply", `{"f:metadata":{"f:labels":{"f:other":{}}}}`),
		managed("nodeecho/db", "Update", `{"f:metadata":{"f:labels":{"f:updated":{}}}}`),
		managed("nodeecho/db", "Apply", `not json`),
		managed("nodeecho/db", "Apply", ""),
		// The "." entry of a map is not a key.
		managed("nodeecho/db", "Apply", `{"f:metadata":{"f:labels":{".":{},"f:c":{}}}}`),
	}}}
	labels, annotations := ownedKeys(pod, "nodeecho/db")
	if want := map[string]bool{"a.io/x": true, "b": true, "c": true}; !maps.Equal(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}
	if want := map[string]bool{"c.io/y": true}; !maps.Equal(annotations, want) {
		t.Errorf("annotations = %v, want %v", annotations, want)
	}

	labels, annotations = ownedKeys(&corev1.Pod{}, "nodeecho/db")
	if len(labels) != 0 || len(annotations) != 0 {
		t.Errorf("no managedFields: got %v %v", labels, annotations)
	}
}

func TestConflictingKeys(t *testing.T) {
	conflict := apierrors.NewApplyConflict([]metav1.StatusCause{
		{Type: metav1.CauseTypeFieldManagerConflict, Field: ".metadata.labels.a.io/x"},
		{Type: metav1.CauseTypeFieldManagerConflict, Field: ".metadata.annotations.b.io/y"},
		{Type: metav1.CauseTypeFieldManagerConflict, Field: ".spec.nodeName"},
	}, "conflict")
	got := conflictingKeys(fmt.Errorf("wrapped: %w", conflict))
	want := []metadataKey{{key: "a.io/x", labels: true}, {key: "b.io/y"}}
	if !slices.Equal(got, want) {
		t.Errorf("keys = %+v, want %+v", got, want)
	}

	for _, err := range []error{
		errors.New("plain"),
		apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "p", errors.New("no causes")),
	} {
		if got := conflictingKeys(err); len(got) != 0 {
			t.Errorf("%v: keys = %+v, want none", err, got)
		}
	}
}

func TestUpToDate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    map[string]string
		current map[string]string
		owned   map[string]bool
		ok      bool
	}{
		{"nothing wanted, nothing owned", nil, map[string]string{"x": "1"}, nil, true},
		{"wanted and owned", map[string]string{"k": "v"}, map[string]string{"k": "v"}, map[string]bool{"k": true}, true},
		{"value differs", map[string]string{"k": "v"}, map[string]string{"k": "w"}, map[string]bool{"k": true}, false},
		{"key missing", map[string]string{"k": "v"}, nil, map[string]bool{"k": true}, false},
		{"present but not owned", map[string]string{"k": "v"}, map[string]string{"k": "v"}, nil, false},
		{"stale owned key", nil, map[string]string{"k": "v"}, map[string]bool{"k": true}, false},
		{"owned other key", map[string]string{"k": "v"}, map[string]string{"k": "v", "j": "v"}, map[string]bool{"j": true}, false},
		{"empty value", map[string]string{"k": ""}, map[string]string{"k": ""}, map[string]bool{"k": true}, true},
	} {
		if got := upToDate(tc.want, tc.current, tc.owned); got != tc.ok {
			t.Errorf("%s: upToDate = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

func TestPodPredicate(t *testing.T) {
	base := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}, Annotations: map[string]string{"b": "2"}}}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Pod)
		want   bool
	}{
		{"status only", func(p *corev1.Pod) { p.Status.Phase = corev1.PodRunning }, false},
		{"resource version only", func(p *corev1.Pod) { p.ResourceVersion = "9" }, false},
		{"label", func(p *corev1.Pod) { p.Labels["a"] = "x" }, true},
		{"label removed", func(p *corev1.Pod) { p.Labels = nil }, true},
		{"annotation", func(p *corev1.Pod) { p.Annotations["c"] = "" }, true},
		{"bound", func(p *corev1.Pod) { p.Spec.NodeName = "n" }, true},
	} {
		n := base.DeepCopy()
		tc.mutate(n)
		if got := podMetadataChanged.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: n}); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if !podMetadataChanged.Create(event.CreateEvent{Object: base}) {
		t.Error("creations must pass")
	}
	if podMetadataChanged.Delete(event.DeleteEvent{Object: base}) {
		t.Error("deletions have nothing to clean")
	}
}

func TestNodePredicate(t *testing.T) {
	base := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
		Spec:       corev1.NodeSpec{Taints: []corev1.Taint{{Key: "t", Effect: corev1.TaintEffectNoSchedule}}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Node)
		want   bool
	}{
		{"heartbeat", func(n *corev1.Node) {
			n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
		}, false},
		{"pod CIDR", func(n *corev1.Node) { n.Spec.PodCIDR = "10.0.0.0/24" }, false},
		{"cordon", func(n *corev1.Node) { n.Spec.Unschedulable = true }, true},
		{"taint value", func(n *corev1.Node) { n.Spec.Taints[0].Value = "v" }, true},
		{"taint removed", func(n *corev1.Node) { n.Spec.Taints = nil }, true},
		{"label", func(n *corev1.Node) { n.Labels["a"] = "2" }, true},
		{"annotation", func(n *corev1.Node) { n.Annotations = map[string]string{"x": ""} }, true},
	} {
		n := base.DeepCopy()
		tc.mutate(n)
		if got := nodeEchoedFieldsChanged(func() bool { return true }).Update(event.UpdateEvent{ObjectOld: base, ObjectNew: n}); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	annotated := base.DeepCopy()
	annotated.Annotations = map[string]string{"csi.volume.kubernetes.io/nodeid": "{}"}
	if nodeEchoedFieldsChanged(func() bool { return false }).Update(event.UpdateEvent{ObjectOld: base, ObjectNew: annotated}) {
		t.Error("annotation changes must be ignored while no rule copies annotations")
	}
}

func rulesOf(t *testing.T, doc string) ([]config.Rule, []config.RuleError) {
	t.Helper()
	valid, refused, err := config.Parse([]byte("version: 1\nrules:\n" + doc))
	if err != nil {
		t.Fatal(err)
	}
	return valid, refused
}

func names(rules []Rule) []string {
	var out []string
	for _, r := range rules {
		out = append(out, r.Name)
	}
	return out
}

func TestStore(t *testing.T) {
	s := &Store{}
	if s.Loaded() || len(s.Rules()) != 0 {
		t.Fatal("a new store is not loaded")
	}

	// An empty configuration is a valid first load.
	changed, _, err := s.Set(nil, nil)
	if err != nil || !changed || !s.Loaded() {
		t.Fatalf("first load: changed=%v err=%v loaded=%v", changed, err, s.Loaded())
	}

	a := "- name: a\n  podSelector: {matchLabels: {app: a}}\n  unschedulable: {}\n"
	b := "- name: b\n  podSelector: {matchLabels: {app: b}}\n  unschedulable: {key: x.io/b}\n"
	valid, refused := rulesOf(t, a+b)
	if changed, kept, _ := s.Set(valid, refused); !changed || len(kept) != 0 || !slices.Equal(names(s.Rules()), []string{"a", "b"}) {
		t.Fatalf("set a, b: changed=%v kept=%v rules=%v", changed, kept, names(s.Rules()))
	}
	if changed, _, _ := s.Set(valid, refused); changed {
		t.Error("the same rules again must not report a change")
	}

	// a breaks: its last valid version keeps running.
	broken := "- name: a\n  podSelector: {}\n  unschedulable: {}\n"
	valid, refused = rulesOf(t, broken+b)
	changed, kept, _ := s.Set(valid, refused)
	if changed || !slices.Equal(kept, []string{"a"}) || !slices.Equal(names(s.Rules()), []string{"b", "a"}) {
		t.Errorf("refused a: changed=%v kept=%v rules=%v", changed, kept, names(s.Rules()))
	}

	// A refused rule that never ran is not kept, and a dropped rule goes.
	valid, refused = rulesOf(t, "- name: c\n  podSelector: {}\n  unschedulable: {}\n")
	if _, kept, _ := s.Set(valid, refused); len(kept) != 0 || len(s.Rules()) != 0 {
		t.Errorf("unknown refused rule: kept=%v rules=%v", kept, names(s.Rules()))
	}

	// Reordering is not a change.
	valid, refused = rulesOf(t, a+b)
	_, _, _ = s.Set(valid, refused)
	valid, refused = rulesOf(t, b+a)
	if changed, _, _ := s.Set(valid, refused); changed {
		t.Error("reordered rules must not report a change")
	}
	if got := s.Rules()[0].FieldManager(); got != "nodeecho/b" {
		t.Errorf("field manager = %q", got)
	}
}

func TestStoreDoesNotKeepARuleWhoseKeyIsTaken(t *testing.T) {
	s := &Store{}
	valid, refused := rulesOf(t, "- name: a\n  podSelector: {matchLabels: {app: a}}\n  unschedulable: {}\n")
	_, _, _ = s.Set(valid, refused)

	// a breaks while a new rule b takes its unschedulable key.
	valid, refused = rulesOf(t, "- name: a\n  podSelector: {}\n  unschedulable: {}\n"+
		"- name: b\n  podSelector: {matchLabels: {app: b}}\n  unschedulable: {}\n")
	changed, kept, _ := s.Set(valid, refused)
	if !changed || len(kept) != 0 || !slices.Equal(names(s.Rules()), []string{"b"}) {
		t.Errorf("changed=%v kept=%v rules=%v, want b alone", changed, kept, names(s.Rules()))
	}
}

func TestStripKeepsWhatNodeEchoReads(t *testing.T) {
	now := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "n", UID: "u", ResourceVersion: "7", DeletionTimestamp: &now,
			Labels: map[string]string{"a": "1"}, Annotations: map[string]string{"b": "2"},
			OwnerReferences: []metav1.OwnerReference{{Name: "rs"}},
			ManagedFields: []metav1.ManagedFieldsEntry{
				managed("nodeecho/db", "Apply", `{"f:metadata":{"f:labels":{"f:a":{}}}}`),
				managed("kube-controller-manager", "Update", `{"f:spec":{}}`),
			},
		},
		Spec:   corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "c", Image: "i"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	got, _ := stripPod(pod)
	p := got.(*corev1.Pod)
	if p.Name != "p" || p.Namespace != "n" || p.UID != "u" || p.ResourceVersion != "7" || p.DeletionTimestamp == nil ||
		!maps.Equal(p.Labels, pod.Labels) || !maps.Equal(p.Annotations, pod.Annotations) || p.Spec.NodeName != "node" {
		t.Errorf("stripped pod lost a field NodeEcho reads: %+v", p)
	}
	if len(p.Spec.Containers) != 0 || p.Status.Phase != "" || len(p.OwnerReferences) != 0 {
		t.Errorf("stripped pod kept spec, status or owners: %+v", p)
	}
	if len(p.ManagedFields) != 1 || p.ManagedFields[0].Manager != "nodeecho/db" {
		t.Errorf("managed fields = %+v, want NodeEcho's only", p.ManagedFields)
	}
	if labels, _ := ownedKeys(p, "nodeecho/db"); !labels["a"] {
		t.Error("owned keys lost after stripping")
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: map[string]string{"z": "a"}, Annotations: map[string]string{"x": "y"}},
		Spec:       corev1.NodeSpec{Unschedulable: true, Taints: []corev1.Taint{{Key: "t"}}, PodCIDR: "10.0.0.0/24"},
		Status:     corev1.NodeStatus{Images: []corev1.ContainerImage{{Names: []string{"big"}}}},
	}
	got, _ = stripNode(node)
	n := got.(*corev1.Node)
	if !n.Spec.Unschedulable || len(n.Spec.Taints) != 1 || n.Labels["z"] != "a" || n.Annotations["x"] != "y" {
		t.Errorf("stripped node lost a field NodeEcho reads: %+v", n)
	}
	if n.Spec.PodCIDR != "" || len(n.Status.Images) != 0 {
		t.Errorf("stripped node kept spec or status: %+v", n)
	}

	got, _ = stripNamespace(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "n", Labels: map[string]string{"e": "on"}},
		Spec: corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{"kubernetes"}}})
	if ns := got.(*corev1.Namespace); ns.Labels["e"] != "on" || len(ns.Spec.Finalizers) != 0 {
		t.Errorf("stripped namespace = %+v", ns)
	}

	// Tombstones and other objects pass through.
	tomb := toolscache.DeletedFinalStateUnknown{Key: "n/p"}
	for _, f := range []toolscache.TransformFunc{stripPod, stripNode, stripNamespace} {
		if out, _ := f(tomb); out != tomb {
			t.Errorf("tombstone changed: %v", out)
		}
	}
}

func TestPodGone(t *testing.T) {
	pods := schema.GroupResource{Resource: "pods"}
	for name, tc := range map[string]struct {
		err  error
		gone bool
	}{
		"not found":                  {apierrors.NewNotFound(pods, "p"), true},
		"invalid uid":                {apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "p", nil), false},
		"uid mismatch before 1.36":   {apierrors.NewConflict(pods, "p", errors.New("uid mismatch: the provided object specified uid 1, and no existing object was found")), true},
		"field manager conflict":     {apierrors.NewApplyConflict([]metav1.StatusCause{{Field: ".metadata.labels.k"}}, "conflict"), false},
		"conflict on something else": {apierrors.NewConflict(pods, "p", errors.New("the object has been modified")), false},
		"forbidden":                  {apierrors.NewForbidden(pods, "p", errors.New("no")), false},
		"wrapped not found":          {fmt.Errorf("applying: %w", apierrors.NewNotFound(pods, "p")), true},
	} {
		if got := podGone(tc.err); got != tc.gone {
			t.Errorf("%s: podGone = %v, want %v", name, got, tc.gone)
		}
	}
	invalidUID := apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "p", field.ErrorList{
		field.Invalid(field.NewPath("metadata", "uid"), "x", "field is immutable"),
	})
	if !podGone(invalidUID) {
		t.Error("an Invalid metadata.uid means the pod was replaced")
	}
}
