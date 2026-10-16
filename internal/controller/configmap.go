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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/fyannk/nodeecho/internal/config"
)

// ConfigKey is the ConfigMap key holding the rules.
const ConfigKey = "rules.yaml"

// ConfigReconciler loads the rules ConfigMap into the Store and, when the
// active rules change, sends every pod to the pod projector.
type ConfigReconciler struct {
	client.Client
	ConfigMap types.NamespacedName
	Store     *Store
	// Resync receives one event per pod after a rule change.
	Resync chan<- event.GenericEvent
}

// Reconcile reads the ConfigMap. A missing ConfigMap, a missing key or an
// invalid document keeps the last valid configuration.
func (r *ConfigReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	if err := r.Get(ctx, r.ConfigMap, cm); err != nil {
		if apierrors.IsNotFound(err) {
			configValid.Set(0)
			configLoads.WithLabelValues("missing").Inc()
			logger.Info("Rules ConfigMap not found, keeping the last valid configuration", "configMap", r.ConfigMap)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	data, ok := cm.Data[ConfigKey]
	if !ok {
		configValid.Set(0)
		configLoads.WithLabelValues("missing").Inc()
		logger.Info("Rules ConfigMap has no rules key, keeping the last valid configuration",
			"configMap", r.ConfigMap, "key", ConfigKey)
		return ctrl.Result{}, nil
	}

	valid, refused, err := config.Parse([]byte(data))
	if err != nil {
		configValid.Set(0)
		configLoads.WithLabelValues("invalid").Inc()
		logger.Error(err, "Invalid configuration, keeping the last valid one", "configMap", r.ConfigMap)
		return ctrl.Result{}, nil
	}
	configValid.Set(1)
	configLoads.WithLabelValues("valid").Inc()
	configLastValid.SetToCurrentTime()
	rulesRefused.Set(float64(len(refused)))
	for _, e := range refused {
		logger.Error(e.Err, "Rule refused", "index", e.Index, "rule", e.Name)
	}

	changed, kept, err := r.Store.Set(valid, refused)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("activating rules: %w", err)
	}
	for _, name := range kept {
		logger.Info("Refused rule keeps its last valid version", "rule", name)
	}
	rulesActive.Set(float64(len(r.Store.Rules())))
	if !changed {
		return ctrl.Result{}, nil
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods); err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("Rules loaded", "active", len(r.Store.Rules()), "refused", len(refused), "podsToResync", len(pods.Items))
	for i := range pods.Items {
		select {
		case r.Resync <- event.GenericEvent{Object: &pods.Items[i]}:
		case <-ctx.Done():
			return ctrl.Result{}, ctx.Err()
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller for the one rules ConfigMap.
func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isRulesConfigMap := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return o.GetNamespace() == r.ConfigMap.Namespace && o.GetName() == r.ConfigMap.Name
	})
	// One reconcile at start, so that a missing ConfigMap is logged.
	start := make(chan event.GenericEvent, 1)
	start <- event.GenericEvent{Object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Namespace: r.ConfigMap.Namespace, Name: r.ConfigMap.Name,
	}}}
	return ctrl.NewControllerManagedBy(mgr).
		Named("rules").
		For(&corev1.ConfigMap{}, builder.WithPredicates(isRulesConfigMap)).
		WatchesRawSource(source.Channel(start, &handler.EnqueueRequestForObject{})).
		Complete(r)
}
