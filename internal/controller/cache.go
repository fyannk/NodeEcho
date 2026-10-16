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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CacheOptions keeps in the cache only what NodeEcho reads, since it caches
// every pod and Node of the cluster: no pod spec but the node name, no
// status, and only NodeEcho's own field manager entries.
func CacheOptions() map[client.Object]cache.ByObject {
	return map[client.Object]cache.ByObject{
		&corev1.Pod{}:       {Transform: stripPod},
		&corev1.Node{}:      {Transform: stripNode},
		&corev1.Namespace{}: {Transform: stripNamespace},
	}
}

var (
	_ toolscache.TransformFunc = stripPod
	_ toolscache.TransformFunc = stripNode
	_ toolscache.TransformFunc = stripNamespace
)

func meta(m *metav1.ObjectMeta) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:              m.Name,
		Namespace:         m.Namespace,
		UID:               m.UID,
		ResourceVersion:   m.ResourceVersion,
		DeletionTimestamp: m.DeletionTimestamp,
		Labels:            m.Labels,
		Annotations:       m.Annotations,
	}
}

func stripPod(obj any) (any, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return obj, nil
	}
	out := &corev1.Pod{TypeMeta: pod.TypeMeta, ObjectMeta: meta(&pod.ObjectMeta), Spec: corev1.PodSpec{NodeName: pod.Spec.NodeName}}
	for _, m := range pod.ManagedFields {
		if strings.HasPrefix(m.Manager, fieldManagerPrefix) {
			out.ManagedFields = append(out.ManagedFields, m)
		}
	}
	return out, nil
}

func stripNode(obj any) (any, error) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return obj, nil
	}
	return &corev1.Node{TypeMeta: node.TypeMeta, ObjectMeta: meta(&node.ObjectMeta), Spec: corev1.NodeSpec{
		Unschedulable: node.Spec.Unschedulable,
		Taints:        node.Spec.Taints,
	}}, nil
}

func stripNamespace(obj any) (any, error) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return obj, nil
	}
	return &corev1.Namespace{TypeMeta: ns.TypeMeta, ObjectMeta: meta(&ns.ObjectMeta)}, nil
}
