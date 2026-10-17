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
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/fyannk/nodeecho/internal/projection"
)

// Edge cases of the pod projector, against envtest.

const cordonRule = `- name: cordon
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
`

func cordoned(p *corev1.Pod) bool { return hasLabel(p, uKey, "true") }

func noKeys(p *corev1.Pod) bool {
	_, u := p.Labels[uKey]
	return !u && len(managers(p)) == 0
}

func TestNodeCreatedAfterThePod(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	late := fmt.Sprintf("late-%s", f.ns)
	must(k8s.Create(context.Background(), newPod(f.ns, "early", late, map[string]string{"app": "db"}, nil)))
	consistently(t, "nothing written without a Node", func() bool { return noKeys(getPod(t, f.ns, "early")) })

	must(k8s.Create(context.Background(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: late},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	}))
	eventually(t, "echoed once the Node exists", func() bool { return cordoned(getPod(t, f.ns, "early")) })
}

func TestDeletedNodeLeavesThePodAlone(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	must(k8s.Delete(context.Background(), f.node))
	// Trigger a reconcile through a pod change.
	p := getPod(t, f.ns, "db")
	p.Annotations = map[string]string{"poke": "1"}
	must(k8s.Update(context.Background(), p))
	consistently(t, "keys kept on a pod whose Node is gone", func() bool { return cordoned(getPod(t, f.ns, "db")) })
}

func TestNamespaceSelector(t *testing.T) {
	setRules(t, `- name: cordon
  namespaceSelector: {matchLabels: {echo: "on"}}
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
`)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	consistently(t, "namespace not selected", func() bool { return noKeys(getPod(t, f.ns, "db")) })

	setNamespaceLabels := func(l map[string]string) {
		ns := &corev1.Namespace{}
		must(k8s.Get(context.Background(), types.NamespacedName{Name: f.ns}, ns))
		ns.Labels = l
		must(k8s.Update(context.Background(), ns))
	}
	setNamespaceLabels(map[string]string{"echo": "on"})
	eventually(t, "namespace selected", func() bool { return cordoned(getPod(t, f.ns, "db")) })
	setNamespaceLabels(map[string]string{"echo": "off"})
	eventually(t, "namespace left scope", func() bool { return noKeys(getPod(t, f.ns, "db")) })
}

func TestNodeSelector(t *testing.T) {
	setRules(t, `- name: cordon
  podSelector: {matchLabels: {app: db}}
  nodeSelector: {matchExpressions: [{key: pool, operator: In, values: [db]}]}
  unschedulable: {}
`)
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{"pool": "db"})
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "node selected", func() bool { return cordoned(getPod(t, f.ns, "db")) })
	f.updateNode(t, func(n *corev1.Node) { n.Labels["pool"] = "web" })
	eventually(t, "node left scope", func() bool { return noKeys(getPod(t, f.ns, "db")) })
}

func TestPodEntersScope(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "web"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	consistently(t, "not selected", func() bool { return noKeys(getPod(t, f.ns, "db")) })
	p := getPod(t, f.ns, "db")
	p.Labels["app"] = "db"
	must(k8s.Update(context.Background(), p))
	eventually(t, "selected after a label change", func() bool { return cordoned(getPod(t, f.ns, "db")) })
}

func TestRuleRename(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "written by cordon", func() bool {
		return slices.Equal(managers(getPod(t, f.ns, "db")), []string{"nodeecho/cordon"})
	})

	setRules(t, `- name: drain
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
`)
	eventually(t, "handed over to drain", func() bool {
		p := getPod(t, f.ns, "db")
		return cordoned(p) && slices.Equal(managers(p), []string{"nodeecho/drain"})
	})
}

func TestTwoRulesCopyingTheSameKey(t *testing.T) {
	both := `- name: one
  podSelector: {matchLabels: {app: db}}
  labels: {copy: [rack.example.org/id]}
- name: two
  podSelector: {matchLabels: {app: db}}
  labels: {copy: [rack.example.org/id]}
`
	setRules(t, both)
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{rack: "r1"})
	eventually(t, "copied by both", func() bool {
		p := getPod(t, f.ns, "db")
		return hasLabel(p, rack, "r1") && len(managers(p)) == 2
	})

	setRules(t, both[:len(both)/2])
	eventually(t, "two gone, one keeps the key", func() bool {
		p := getPod(t, f.ns, "db")
		return hasLabel(p, rack, "r1") && slices.Equal(managers(p), []string{"nodeecho/one"})
	})
	f.updateNode(t, func(n *corev1.Node) { n.Labels[rack] = "r2" })
	eventually(t, "follows the node", func() bool { return hasLabel(getPod(t, f.ns, "db"), rack, "r2") })
}

func TestValueSharedWithAnotherWriterIsNotRemoved(t *testing.T) {
	setRules(t, `- name: racks
  podSelector: {matchLabels: {app: db}}
  labels: {copy: [rack.example.org/id]}
`)
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{rack: "r1"})
	eventually(t, "copied", func() bool { return hasLabel(getPod(t, f.ns, "db"), rack, "r1") })

	// Another writer applies the same value: shared ownership.
	ac := corev1ac.Pod("db", f.ns).WithLabels(map[string]string{rack: "r1"})
	must(k8s.Apply(context.Background(), ac, client.FieldOwner("someone")))
	f.updateNode(t, func(n *corev1.Node) { delete(n.Labels, rack) })
	eventually(t, "nodeecho released its ownership", func() bool { return len(managers(getPod(t, f.ns, "db"))) == 0 })
	consistently(t, "the other writer's value stays", func() bool { return hasLabel(getPod(t, f.ns, "db"), rack, "r1") })
}

func TestOverwrittenKeyIsNotFoughtOver(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	// As kubectl label --overwrite: an Update takes the key over.
	p := getPod(t, f.ns, "db")
	p.Labels[uKey] = "forged"
	must(k8s.Update(context.Background(), p, client.FieldOwner("kubectl")))
	consistently(t, "the other writer keeps it", func() bool { return hasLabel(getPod(t, f.ns, "db"), uKey, "forged") })
	eventually(t, "KeySkipped event", func() bool { return hasEvent(f.ns, "db", ReasonKeySkipped) })
}

func TestReplacedPodWithTheSameName(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })
	old := getPod(t, f.ns, "db").UID

	must(k8s.Delete(context.Background(), f.pod, client.GracePeriodSeconds(0)))
	eventually(t, "deleted", func() bool {
		return client.IgnoreNotFound(k8s.Get(context.Background(), client.ObjectKeyFromObject(f.pod), &corev1.Pod{})) == nil &&
			k8s.Get(context.Background(), client.ObjectKeyFromObject(f.pod), &corev1.Pod{}) != nil
	})
	must(k8s.Create(context.Background(), newPod(f.ns, "db", f.node.Name, map[string]string{"app": "db"}, nil)))
	eventually(t, "the new pod is echoed", func() bool {
		p := getPod(t, f.ns, "db")
		return p.UID != old && cordoned(p)
	})
}

func TestTerminatingPodStaysInScope(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	p := newPod(f.ns, "finalized", f.node.Name, map[string]string{"app": "db"}, nil)
	p.Finalizers = []string{"example.org/hold"}
	must(k8s.Create(context.Background(), p))
	must(k8s.Delete(context.Background(), p))

	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "terminating pod echoed", func() bool { return cordoned(getPod(t, f.ns, "finalized")) })

	p = getPod(t, f.ns, "finalized")
	p.Finalizers = nil
	must(k8s.Update(context.Background(), p))
}

func TestInvalidLabelValueIsSkippedAndRemoved(t *testing.T) {
	const pos = "rack.example.org/position"
	setRules(t, `- name: positions
  podSelector: {matchLabels: {app: db}}
  annotations: {copy: [rack.example.org/position], target: labels}
`)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Annotations = map[string]string{pos: "row-3"} })
	eventually(t, "copied", func() bool { return hasLabel(getPod(t, f.ns, "db"), pos, "row-3") })

	f.updateNode(t, func(n *corev1.Node) { n.Annotations[pos] = "row 3, slot 12" })
	eventually(t, "invalid value removed", func() bool {
		_, ok := getPod(t, f.ns, "db").Labels[pos]
		return !ok
	})
	eventually(t, "KeySkipped event", func() bool { return hasEvent(f.ns, "db", ReasonKeySkipped) })
}

func TestEmptyRulesRemoveEverythingAndDeletedConfigMapKeepsThem(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: configNS, Name: configName}}
	must(k8s.Delete(context.Background(), cm))
	consistently(t, "keys kept without a ConfigMap", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	setRules(t, "  []\n")
	eventually(t, "rules: [] removes everything", func() bool { return noKeys(getPod(t, f.ns, "db")) })
}

func TestNothingIsWrittenBeforeTheFirstConfiguration(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	// A second instance that has not loaded its rules: it must neither write
	// nor remove anything.
	idle := &PodReconciler{Client: k8s, Store: &Store{}}
	before := getPod(t, f.ns, "db").ResourceVersion
	if _, err := idle.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.pod)}); err != nil {
		t.Fatal(err)
	}
	if p := getPod(t, f.ns, "db"); p.ResourceVersion != before || !cordoned(p) {
		t.Errorf("an unloaded projector touched the pod")
	}
}

func TestManyPodsOnOneNode(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	const n = 300
	for i := range n {
		must(k8s.Create(context.Background(), newPod(f.ns, fmt.Sprintf("p%d", i), f.node.Name, map[string]string{"app": "db"}, nil)))
	}
	count := func(want bool) func() bool {
		return func() bool {
			pods := &corev1.PodList{}
			must(k8s.List(context.Background(), pods, client.InNamespace(f.ns)))
			c := 0
			for i := range pods.Items {
				if cordoned(&pods.Items[i]) == want {
					c++
				}
			}
			return c == n+1
		}
	}
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "all pods echoed", count(true))
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = false })
	eventually(t, "all pods cleaned", count(false))
}

// A pod object read before another writer set a key: the apply conflicts,
// the key is skipped, and what the manager no longer wants is still removed.
func TestApplyConflictSkipsTheKeyAndStillCleansUp(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	ctx := context.Background()
	// A manager outside nodeecho/, so that the running projector ignores it.
	const manager = "test/conflict"
	stale := corev1ac.Pod("db", f.ns).WithLabels(map[string]string{"stale.example.org/k": "old"})
	must(k8s.Apply(ctx, stale, client.FieldOwner(manager)))
	snapshot := getPod(t, f.ns, "db")

	theirs := corev1ac.Pod("db", f.ns).WithLabels(map[string]string{rack: "theirs"})
	must(k8s.Apply(ctx, theirs, client.FieldOwner("someone")))

	rec := events.NewFakeRecorder(10)
	r := &PodReconciler{Client: k8s, Recorder: rec}
	if err := r.sync(ctx, snapshot, "test", manager, projection.Result{Labels: map[string]string{rack: "mine"}}); err != nil {
		t.Fatal(err)
	}
	p := getPod(t, f.ns, "db")
	if !hasLabel(p, rack, "theirs") {
		t.Errorf("the other writer's value was overwritten: %v", p.Labels)
	}
	if _, ok := p.Labels["stale.example.org/k"]; ok {
		t.Errorf("stale key kept after the conflict: %v", p.Labels)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, ReasonKeySkipped) || !strings.Contains(e, rack) {
			t.Errorf("event = %q", e)
		}
	default:
		t.Error("no KeySkipped event")
	}
}

// The pod template sets the same value as the Node, so NodeEcho shares the
// key with the pod's creator. When the Node value changes, NodeEcho's apply
// conflicts: the key is skipped, and the cordon label must still go once
// the Node is uncordoned.
func TestCoOwnedKeyConflictStillRemovesTheCordon(t *testing.T) {
	setRules(t, `- name: db
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
  labels: {copy: [rack.example.org/id]}
`)
	f := newFixture(t, map[string]string{"app": "x"}, map[string]string{rack: "r1"})
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	must(k8s.Create(context.Background(), newPod(f.ns, "shared", f.node.Name, map[string]string{"app": "db", rack: "r1"}, nil)))
	eventually(t, "cordon echoed and rack shared", func() bool {
		p := getPod(t, f.ns, "shared")
		return cordoned(p) && hasLabel(p, rack, "r1")
	})

	f.updateNode(t, func(n *corev1.Node) {
		n.Labels[rack] = "r2"
		n.Spec.Unschedulable = false
	})
	eventually(t, "cordon removed despite the conflict", func() bool {
		p := getPod(t, f.ns, "shared")
		_, u := p.Labels[uKey]
		return !u && hasLabel(p, rack, "r1")
	})
	eventually(t, "KeySkipped event", func() bool { return hasEvent(f.ns, "shared", ReasonKeySkipped) })
}

func TestKeyRemovedByAnotherWriterIsEchoedAgain(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	// As kubectl label key-: the Node is still cordoned, so the echo returns.
	p := getPod(t, f.ns, "db")
	delete(p.Labels, uKey)
	must(k8s.Update(context.Background(), p, client.FieldOwner("kubectl")))
	eventually(t, "echoed again", func() bool { return cordoned(getPod(t, f.ns, "db")) })
}

func TestNodeRecreatedWithAnotherState(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })

	must(k8s.Delete(context.Background(), f.node))
	eventually(t, "node gone", func() bool {
		return k8s.Get(context.Background(), client.ObjectKeyFromObject(f.node), &corev1.Node{}) != nil
	})
	must(k8s.Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: f.node.Name}}))
	eventually(t, "the new, schedulable Node is echoed", func() bool { return noKeys(getPod(t, f.ns, "db")) })
}

func TestSucceededPodIsStillUpdated(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	p := getPod(t, f.ns, "db")
	p.Status.Phase = corev1.PodSucceeded
	must(k8s.Status().Update(context.Background(), p))
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "succeeded pod echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })
}

func TestRejectedWriteIsSkippedNotRetried(t *testing.T) {
	const big = "rack.example.org/inventory"
	setRules(t, `- name: inventory
  podSelector: {matchLabels: {app: db}}
  annotations: {copy: [rack.example.org/inventory]}
`)
	// Each half fits; together they exceed the 256 KiB annotations limit.
	half := strings.Repeat("x", 150*1024)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	must(k8s.Create(context.Background(), newPod(f.ns, "full", f.node.Name, map[string]string{"app": "db"}, map[string]string{"filler": half})))
	f.updateNode(t, func(n *corev1.Node) { n.Annotations = map[string]string{big: half} })

	rec := events.NewFakeRecorder(10)
	r := &PodReconciler{Client: k8s, Store: store, Recorder: rec}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: f.ns, Name: "full"}}); err != nil {
		t.Fatalf("a rejected write must not be retried: %v", err)
	}
	select {
	case e := <-rec.Events:
		if !strings.Contains(e, "rejected") {
			t.Errorf("event = %q", e)
		}
	default:
		t.Error("no event for the rejected write")
	}
	if _, ok := getPod(t, f.ns, "full").Annotations[big]; ok {
		t.Error("the oversized annotation was written")
	}
}

func TestConfigLoaderStates(t *testing.T) {
	if k8s == nil {
		t.Skip("needs envtest")
	}
	ctx := context.Background()
	key := types.NamespacedName{Namespace: configNS, Name: fmt.Sprintf("loader-states-%d", seq.Add(1))}
	resync := make(chan event.GenericEvent, 10000)
	s := &Store{}
	r := &ConfigReconciler{Client: k8s, ConfigMap: key, Store: s, Resync: resync}
	reconcile := func() {
		t.Helper()
		if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}

	reconcile() // missing ConfigMap
	if s.Loaded() {
		t.Fatal("loaded without a ConfigMap")
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: key.Namespace, Name: key.Name},
		Data: map[string]string{"other": "x"}}
	must(k8s.Create(ctx, cm))
	reconcile() // missing key
	if s.Loaded() {
		t.Fatal("loaded without the rules key")
	}
	cm.Data = map[string]string{ConfigKey: "version: 1\n"}
	must(k8s.Update(ctx, cm))
	reconcile() // rules missing
	if s.Loaded() {
		t.Fatal("loaded without a rules field")
	}
	cm.Data = map[string]string{ConfigKey: "version: 1\nrules:\n" + cordonRule}
	must(k8s.Update(ctx, cm))
	reconcile()
	if !s.Loaded() || len(s.Rules()) != 1 {
		t.Fatalf("not loaded: %v", names(s.Rules()))
	}
	pods := &corev1.PodList{}
	must(k8s.List(ctx, pods))
	if len(resync) != len(pods.Items) {
		t.Errorf("resync sent %d pods, want %d", len(resync), len(pods.Items))
	}
	for len(resync) > 0 {
		<-resync
	}
	reconcile() // unchanged rules: no resync
	if len(resync) != 0 {
		t.Errorf("unchanged rules resynced %d pods", len(resync))
	}
}

// A pod deleted while NodeEcho holds it is not an error, whatever form the
// API server gives the failed UID precondition.
func TestWriteToADeletedPodIsNotAnError(t *testing.T) {
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	ctx := context.Background()
	snapshot := getPod(t, f.ns, "db")
	must(k8s.Delete(ctx, snapshot, client.GracePeriodSeconds(0)))
	eventually(t, "deleted", func() bool {
		return apierrors.IsNotFound(k8s.Get(ctx, client.ObjectKeyFromObject(snapshot), &corev1.Pod{}))
	})
	r := &PodReconciler{Client: k8s, Recorder: events.NewFakeRecorder(10)}
	if err := r.sync(ctx, snapshot, "test", "test/gone", projection.Result{Labels: map[string]string{uKey: "true"}}); err != nil {
		t.Fatalf("writing to a deleted pod: %v", err)
	}
}

// The pod is cached while no rule writes the annotation, so its cache entry
// lacks it; a rule then starts writing that key. The foreign value must be
// kept, through the apply conflict.
func TestAnnotationOutsideTheCacheFilterIsStillProtected(t *testing.T) {
	const pos = "rack.example.org/position"
	setRules(t, cordonRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Annotations = map[string]string{pos: "row-1"} })
	must(k8s.Create(context.Background(), newPod(f.ns, "held", f.node.Name, map[string]string{"app": "db"},
		map[string]string{pos: "from-template"})))
	eventually(t, "pod reconciled", func() bool { return len(managers(getPod(t, f.ns, "held"))) == 0 })

	setRules(t, `- name: positions
  podSelector: {matchLabels: {app: db}}
  annotations: {copy: [rack.example.org/position]}
`)
	eventually(t, "KeySkipped event", func() bool { return hasEvent(f.ns, "held", ReasonKeySkipped) })
	consistently(t, "foreign value kept", func() bool {
		return getPod(t, f.ns, "held").Annotations[pos] == "from-template"
	})
}
