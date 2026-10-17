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
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/fyannk/nodeecho/internal/config"
)

const evictKey = config.DefaultEvictionKey

func evictionRequest(ns, pod string, dryRun bool) admission.Request {
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Operation:   admissionv1.Create,
		SubResource: "eviction",
		Namespace:   ns,
		Name:        pod,
		DryRun:      ptr.To(dryRun),
	}}
}

func evictionWebhook(at time.Time) (*EvictionWebhook, *events.FakeRecorder) {
	rec := events.NewFakeRecorder(20)
	return &EvictionWebhook{Client: k8s, Store: store, Recorder: rec, Now: func() time.Time { return at }}, rec
}

func TestEvictionMarkAndAllow(t *testing.T) {
	setRules(t, `- name: drain
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
  eviction: {}
`)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	first := time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)
	w, rec := evictionWebhook(first)

	// A dry run answers but writes nothing.
	if resp := w.Handle(context.Background(), evictionRequest(f.ns, "db", true)); !resp.Allowed {
		t.Fatalf("dry run refused: %+v", resp.Result)
	}
	if _, ok := getPod(t, f.ns, "db").Annotations[evictKey]; ok {
		t.Fatal("a dry run marked the pod")
	}

	if resp := w.Handle(context.Background(), evictionRequest(f.ns, "db", false)); !resp.Allowed {
		t.Fatalf("eviction refused: %+v", resp.Result)
	}
	eventually(t, "marked with the reception time", func() bool {
		return getPod(t, f.ns, "db").Annotations[evictKey] == "2026-10-16T09:30:00Z"
	})
	if e := <-rec.Events; !strings.Contains(e, ReasonEvictionRequested) {
		t.Errorf("event = %q", e)
	}

	// A retry, as kubectl drain does, keeps the first time.
	w.Now = func() time.Time { return first.Add(5 * time.Second) }
	eventually(t, "the cache sees the mark", func() bool {
		return w.Handle(context.Background(), evictionRequest(f.ns, "db", false)).Allowed
	})
	consistently(t, "first reception kept", func() bool {
		return getPod(t, f.ns, "db").Annotations[evictKey] == "2026-10-16T09:30:00Z"
	})

	// The pod projector leaves the mark alone, through a cordon and back.
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "cordon echoed", func() bool { return cordoned(getPod(t, f.ns, "db")) })
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = false })
	eventually(t, "uncordon echoed", func() bool { return !cordoned(getPod(t, f.ns, "db")) })
	if getPod(t, f.ns, "db").Annotations[evictKey] != "2026-10-16T09:30:00Z" {
		t.Error("the pod projector removed the eviction mark")
	}
}

func TestEvictionDeny(t *testing.T) {
	setRules(t, `- name: hold
  podSelector: {matchLabels: {app: db}}
  eviction: {key: example.org/evicted, response: deny, message: "switch over first"}
`)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	w, rec := evictionWebhook(time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
	resp := w.Handle(context.Background(), evictionRequest(f.ns, "db", false))
	if resp.Allowed || resp.Result.Code != http.StatusTooManyRequests || resp.Result.Message != "switch over first" {
		t.Fatalf("response = allowed %v, %+v; want 429 with the message", resp.Allowed, resp.Result)
	}
	eventually(t, "marked even when refused", func() bool {
		return getPod(t, f.ns, "db").Annotations["example.org/evicted"] == "2026-10-16T10:00:00Z"
	})
	if e := <-rec.Events; !strings.Contains(e, ReasonEvictionRefused) || !strings.Contains(e, "switch over first") {
		t.Errorf("event = %q", e)
	}
}

func TestEvictionPassesThrough(t *testing.T) {
	setRules(t, `- name: drain
  podSelector: {matchLabels: {app: db}}
  eviction: {response: deny}
`)
	f := newFixture(t, map[string]string{"app": "web"}, nil)
	w, _ := evictionWebhook(time.Now())
	ctx := context.Background()

	for name, req := range map[string]admission.Request{
		"unselected pod": evictionRequest(f.ns, "db", false),
		"missing pod":    evictionRequest(f.ns, "nothing", false),
		"other subresource": {AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create, SubResource: "binding", Namespace: f.ns, Name: "db"}},
	} {
		if resp := w.Handle(ctx, req); !resp.Allowed {
			t.Errorf("%s: refused %+v", name, resp.Result)
		}
	}
	if len(getPod(t, f.ns, "db").Annotations) != 0 {
		t.Error("an unselected pod was marked")
	}

	pending := newPod(f.ns, "pending", "", map[string]string{"app": "db"}, nil)
	must(k8s.Create(ctx, pending))
	if resp := w.Handle(ctx, evictionRequest(f.ns, "pending", false)); !resp.Allowed {
		t.Error("an unbound pod is out of scope and must pass")
	}

	unloaded := &EvictionWebhook{Client: k8s, Store: &Store{}, Recorder: events.NewFakeRecorder(1)}
	must(k8s.Create(ctx, newPod(f.ns, "selected", f.node.Name, map[string]string{"app": "db"}, nil)))
	if resp := unloaded.Handle(ctx, evictionRequest(f.ns, "selected", false)); !resp.Allowed {
		t.Error("before a configuration is loaded, every eviction must pass")
	}
}
