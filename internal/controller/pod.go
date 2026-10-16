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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/fyannk/nodeecho/internal/projection"
)

// NodeNameIndex indexes pods by spec.nodeName.
const NodeNameIndex = "spec.nodeName"

// Event reasons.
const (
	ReasonKeySkipped                = "KeySkipped"
	ReasonKubernetesTopologyMissing = "KubernetesTopologyMissing"
)

// PodReconciler writes each rule's projection on the pods it selects, with
// one server-side apply field manager per rule, and removes what a rule no
// longer writes. See web/docs/reference/contract.md.
type PodReconciler struct {
	client.Client
	Store    *Store
	Recorder events.EventRecorder
	Options  projection.Options
	// Resync delivers the pods to reconcile after a rule change.
	Resync <-chan event.GenericEvent
}

// Reconcile brings one pod in line with every rule.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !r.Store.Loaded() {
		// The first configuration load sends every pod again.
		return ctrl.Result{}, nil
	}

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var node *corev1.Node
	if pod.Spec.NodeName != "" {
		node = &corev1.Node{}
		if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
			if apierrors.IsNotFound(err) {
				// Left as is: the pod garbage collector deletes the pods of a
				// deleted Node, and a Node created later enqueues its pods.
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: pod.Namespace}, ns); err != nil {
		// A namespace missing from the cache would read as having no labels.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var errs []error
	active := map[string]bool{}
	rules := r.Store.Rules() // one snapshot: Set may swap the rules meanwhile
	for i := range rules {
		rule := &rules[i]
		active[rule.FieldManager()] = true
		desired := projection.Result{}
		if node != nil && inScope(rule, ns, pod, node) {
			var err error
			if desired, err = projection.Desired(rule.Rule, node, r.Options); err != nil {
				errs = append(errs, err)
				continue
			}
			r.warnMissingTopology(pod, node, rule)
		}
		errs = append(errs, r.sync(ctx, pod, rule.Name, rule.FieldManager(), desired))
	}

	// Field managers of rules that no longer exist.
	for _, m := range pod.ManagedFields {
		if strings.HasPrefix(m.Manager, fieldManagerPrefix) && m.Operation == "Apply" && !active[m.Manager] {
			active[m.Manager] = true
			errs = append(errs, r.sync(ctx, pod, removedRule, m.Manager, projection.Result{}))
		}
	}
	return ctrl.Result{}, errors.Join(errs...)
}

func inScope(rule *Rule, ns *corev1.Namespace, pod *corev1.Pod, node *corev1.Node) bool {
	return rule.pods.Matches(labels.Set(pod.Labels)) &&
		rule.namespaces.Matches(labels.Set(ns.Labels)) &&
		rule.nodes.Matches(labels.Set(node.Labels))
}

// warnMissingTopology signals a pod lacking a zone or region label that
// Kubernetes should have copied, see web/docs/reference/contract.md.
func (r *PodReconciler) warnMissingTopology(pod *corev1.Pod, node *corev1.Node, rule *Rule) {
	l := rule.Labels
	if l == nil {
		return
	}
	for _, k := range l.Copy {
		if !projection.LeftToKubernetes(l, k, r.Options) {
			continue
		}
		if _, onNode := node.Labels[k]; !onNode {
			continue
		}
		if _, onPod := pod.Labels[k]; !onPod {
			r.Recorder.Eventf(pod, nil, corev1.EventTypeWarning, ReasonKubernetesTopologyMissing, "Echo",
				"rule %s: label %s is on node %s but not on the pod; Kubernetes did not copy it, set labels.forceKubernetesManaged to copy it",
				rule.Name, k, node.Name)
		}
	}
}

// removedRule names a rule that no longer exists in metrics and Events:
// anyone able to patch a pod can apply under a nodeecho/ manager name, so
// that name is not used as a metric label.
const removedRule = "(removed)"

// sync applies the desired keys of one field manager, skipping the keys
// another writer owns, and writes nothing when the pod already matches.
func (r *PodReconciler) sync(ctx context.Context, pod *corev1.Pod, rule, manager string, desired projection.Result) error {
	for _, s := range desired.Skipped {
		r.skip(pod, rule, s.Key, "invalid-value", s.Reason)
	}
	ownedLabels, ownedAnnotations := ownedKeys(pod, manager)
	wantLabels := r.withoutForeign(pod, rule, desired.Labels, pod.Labels, ownedLabels)
	wantAnnotations := r.withoutForeign(pod, rule, desired.Annotations, pod.Annotations, ownedAnnotations)

	retry := false
	for attempt := 0; ; attempt++ {
		// After a conflict the pod object is stale, so apply anyway.
		if !retry && upToDate(wantLabels, pod.Labels, ownedLabels) && upToDate(wantAnnotations, pod.Annotations, ownedAnnotations) {
			return nil
		}
		ac := corev1ac.Pod(pod.Name, pod.Namespace).WithUID(pod.UID)
		if len(wantLabels) > 0 {
			ac.WithLabels(wantLabels)
		}
		if len(wantAnnotations) > 0 {
			ac.WithAnnotations(wantAnnotations)
		}
		err := r.Apply(ctx, ac, client.FieldOwner(manager))
		switch {
		case err == nil:
			podWrites.WithLabelValues(rule, "ok").Inc()
			log.FromContext(ctx).V(1).Info("pod updated", "rule", rule, "labels", wantLabels, "annotations", wantAnnotations)
			return nil
		case apierrors.IsNotFound(err), apierrors.IsInvalid(err) && strings.Contains(err.Error(), "metadata.uid"):
			podWrites.WithLabelValues(rule, "gone").Inc()
			return nil // the pod was deleted or replaced; the new one has its own event
		case apierrors.IsInvalid(err), apierrors.IsForbidden(err):
			// The pod refuses the write, for instance annotations over the
			// size limit or no patch right in its namespace: retrying cannot
			// help until something changes, which brings a new event.
			podWrites.WithLabelValues(rule, "rejected").Inc()
			keysSkipped.WithLabelValues(rule, "rejected").Inc()
			r.Recorder.Eventf(pod, nil, corev1.EventTypeWarning, ReasonKeySkipped, "Echo", "rule %s: write rejected: %v", rule, err)
			return nil
		case apierrors.IsConflict(err) && attempt < 3:
			podWrites.WithLabelValues(rule, "conflict").Inc()
			keys := conflictingKeys(err)
			if len(keys) == 0 {
				return err
			}
			for _, k := range keys {
				if _, ok := wantLabels[k.key]; ok && k.labels {
					delete(wantLabels, k.key)
					r.skip(pod, rule, k.key, "owned-by-other", "label owned by another writer")
				}
				if _, ok := wantAnnotations[k.key]; ok && !k.labels {
					delete(wantAnnotations, k.key)
					r.skip(pod, rule, k.key, "owned-by-other", "annotation owned by another writer")
				}
			}
			retry = true
		default:
			podWrites.WithLabelValues(rule, "error").Inc()
			return fmt.Errorf("applying rule %s to pod %s/%s: %w", rule, pod.Namespace, pod.Name, err)
		}
	}
}

// withoutForeign drops the keys that exist on the pod with another value and
// that this manager does not own: another writer set them, and NodeEcho never
// overwrites another writer's key.
func (r *PodReconciler) withoutForeign(pod *corev1.Pod, rule string, want, current map[string]string, owned map[string]bool) map[string]string {
	out := make(map[string]string, len(want))
	for k, v := range want {
		if cur, exists := current[k]; exists && cur != v && !owned[k] {
			r.skip(pod, rule, k, "owned-by-other", "key set by another writer")
			continue
		}
		out[k] = v
	}
	return out
}

func (r *PodReconciler) skip(pod *corev1.Pod, rule, key, reason, detail string) {
	keysSkipped.WithLabelValues(rule, reason).Inc()
	r.Recorder.Eventf(pod, nil, corev1.EventTypeWarning, ReasonKeySkipped, "Echo", "rule %s: key %s not written: %s", rule, key, detail)
}

// upToDate reports whether the pod already has every wanted value and the
// manager owns exactly the wanted keys.
func upToDate(want, current map[string]string, owned map[string]bool) bool {
	if len(want) != len(owned) {
		return false
	}
	for k, v := range want {
		if !owned[k] {
			return false
		}
		if cur, ok := current[k]; !ok || cur != v {
			return false
		}
	}
	return true
}

// ownedKeys returns the label and annotation keys a field manager owns on the
// pod, from its managedFields entry.
func ownedKeys(pod *corev1.Pod, manager string) (labelKeys, annotationKeys map[string]bool) {
	labelKeys, annotationKeys = map[string]bool{}, map[string]bool{}
	for _, m := range pod.ManagedFields {
		if m.Manager != manager || m.Operation != "Apply" || m.FieldsV1 == nil {
			continue
		}
		var fields struct {
			Metadata struct {
				Labels      map[string]json.RawMessage `json:"f:labels"`
				Annotations map[string]json.RawMessage `json:"f:annotations"`
			} `json:"f:metadata"`
		}
		if err := json.Unmarshal(m.FieldsV1.GetRawBytes(), &fields); err != nil {
			continue
		}
		for k := range fields.Metadata.Labels {
			if key, ok := strings.CutPrefix(k, "f:"); ok {
				labelKeys[key] = true
			}
		}
		for k := range fields.Metadata.Annotations {
			if key, ok := strings.CutPrefix(k, "f:"); ok {
				annotationKeys[key] = true
			}
		}
	}
	return labelKeys, annotationKeys
}

type metadataKey struct {
	key    string
	labels bool
}

// conflictingKeys extracts the label and annotation keys of a server-side
// apply conflict, whose causes name fields as ".metadata.labels.<key>".
func conflictingKeys(err error) []metadataKey {
	var status apierrors.APIStatus
	if !errors.As(err, &status) || status.Status().Details == nil {
		return nil
	}
	var keys []metadataKey
	for _, c := range status.Status().Details.Causes {
		if k, ok := strings.CutPrefix(c.Field, ".metadata.labels."); ok {
			keys = append(keys, metadataKey{key: k, labels: true})
		} else if k, ok := strings.CutPrefix(c.Field, ".metadata.annotations."); ok {
			keys = append(keys, metadataKey{key: k})
		}
	}
	return keys
}

// SetupWithManager registers the pod projector and its watches.
func (r *PodReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, NodeNameIndex, func(o client.Object) []string {
		if name := o.(*corev1.Pod).Spec.NodeName; name != "" {
			return []string{name}
		}
		return nil
	}); err != nil {
		return err
	}

	podsOnNode := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
		return r.requests(ctx, client.MatchingFields{NodeNameIndex: o.GetName()})
	})
	podsInNamespace := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
		return r.requests(ctx, client.InNamespace(o.GetName()))
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("pods").
		For(&corev1.Pod{}, builder.WithPredicates(podMetadataChanged)).
		Watches(&corev1.Node{}, podsOnNode, builder.WithPredicates(nodeEchoedFieldsChanged(r.Store.CopiesAnnotations))).
		Watches(&corev1.Namespace{}, podsInNamespace, builder.WithPredicates(predicate.LabelChangedPredicate{})).
		WatchesRawSource(source.Channel(r.Resync, &handler.EnqueueRequestForObject{})).
		Complete(r)
}

func (r *PodReconciler) requests(ctx context.Context, opts ...client.ListOption) []ctrl.Request {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, opts...); err != nil {
		log.FromContext(ctx).Error(err, "listing pods to enqueue")
		return nil
	}
	reqs := make([]ctrl.Request, 0, len(pods.Items))
	for _, p := range pods.Items {
		reqs = append(reqs, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: p.Namespace, Name: p.Name}})
	}
	return reqs
}

// podMetadataChanged lets through creations and the updates that can change
// a projection: labels (selection), annotations and node assignment.
// Status-only updates are ignored.
var podMetadataChanged = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		o, n := e.ObjectOld.(*corev1.Pod), e.ObjectNew.(*corev1.Pod)
		return o.Spec.NodeName != n.Spec.NodeName ||
			!maps.Equal(o.Labels, n.Labels) ||
			!maps.Equal(o.Annotations, n.Annotations)
	},
	DeleteFunc: func(event.DeleteEvent) bool { return false },
}

// nodeEchoedFieldsChanged ignores Node status updates such as heartbeats,
// and annotation changes while no rule copies annotations: CNI, CSI and
// cloud controllers update Node annotations often. A rule change resyncs
// every pod anyway.
func nodeEchoedFieldsChanged(copiesAnnotations func() bool) predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			o, n := e.ObjectOld.(*corev1.Node), e.ObjectNew.(*corev1.Node)
			return o.Spec.Unschedulable != n.Spec.Unschedulable ||
				!equality.Semantic.DeepEqual(o.Spec.Taints, n.Spec.Taints) ||
				!maps.Equal(o.Labels, n.Labels) ||
				(!maps.Equal(o.Annotations, n.Annotations) && copiesAnnotations())
		},
		DeleteFunc: func(event.DeleteEvent) bool { return false },
	}
}
