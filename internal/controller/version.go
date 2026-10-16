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
	"fmt"

	kubeversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// KubernetesCopiesTopology reports whether the API server is 1.35 or later,
// where Kubernetes copies the zone and region Node labels onto pods.
func KubernetesCopiesTopology(cfg *rest.Config) (bool, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false, err
	}
	info, err := dc.ServerVersion()
	if err != nil {
		return false, err
	}
	v, err := kubeversion.ParseGeneric(info.GitVersion)
	if err != nil {
		return false, fmt.Errorf("parsing %q: %w", info.GitVersion, err)
	}
	return v.AtLeast(kubeversion.MajorMinor(1, 35)), nil
}
