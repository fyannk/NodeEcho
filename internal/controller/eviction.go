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
	"net/http"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/fyannk/nodeecho/internal/config"
)

// EvictionPath is the path the eviction webhook is served on.
const EvictionPath = "/evictions"

// Event reasons of the eviction webhook.
const (
	ReasonEvictionRequested = "EvictionRequested"
	ReasonEvictionRefused   = "EvictionRefused"
)

// evictionManagerPrefix is the field manager of the eviction marks. It is
// not under nodeecho/, so the pod projector never removes a mark.
const evictionManagerPrefix = "nodeecho-eviction/"

// EvictionWebhook answers the pods/eviction admission requests: for a pod
// that rules with an eviction block select, it records when NodeEcho first
// received an eviction request, and allows or refuses it as configured.
// See web/docs/reference/contract.md.
type EvictionWebhook struct {
	Client   client.Client
	Store    *Store
	Recorder events.EventRecorder
	// Now returns the time a request is received; time.Now by default.
	Now func() time.Time
}

var _ admission.Handler = &EvictionWebhook{}

// Handle answers one eviction request.
func (w *EvictionWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	received := time.Now()
	if w.Now != nil {
		received = w.Now()
	}
	if req.Operation != admissionv1.Create || req.SubResource != "eviction" {
		return admission.Allowed("")
	}
	if !w.Store.Loaded() {
		return admission.Allowed("no NodeEcho configuration loaded")
	}
	logger := log.FromContext(ctx).WithValues("pod", types.NamespacedName{Namespace: req.Namespace, Name: req.Name})

	pod := &corev1.Pod{}
	if err := w.Client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, pod); err != nil {
		return admission.Allowed("")
	}
	if pod.Spec.NodeName == "" {
		return admission.Allowed("")
	}
	node := &corev1.Node{}
	if err := w.Client.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
		return admission.Allowed("")
	}
	ns := &corev1.Namespace{}
	if err := w.Client.Get(ctx, types.NamespacedName{Name: pod.Namespace}, ns); err != nil {
		return admission.Allowed("")
	}

	dryRun := req.DryRun != nil && *req.DryRun
	var refusal []string
	rules := w.Store.Rules()
	for i := range rules {
		rule := &rules[i]
		if rule.Eviction == nil || !inScope(rule, ns, pod, node) {
			continue
		}
		response := rule.Eviction.Response
		evictions.WithLabelValues(rule.Name, response).Inc()
		if response == config.EvictionDeny {
			refusal = append(refusal, rule.Eviction.Message)
		}
		if dryRun {
			continue
		}
		if _, marked := pod.Annotations[rule.Eviction.Key]; !marked {
			if err := w.mark(ctx, pod, rule, received); err != nil {
				logger.Error(err, "Unable to record the eviction request", "rule", rule.Name)
			}
		}
		w.event(pod, rule, response)
		logger.Info("Eviction request received", "rule", rule.Name, "response", response)
	}

	if len(refusal) == 0 {
		return admission.Allowed("")
	}
	// 429, as a PodDisruptionBudget answers: kubectl drain and the cluster
	// autoscalers retry it instead of failing.
	resp := admission.Denied(strings.Join(refusal, "; "))
	resp.Result.Code = http.StatusTooManyRequests
	resp.Result.Reason = metav1.StatusReasonTooManyRequests
	return resp
}

// mark records the time of the request on the pod, with the UID
// precondition: a pod replaced meanwhile is not marked.
func (w *EvictionWebhook) mark(ctx context.Context, pod *corev1.Pod, rule *Rule, received time.Time) error {
	ac := corev1ac.Pod(pod.Name, pod.Namespace).WithUID(pod.UID).
		WithAnnotations(map[string]string{rule.Eviction.Key: received.UTC().Format(time.RFC3339)})
	err := w.Client.Apply(ctx, ac, client.FieldOwner(evictionManagerPrefix+rule.Name))
	if podGone(err) {
		return nil
	}
	return err
}

func (w *EvictionWebhook) event(pod *corev1.Pod, rule *Rule, response string) {
	if response == config.EvictionDeny {
		w.Recorder.Eventf(pod, nil, corev1.EventTypeWarning, ReasonEvictionRefused, "Evict",
			"rule %s: eviction request refused: %s", rule.Name, rule.Eviction.Message)
		return
	}
	w.Recorder.Eventf(pod, nil, corev1.EventTypeNormal, ReasonEvictionRequested, "Evict",
		"rule %s: eviction request received", rule.Name)
}
