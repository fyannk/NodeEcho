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

// Command nodeecho runs the NodeEcho controller manager.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubeversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/fyannk/nodeecho/internal/controller"
	"github.com/fyannk/nodeecho/internal/projection"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	version  = "development"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var metricsAddr, probeAddr, configName, configNamespace string
	var leaderElect bool
	flag.StringVar(&configName, "config-name", "nodeecho", "Name of the rules ConfigMap.")
	flag.StringVar(&configNamespace, "config-namespace", os.Getenv("POD_NAMESPACE"),
		"Namespace of the rules ConfigMap, NodeEcho's own namespace (default $POD_NAMESPACE).")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the metrics endpoint binds to; \"0\" disables it.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probes bind to.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election, for more than one replica.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if configNamespace == "" {
		setupLog.Error(errors.New("no namespace"), "set --config-namespace or POD_NAMESPACE")
		os.Exit(1)
	}

	restConfig := ctrl.GetConfigOrDie()
	copiesTopology, err := kubernetesCopiesTopology(restConfig)
	if err != nil {
		setupLog.Error(err, "unable to read the Kubernetes version")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		// NodeEcho reads ConfigMaps in its own namespace only.
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&corev1.ConfigMap{}: {Namespaces: map[string]cache.Config{configNamespace: {}}},
		}},
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "nodeecho.fyannk.dev",
		// main exits as soon as the manager stops, so the lease can be
		// released at once and the next instance takes over quickly.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create the manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	store := &controller.Store{}
	resync := make(chan event.GenericEvent)
	if err := (&controller.ConfigReconciler{
		Client:    mgr.GetClient(),
		ConfigMap: types.NamespacedName{Namespace: configNamespace, Name: configName},
		Store:     store,
		Resync:    resync,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up the rules controller")
		os.Exit(1)
	}
	if err := (&controller.PodReconciler{
		Client:   mgr.GetClient(),
		Store:    store,
		Recorder: mgr.GetEventRecorder("nodeecho"),
		Options:  projection.Options{KubernetesCopiesTopology: copiesTopology},
		Resync:   resync,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to set up the pod controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up the health check")
		os.Exit(1)
	}
	// The elected instance is ready once a valid configuration is loaded:
	// before that, it writes nothing. A standby instance runs no controller
	// and is ready, so that a rolling update can start the next instance
	// while the current one holds the lease.
	if err := mgr.AddReadyzCheck("readyz", func(*http.Request) error {
		select {
		case <-mgr.Elected():
		default:
			return nil
		}
		if !store.Loaded() {
			return errors.New("no valid configuration loaded")
		}
		return nil
	}); err != nil {
		setupLog.Error(err, "unable to set up the ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "version", version,
		"configMap", configNamespace+"/"+configName, "kubernetesCopiesTopology", copiesTopology)
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "manager stopped")
		os.Exit(1)
	}
}

// kubernetesCopiesTopology reports whether the API server is 1.35 or later,
// where Kubernetes copies the zone and region Node labels onto pods.
func kubernetesCopiesTopology(cfg *rest.Config) (bool, error) {
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
