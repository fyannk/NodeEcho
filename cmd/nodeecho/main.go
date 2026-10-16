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
	stdlog "log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"github.com/go-logr/logr"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
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
	var metricsAddr, metricsCertDir, probeAddr, pprofAddr, configName, configNamespace string
	var leaderElect, metricsSecure, showVersion bool
	flag.StringVar(&configName, "config-name", "nodeecho", "Name of the rules ConfigMap.")
	flag.StringVar(&configNamespace, "config-namespace", os.Getenv("POD_NAMESPACE"),
		"Namespace of the rules ConfigMap, NodeEcho's own namespace (default $POD_NAMESPACE).")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8443", "Address the metrics endpoint binds to; \"0\" disables it.")
	flag.BoolVar(&metricsSecure, "metrics-secure", true,
		"Serve metrics over HTTPS, to clients the API server authenticates and authorizes for GET /metrics.")
	flag.StringVar(&metricsCertDir, "metrics-cert-dir", "",
		"Directory with tls.crt and tls.key for the metrics endpoint; a self-signed certificate otherwise.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probes bind to.")
	flag.StringVar(&pprofAddr, "pprof-bind-address", "",
		"Address the pprof endpoint binds to, for instance 127.0.0.1:6060; empty disables it. Unauthenticated.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election, for more than one replica.")
	flag.BoolVar(&showVersion, "version", false, "Print the version and exit.")
	// Production defaults: JSON lines, ISO 8601 times, stack traces on
	// panics only. The --zap-* flags override them.
	opts := zap.Options{
		Development:     false,
		TimeEncoder:     zapcore.ISO8601TimeEncoder,
		StacktraceLevel: zapcore.DPanicLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if showVersion {
		_, _ = fmt.Fprintln(os.Stdout, "nodeecho", version, goVersion())
		return
	}
	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	// client-go logs through klog, leader election included: route it through
	// the same logger, so that every line has the same format.
	klog.SetLogger(logger)
	// So is the standard library's log package, which net/http uses for
	// errors such as failed TLS handshakes on the metrics endpoint.
	stdlog.SetFlags(0)
	stdlog.SetOutput(stdlibWriter{logger.WithName("stdlib")})

	if configNamespace == "" {
		setupLog.Error(errors.New("no namespace"), "Set --config-namespace or POD_NAMESPACE")
		os.Exit(1)
	}

	restConfig := ctrl.GetConfigOrDie()
	copiesTopology, err := controller.KubernetesCopiesTopology(restConfig)
	if err != nil {
		setupLog.Error(err, "Unable to read the Kubernetes version")
		os.Exit(1)
	}
	controller.SetBuildInfo(version, goVersion())

	metricsOpts := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: metricsSecure,
		CertDir:       metricsCertDir,
	}
	if metricsSecure {
		// Requires the tokenreviews and subjectaccessreviews create rights.
		metricsOpts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		// NodeEcho reads ConfigMaps in its own namespace only, and caches
		// pods, Nodes and Namespaces stripped to what it reads.
		Cache:                  cache.Options{ByObject: cacheOptions(configNamespace)},
		Metrics:                metricsOpts,
		HealthProbeBindAddress: probeAddr,
		PprofBindAddress:       pprofAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "nodeecho.fyannk.dev",
		// main exits as soon as the manager stops, so the lease can be
		// released at once and the next instance takes over quickly.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Unable to create the manager")
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
		setupLog.Error(err, "Unable to set up the rules controller")
		os.Exit(1)
	}
	if err := (&controller.PodReconciler{
		Client:   mgr.GetClient(),
		Store:    store,
		Recorder: mgr.GetEventRecorder("nodeecho"),
		Options:  projection.Options{KubernetesCopiesTopology: copiesTopology},
		Resync:   resync,
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "Unable to set up the pod controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up the health check")
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
		setupLog.Error(err, "Unable to set up the ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager", "version", version, "goVersion", goVersion(),
		"configMap", configNamespace+"/"+configName, "kubernetesCopiesTopology", copiesTopology,
		"leaderElection", leaderElect, "metricsAddress", metricsAddr, "metricsSecure", metricsSecure,
		"pprofAddress", pprofAddr)
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "Manager stopped")
		os.Exit(1)
	}
}

// stdlibWriter turns lines of the standard library's log package into
// structured log entries.
type stdlibWriter struct{ logger logr.Logger }

func (w stdlibWriter) Write(p []byte) (int, error) {
	w.logger.Info("Go standard library log", "line", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func goVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return "unknown"
}

func cacheOptions(configNamespace string) map[client.Object]cache.ByObject {
	opts := controller.CacheOptions()
	opts[&corev1.ConfigMap{}] = cache.ByObject{Namespaces: map[string]cache.Config{configNamespace: {}}}
	return opts
}
