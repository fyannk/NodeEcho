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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nodeecho_build_info",
		Help: "Always 1, labelled with the NodeEcho version and the Go version it was built with.",
	}, []string{"version", "go_version"})
	configLoads = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nodeecho_config_loads_total",
		Help: "Reads of the rules ConfigMap, by result: valid, invalid or missing.",
	}, []string{"result"})
	configLastValid = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nodeecho_config_last_valid_timestamp_seconds",
		Help: "Time of the last read of a valid configuration, in seconds since the epoch.",
	})
	configValid = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nodeecho_config_valid",
		Help: "1 when the last read configuration was valid, 0 when NodeEcho keeps an earlier one.",
	})
	rulesActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nodeecho_rules_active",
		Help: "Number of rules applied.",
	})
	rulesRefused = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nodeecho_rules_refused",
		Help: "Number of rules refused in the last read configuration.",
	})
	keysSkipped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nodeecho_keys_skipped_total",
		Help: "Configured keys not written on a pod, by rule and reason.",
	}, []string{"rule", "reason"})
	podWrites = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nodeecho_pod_writes_total",
		Help: "Server-side apply requests sent to pods, by rule and result.",
	}, []string{"rule", "result"})
)

func init() {
	metrics.Registry.MustRegister(buildInfo, configLoads, configLastValid, configValid, rulesActive, rulesRefused,
		keysSkipped, podWrites)
}

// SetBuildInfo publishes nodeecho_build_info.
func SetBuildInfo(version, goVersion string) {
	buildInfo.Reset()
	buildInfo.WithLabelValues(version, goVersion).Set(1)
}
