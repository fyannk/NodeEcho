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
	"io"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/fyannk/nodeecho/internal/config"
	"github.com/fyannk/nodeecho/internal/projection"
)

// The tests run NodeEcho against a real API server (envtest). Without
// KUBEBUILDER_ASSETS they are skipped and only the unit tests run; `make
// test` sets it.

const (
	configNS   = "nodeecho-system"
	configName = "nodeecho"
	uKey       = config.DefaultUnschedulableKey
	tKey       = config.DefaultTaintsKey
	zone       = "topology.kubernetes.io/zone"
	rack       = "rack.example.org/id"
)

var (
	// copiesTopology is true on Kubernetes 1.35 and later, as in main.
	copiesTopology bool
	k8s            client.Client
	store          *Store
	seq            atomic.Int32
)

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Println("KUBEBUILDER_ASSETS unset, skipping the envtest suite")
		os.Exit(m.Run())
	}
	logs := io.Discard
	if os.Getenv("NODEECHO_TEST_LOGS") != "" {
		logs = os.Stderr
	}
	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(logs)))
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		panic(err)
	}
	code := run(m, cfg)
	_ = env.Stop()
	os.Exit(code)
}

func run(m *testing.M, cfg *rest.Config) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache:   cache.Options{ByObject: CacheOptions()},
	})
	if err != nil {
		panic(err)
	}
	if copiesTopology, err = KubernetesCopiesTopology(cfg); err != nil {
		panic(err)
	}
	store = &Store{}
	resync := make(chan event.GenericEvent)
	if err := (&ConfigReconciler{
		Client: mgr.GetClient(), ConfigMap: types.NamespacedName{Namespace: configNS, Name: configName},
		Store: store, Resync: resync,
	}).SetupWithManager(mgr); err != nil {
		panic(err)
	}
	if err := (&PodReconciler{
		Client: mgr.GetClient(), Store: store, Recorder: mgr.GetEventRecorder("nodeecho"),
		Options: projection.Options{KubernetesCopiesTopology: copiesTopology}, Resync: resync,
	}).SetupWithManager(ctx, mgr); err != nil {
		panic(err)
	}
	go func() {
		if err := mgr.Start(ctx); err != nil {
			panic(err)
		}
	}()

	k8s, err = client.New(cfg, client.Options{})
	if err != nil {
		panic(err)
	}
	must(k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: configNS}}))
	return m.Run()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// setRules writes the rules ConfigMap and waits until the Store holds them.
func setRules(t *testing.T, rules string) {
	t.Helper()
	if k8s == nil {
		t.Skip("needs envtest")
	}
	ctx := context.Background()
	data := "version: 1\nrules:\n" + rules
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: configNS, Name: configName}}
	_, err := ctrl.CreateOrUpdate(ctx, k8s, cm, func() error {
		cm.Data = map[string]string{ConfigKey: data}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := config.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "rules loaded", func() bool {
		if !store.Loaded() || len(store.Rules()) != len(want) {
			return false
		}
		for i := range want {
			if store.Rules()[i].Name != want[i].Name {
				return false
			}
		}
		return true
	})
}

// fixture creates a namespace, a node and a pod bound to it.
type fixture struct {
	ns   string
	node *corev1.Node
	pod  *corev1.Pod
}

func newFixture(t *testing.T, podLabels, nodeLabels map[string]string) *fixture {
	t.Helper()
	ctx := context.Background()
	n := seq.Add(1)
	f := &fixture{ns: fmt.Sprintf("t%d", n)}
	must(k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: f.ns}}))
	f.node = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", n), Labels: nodeLabels}}
	must(k8s.Create(ctx, f.node))
	f.pod = newPod(f.ns, "db", f.node.Name, podLabels, nil)
	must(k8s.Create(ctx, f.pod))
	return f
}

func newPod(ns, name, node string, labels, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels, Annotations: annotations},
		Spec: corev1.PodSpec{
			NodeName:   node,
			Containers: []corev1.Container{{Name: "c", Image: "busybox"}},
		},
	}
}

func (f *fixture) updateNode(t *testing.T, mutate func(*corev1.Node)) {
	t.Helper()
	ctx := context.Background()
	node := &corev1.Node{}
	must(k8s.Get(ctx, client.ObjectKeyFromObject(f.node), node))
	mutate(node)
	if err := k8s.Update(ctx, node); err != nil {
		t.Fatal(err)
	}
}

func getPod(t *testing.T, ns, name string) *corev1.Pod {
	t.Helper()
	p := &corev1.Pod{}
	if err := k8s.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// consistently fails if cond becomes false within a short window.
func consistently(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !cond() {
			t.Fatalf("%s did not hold", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func hasLabel(p *corev1.Pod, k, v string) bool { got, ok := p.Labels[k]; return ok && got == v }

func managers(p *corev1.Pod) []string {
	var out []string
	for _, m := range p.ManagedFields {
		if strings.HasPrefix(m.Manager, fieldManagerPrefix) {
			out = append(out, m.Manager)
		}
	}
	return out
}

const dbRule = `- name: db
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
  taints: {copy: [karpenter.sh/disrupted]}
  labels: {copy: [rack.example.org/id]}
`

func TestCordonTaintAndLabelEcho(t *testing.T) {
	setRules(t, dbRule)
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{rack: "a"})
	other := newPod(f.ns, "other", f.node.Name, map[string]string{"app": "web"}, nil)
	must(k8s.Create(context.Background(), other))

	eventually(t, "rack copied", func() bool { return hasLabel(getPod(t, f.ns, "db"), rack, "a") })
	if p := getPod(t, f.ns, "db"); p.Labels[uKey] != "" || p.Annotations[tKey] != "" {
		t.Fatalf("healthy node: unexpected keys %v %v", p.Labels, p.Annotations)
	}

	f.updateNode(t, func(n *corev1.Node) {
		n.Spec.Unschedulable = true
		n.Spec.Taints = []corev1.Taint{
			{Key: "karpenter.sh/disrupted", Effect: corev1.TaintEffectNoSchedule},
			{Key: "dedicated", Effect: corev1.TaintEffectNoSchedule},
		}
		n.Labels[rack] = "b"
	})
	eventually(t, "cordon, taint and new rack echoed", func() bool {
		p := getPod(t, f.ns, "db")
		return hasLabel(p, uKey, "true") && hasLabel(p, rack, "b") &&
			p.Annotations[tKey] == `[{"key":"karpenter.sh/disrupted","effect":"NoSchedule"}]`
	})

	f.updateNode(t, func(n *corev1.Node) {
		n.Spec.Unschedulable = false
		n.Spec.Taints = nil
		delete(n.Labels, rack)
	})
	eventually(t, "keys removed", func() bool {
		p := getPod(t, f.ns, "db")
		_, u := p.Labels[uKey]
		_, z := p.Labels[rack]
		_, tt := p.Annotations[tKey]
		return !u && !z && !tt && len(managers(p)) == 0
	})

	if p := getPod(t, f.ns, "other"); len(managers(p)) != 0 {
		t.Errorf("unselected pod was written: %v", managers(p))
	}
}

func TestHealthyPodIsNeverWritten(t *testing.T) {
	setRules(t, dbRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	consistently(t, "no write on a healthy, unlabelled node", func() bool {
		return len(managers(getPod(t, f.ns, "db"))) == 0
	})
}

func TestUnboundPodIsIgnored(t *testing.T) {
	setRules(t, dbRule)
	f := newFixture(t, map[string]string{"app": "x"}, nil)
	pending := newPod(f.ns, "pending", "", map[string]string{"app": "db"}, nil)
	must(k8s.Create(context.Background(), pending))
	consistently(t, "unbound pod untouched", func() bool {
		return len(managers(getPod(t, f.ns, "pending"))) == 0
	})
}

func TestForeignKeyIsNeverOverwritten(t *testing.T) {
	// Not the zone label: Kubernetes 1.35+ itself sets it on pods created
	// bound to a node.
	setRules(t, `- name: racks
  podSelector: {matchLabels: {app: db}}
  unschedulable: {}
  labels: {copy: [rack.example.org/id]}
`)
	f := newFixture(t, map[string]string{"app": "x"}, map[string]string{rack: "r1"})
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	p := newPod(f.ns, "templated", f.node.Name, map[string]string{"app": "db", rack: "from-template"}, nil)
	must(k8s.Create(context.Background(), p))

	eventually(t, "unschedulable echoed", func() bool { return hasLabel(getPod(t, f.ns, "templated"), uKey, "true") })
	consistently(t, "template value kept", func() bool { return hasLabel(getPod(t, f.ns, "templated"), rack, "from-template") })
	eventually(t, "KeySkipped event", func() bool { return hasEvent(f.ns, "templated", ReasonKeySkipped) })
}

func hasEvent(ns, pod, reason string) bool {
	evs := &corev1.EventList{}
	must(k8s.List(context.Background(), evs, client.InNamespace(ns)))
	for _, e := range evs.Items {
		if e.Reason == reason && e.InvolvedObject.Name == pod {
			return true
		}
	}
	return false
}

// The suite runs as on Kubernetes 1.35+, where zone and region belong to
// Kubernetes, unless forceKubernetesManaged is set for a cluster with
// PodTopologyLabelsAdmission off.
func TestKubernetesManagedZone(t *testing.T) {
	const zones = `- name: zones
  podSelector: {matchLabels: {app: db}}
  labels: {copy: [topology.kubernetes.io/zone]%s}
`
	setRules(t, fmt.Sprintf(zones, ""))
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{zone: "a"})
	if !copiesTopology {
		// Before 1.35, NodeEcho copies the zone itself.
		eventually(t, "zone copied by NodeEcho", func() bool {
			p := getPod(t, f.ns, "db")
			return hasLabel(p, zone, "a") && slices.Equal(managers(p), []string{"nodeecho/zones"})
		})
		return
	}
	consistently(t, "zone left to Kubernetes", func() bool {
		p := getPod(t, f.ns, "db")
		return hasLabel(p, zone, "a") && len(managers(p)) == 0
	})

	// A pod Kubernetes did not label, as with the feature off: the zone
	// reaches the node only after binding.
	g := newFixture(t, map[string]string{"app": "db"}, nil)
	g.updateNode(t, func(n *corev1.Node) { n.Labels = map[string]string{zone: "b"} })
	eventually(t, "KubernetesTopologyMissing event", func() bool {
		return hasEvent(g.ns, "db", ReasonKubernetesTopologyMissing)
	})
	if p := getPod(t, g.ns, "db"); p.Labels[zone] != "" || len(managers(p)) != 0 {
		t.Fatalf("zone written without forceKubernetesManaged: %v %v", p.Labels, managers(p))
	}

	setRules(t, fmt.Sprintf(zones, ", forceKubernetesManaged: true"))
	eventually(t, "zone copied when forced", func() bool { return hasLabel(getPod(t, g.ns, "db"), zone, "b") })
	g.updateNode(t, func(n *corev1.Node) { n.Labels[zone] = "c" })
	eventually(t, "forced zone follows the node", func() bool { return hasLabel(getPod(t, g.ns, "db"), zone, "c") })
}

func TestLeavingScopeAndRuleRemovalCleanUp(t *testing.T) {
	setRules(t, dbRule+`- name: zones
  podSelector: {matchLabels: {app: db}}
  labels: {copy: [rack.example.org/id], target: annotations}
`)
	f := newFixture(t, map[string]string{"app": "db"}, map[string]string{"rack.example.org/id": "r1"})
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "both rules written", func() bool {
		p := getPod(t, f.ns, "db")
		return hasLabel(p, uKey, "true") && p.Annotations["rack.example.org/id"] == "r1"
	})

	// Rule removal: the zones rule's annotation goes, db stays.
	setRules(t, dbRule)
	eventually(t, "removed rule cleaned", func() bool {
		p := getPod(t, f.ns, "db")
		_, rack := p.Annotations["rack.example.org/id"]
		return !rack && hasLabel(p, uKey, "true")
	})

	// Leaving scope: the pod no longer matches db.
	p := getPod(t, f.ns, "db")
	p.Labels["app"] = "other"
	must(k8s.Update(context.Background(), p))
	eventually(t, "out-of-scope pod cleaned", func() bool {
		p := getPod(t, f.ns, "db")
		_, u := p.Labels[uKey]
		return !u && len(managers(p)) == 0
	})
}

func TestInvalidConfigurationKeepsTheLastValidOne(t *testing.T) {
	setRules(t, dbRule)
	f := newFixture(t, map[string]string{"app": "db"}, nil)
	f.updateNode(t, func(n *corev1.Node) { n.Spec.Unschedulable = true })
	eventually(t, "echoed", func() bool { return hasLabel(getPod(t, f.ns, "db"), uKey, "true") })

	cm := &corev1.ConfigMap{}
	must(k8s.Get(context.Background(), types.NamespacedName{Namespace: configNS, Name: configName}, cm))
	cm.Data[ConfigKey] = "version: 2\nrules: []\n"
	must(k8s.Update(context.Background(), cm))
	consistently(t, "keys kept under an invalid configuration", func() bool {
		return hasLabel(getPod(t, f.ns, "db"), uKey, "true") && len(store.Rules()) == 1
	})

	// A typo in the running rule keeps its last valid version.
	cm.Data[ConfigKey] = "version: 1\nrules:\n- name: db\n  podSelector: {}\n  unschedulable: {}\n"
	must(k8s.Update(context.Background(), cm))
	consistently(t, "refused rule keeps running", func() bool {
		return hasLabel(getPod(t, f.ns, "db"), uKey, "true") && len(store.Rules()) == 1
	})
}
