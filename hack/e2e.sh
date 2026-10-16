#!/usr/bin/env bash
# Copyright © contributors to the NodeEcho project.
# SPDX-License-Identifier: Apache-2.0
#
# End-to-end test of NodeEcho on throwaway k3d clusters, one per variant:
#
#   k8s-1.34  Kubernetes before 1.35: NodeEcho copies zone and region itself.
#   k8s-1.36  Kubernetes copies zone and region at binding; NodeEcho leaves them.
#   gate-off  1.36 with PodTopologyLabelsAdmission off: NodeEcho warns, and
#             copies them once the rule sets forceKubernetesManaged.
#
# Every cluster gets its own kubeconfig in a temporary directory; the default
# kubeconfig is never read nor written. Clusters are deleted at the end unless
# E2E_KEEP=1. NodeEcho and the test image are imported from the host's
# Docker; the proxy variables, when set, are passed to the nodes for the k3s
# system images.
#
# Environment:
#   E2E_VARIANTS   variants to run (default: "k8s-1.34 k8s-1.36 gate-off")
#   E2E_K3S_OLD    k3s image before 1.35 (default rancher/k3s:v1.34.12-k3s1)
#   E2E_K3S_NEW    k3s image from 1.35  (default rancher/k3s:v1.36.5-k3s1)
#   E2E_BULK       pods of the load scenario (default 120, 0 to skip)
#   E2E_KEEP       1 keeps the clusters and the work directory
#   E2E_SKIP_BUILD 1 reuses an existing nodeecho:e2e image
#   E2E_NETWORK    existing Docker network for the k3d nodes, for instance
#                  the one that reaches the proxy

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VARIANTS=${E2E_VARIANTS:-"k8s-1.34 k8s-1.36 gate-off"}
K3S_OLD=${E2E_K3S_OLD:-rancher/k3s:v1.34.12-k3s1}
K3S_NEW=${E2E_K3S_NEW:-rancher/k3s:v1.36.5-k3s1}
BULK=${E2E_BULK:-120}
IMG=nodeecho:e2e
PAUSE=registry.k8s.io/pause:3.10
SYS=nodeecho-system
NS=e2e
WORK=$(mktemp -d "${TMPDIR:-/tmp}/nodeecho-e2e.XXXXXX")
CLUSTERS=()
PASSED=0
CLUSTER=""
KCFG=""

for tool in docker k3d kubectl jq; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

cleanup() {
	local code=$?
	if [[ ${E2E_KEEP:-0} == 1 ]]; then
		echo "kept: clusters ${CLUSTERS[*]:-none}, work directory $WORK"
		for c in "${CLUSTERS[@]}"; do echo "  kubectl --kubeconfig $WORK/$c.kubeconfig ..."; done
	else
		for c in "${CLUSTERS[@]}"; do k3d cluster delete "$c" >/dev/null 2>&1 || true; done
		rm -rf "$WORK"
	fi
	exit "$code"
}
trap cleanup EXIT

# --- helpers ---------------------------------------------------------------

k() { kubectl --kubeconfig "$KCFG" "$@"; }

ok() { PASSED=$((PASSED + 1)); printf '  ok   %s\n' "$*"; }

fail() {
	printf '  FAIL %s\n' "$*" >&2
	if [[ -n $KCFG ]]; then
		echo "--- NodeEcho logs" >&2
		k logs -n "$SYS" -l app.kubernetes.io/name=nodeecho --tail=60 --prefix >&2 || true
		echo "--- events in $NS" >&2
		k get events -n "$NS" --sort-by=.lastTimestamp 2>/dev/null | tail -20 >&2 || true
	fi
	exit 1
}

# wait_for <description> <command...>: retries the command for up to $TIMEOUT
# seconds (default 60).
wait_for() {
	local desc=$1 deadline=$((SECONDS + ${TIMEOUT:-60}))
	shift
	until "$@" >/dev/null 2>&1; do
		((SECONDS < deadline)) || fail "timed out: $desc"
		sleep 1
	done
	ok "$desc"
}

# hold <seconds> <description> <command...>: the command keeps succeeding.
hold() {
	local until=$((SECONDS + $1)) desc=$2
	shift 2
	while ((SECONDS < until)); do
		"$@" >/dev/null 2>&1 || fail "did not hold: $desc"
		sleep 1
	done
	ok "$desc"
}

pod_json() { k get pod -n "$NS" "$1" --show-managed-fields -o json; }
node_of() { k get pod -n "$NS" "$1" -o jsonpath='{.spec.nodeName}'; }

# label_is <pod> <key> <value>; label_absent <pod> <key>; same for annotations.
label_is() { [[ $(pod_json "$1" | jq -r --arg k "$2" '.metadata.labels[$k] // "<absent>"') == "$3" ]]; }
label_absent() { pod_json "$1" | jq -e --arg k "$2" '(.metadata.labels // {}) | has($k) | not' >/dev/null; }
annotation_is() { [[ $(pod_json "$1" | jq -r --arg k "$2" '.metadata.annotations[$k] // "<absent>"') == "$3" ]]; }
annotation_absent() { pod_json "$1" | jq -e --arg k "$2" '(.metadata.annotations // {}) | has($k) | not' >/dev/null; }

# nodeecho_managers <pod>: the nodeecho/ field managers, comma separated.
nodeecho_managers() {
	pod_json "$1" | jq -r '[.metadata.managedFields[]? | select(.manager | startswith("nodeecho/")) | .manager] | join(",")'
}
no_nodeecho_manager() { [[ -z $(nodeecho_managers "$1") ]]; }

# owns_label <pod> <manager> <key>
owns_label() {
	pod_json "$1" | jq -e --arg m "$2" --arg k "f:$3" \
		'any(.metadata.managedFields[]?; .manager == $m and .operation == "Apply" and ((.fieldsV1."f:metadata"."f:labels" // {}) | has($k)))' >/dev/null
}

event_says() {
	k get events -n "$NS" --field-selector "involvedObject.name=$1,reason=$2" -o json |
		jq -e --arg m "$3" 'any(.items[]; .message | contains($m))' >/dev/null
}
has_event() { [[ -n $(k get events -n "$NS" --field-selector "involvedObject.name=$1,reason=$2" -o name) ]]; }

node_label() { k get node "$1" -o json | jq -r --arg k "$2" '.metadata.labels[$k] // "<absent>"'; }

set_rules() {
	k create configmap nodeecho -n "$SYS" --from-file=rules.yaml=/dev/stdin --dry-run=client -o yaml | k apply -f - >/dev/null
}

leader() { k get lease -n "$SYS" nodeecho.fyannk.dev -o jsonpath='{.spec.holderIdentity}' | cut -d_ -f1; }
leader_ready() {
	local l
	l=$(leader)
	[[ -n $l ]] && [[ $(k get pod -n "$SYS" "$l" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}') == True ]]
}
leader_not_ready() {
	local l
	l=$(leader)
	[[ -n $l ]] && [[ $(k get pod -n "$SYS" "$l" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}') == False ]]
}
# forward <port> <command...>: runs the command with $LOCAL set to a local
# port forwarded to <port> of the leader.
forward() {
	local port=$1 out=$WORK/forward.out pid i
	shift
	: >"$out"
	LOCAL=""
	k port-forward -n "$SYS" "pod/$(leader)" ":$port" >"$out" 2>&1 &
	pid=$!
	for i in $(seq 50); do
		LOCAL=$(sed -n 's/^Forwarding from 127.0.0.1:\([0-9]*\).*/\1/p' "$out" | head -1)
		[[ -n $LOCAL ]] && break
		sleep 0.1
	done
	"$@"
	local code=$?
	kill "$pid" 2>/dev/null
	wait "$pid" 2>/dev/null
	return "$code"
}
# http_code <scheme> <path> [curl args...]: the status of a request to $LOCAL.
http_code() { curl -sk -o /dev/null -w '%{http_code}' "$1://127.0.0.1:$LOCAL$2" "${@:3}"; }
http_body() { curl -skf "$1://127.0.0.1:$LOCAL$2" "${@:3}"; }
metrics() { forward 8443 http_body https /metrics -H "Authorization: Bearer $SCRAPER_TOKEN"; }
metric_is() { metrics | grep -qx "$1 $2"; }

policy_checked() { k get validatingadmissionpolicy "$1" -o json | jq -e '.status.typeChecking != null' >/dev/null; }

# as_nodeecho <method> <pod> [merge patch]: calls the API server with a token
# of NodeEcho's ServiceAccount, which has no get right, so kubectl, which
# reads before it writes, cannot be used. Prints the response, fails unless
# the call succeeded.
as_nodeecho() {
	local server ca token code
	server=$(k config view --minify --raw -o jsonpath='{.clusters[0].cluster.server}')
	ca=$WORK/$CLUSTER.ca
	k config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d >"$ca"
	token=$(k create token nodeecho -n "$SYS" --duration=10m)
	local args=(-sS --cacert "$ca" -H "Authorization: Bearer $token" -X "$1" -o "$WORK/response" -w '%{http_code}')
	[[ -n ${3:-} ]] && args+=(-H "Content-Type: application/merge-patch+json" --data "$3")
	code=$(curl "${args[@]}" "$server/api/v1/namespaces/$NS/pods/$2${4:-}")
	cat "$WORK/response"
	[[ $code == 2* ]]
}

# denied <expected message> <command...>
denied() {
	local want=$1 out
	shift
	if out=$("$@" 2>&1); then return 1; fi
	grep -q "$want" <<<"$out"
}

resource_versions() { k get pods -n "$NS" -l spread=db -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.resourceVersion} {end}'; }
rv_unchanged() { [[ $(resource_versions) == "$RV_BEFORE" ]]; }
not_owns_label() { ! owns_label "$@"; }
nodes_ready() { [[ $(k get nodes --no-headers | grep -cw Ready) == 3 ]]; }
no_manager_in_namespace() {
	[[ -z $(k get pods -n "$NS" --show-managed-fields -o json | jq -r '.items[].metadata.managedFields[]?.manager | select(startswith("nodeecho/"))') ]]
}
pod_running() { [[ $(k get pod -n "$NS" "$1" -o jsonpath='{.status.phase}') == Running ]]; }
lease_moved() {
	local l
	l=$(leader)
	[[ -n $l && $l != "$1" ]]
}
bulk_cordoned() { k get pods -n "$NS" -l app=bulk -o json | jq '[.items[] | select(.metadata.labels["e2e.example.org/cordoned"] == "true")] | length'; }
bulk_count_is() { [[ $(bulk_cordoned) == "$1" ]]; }

db_rules() {
	cat <<EOF
version: 1
rules:
- name: db
  podSelector: {matchLabels: {app: e2e-db}}
  unschedulable: {}
  taints: {copy: [e2e.example.org/drain]}
  labels:
    copy: [topology.kubernetes.io/zone, topology.kubernetes.io/region, rack.example.org/id]
    forceKubernetesManaged: ${1:-false}
  annotations: {copy: [rack.example.org/position]}
- name: zone-annotation
  podSelector: {matchLabels: {app: e2e-db}}
  labels: {copy: [topology.kubernetes.io/zone], target: annotations}
EOF
}

# --- cluster ---------------------------------------------------------------

create_cluster() {
	local image=$1
	shift
	CLUSTER=$1
	shift
	KCFG=$WORK/$CLUSTER.kubeconfig
	if k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
		echo "cluster $CLUSTER already exists; delete it with: k3d cluster delete $CLUSTER" >&2
		exit 1
	fi
	CLUSTERS+=("$CLUSTER")
	local extra=()
	[[ -n ${E2E_NETWORK:-} ]] && extra+=(--network "$E2E_NETWORK")
	# Behind a proxy, the nodes need it to pull the k3s system images.
	local https=${HTTPS_PROXY:-${https_proxy:-}} http=${HTTP_PROXY:-${http_proxy:-}}
	if [[ -n $https || -n $http ]]; then
		local no="${NO_PROXY:-${no_proxy:-}},10.42.0.0/16,10.43.0.0/16,.svc,.cluster.local,localhost,127.0.0.1"
		[[ -n $https ]] && extra+=(--env "HTTPS_PROXY=$https@server:*;agent:*")
		[[ -n $http ]] && extra+=(--env "HTTP_PROXY=$http@server:*;agent:*")
		extra+=(--env "NO_PROXY=${no#,}@server:*;agent:*")
	fi
	k3d cluster create "$CLUSTER" --image "$image" --agents 2 --no-lb --wait --timeout 300s \
		--kubeconfig-update-default=false --kubeconfig-switch-context=false \
		--k3s-arg "--disable=traefik,servicelb,metrics-server@server:*" "${extra[@]}" "$@" >/dev/null
	k3d kubeconfig get "$CLUSTER" >"$KCFG"
	chmod 600 "$KCFG"
	[[ $(k config current-context) == "k3d-$CLUSTER" ]] || fail "unexpected kubeconfig context"
	k3d image import -c "$CLUSTER" "$IMG" "$PAUSE" >/dev/null
	TIMEOUT=120 wait_for "3 nodes ready" nodes_ready

	local i=0
	for n in $(k get nodes -o name | sort); do
		k label "$n" --overwrite topology.kubernetes.io/zone="zone-$i" topology.kubernetes.io/region=region-1 \
			rack.example.org/id="rack-$i" >/dev/null
		k annotate "$n" --overwrite rack.example.org/position="row 3, slot $i" >/dev/null
		i=$((i + 1))
	done
}

deploy() {
	local overlay=$WORK/overlay
	mkdir -p "$overlay"
	cat >"$overlay/kustomization.yaml" <<EOF
resources:
  - $(realpath --relative-to="$overlay" "$ROOT")/config/default
  - $(realpath --relative-to="$overlay" "$ROOT")/config/policy
images:
  - name: nodeecho
    newTag: e2e
patches:
  - target: {kind: ValidatingAdmissionPolicy, name: nodeecho-writer-scope}
    patch: |-
      - op: replace
        path: /spec/variables/0/expression
        value: "['nodeecho.fyannk.dev/', 'topology.kubernetes.io/zone', 'topology.kubernetes.io/region', 'rack.example.org/', 'e2e.example.org/']"
  - target: {kind: Deployment, name: nodeecho}
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/args/-
        value: --pprof-bind-address=127.0.0.1:6060
EOF
	k apply -k "$overlay" >/dev/null
	local warnings
	for p in nodeecho-reserved-keys nodeecho-writer-scope; do
		TIMEOUT=30 wait_for "policy $p type-checked" policy_checked "$p"
		warnings=$(k get validatingadmissionpolicy "$p" -o json | jq -r '.status.typeChecking.expressionWarnings // [] | .[] | "\(.fieldRef): \(.warning)"')
		[[ -z $warnings ]] || fail "policy $p warnings: $warnings"
	done
	ok "policies compile without warnings"
	TIMEOUT=120 wait_for "NodeEcho rolled out" k rollout status -n "$SYS" deployment/nodeecho --timeout=5s

	# A scraper allowed to read the metrics, and one that is not.
	k create serviceaccount -n "$SYS" metrics-scraper >/dev/null
	k create serviceaccount -n "$SYS" nobody >/dev/null
	k create clusterrolebinding nodeecho-e2e-scraper --clusterrole=nodeecho-metrics-reader \
		--serviceaccount="$SYS:metrics-scraper" >/dev/null
	SCRAPER_TOKEN=$(k create token -n "$SYS" metrics-scraper --duration=2h)
	NOBODY_TOKEN=$(k create token -n "$SYS" nobody --duration=2h)
}

workload() {
	k create namespace "$NS" >/dev/null
	k apply -n "$NS" -f - >/dev/null <<EOF
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: db}
spec:
  replicas: 3
  serviceName: db
  selector: {matchLabels: {app: e2e-db}}
  template:
    metadata: {labels: {app: e2e-db, spread: db}}
    spec:
      terminationGracePeriodSeconds: 0
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - topologyKey: kubernetes.io/hostname
              labelSelector: {matchLabels: {spread: db}}
      containers: [{name: pause, image: $PAUSE}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: web}
spec:
  replicas: 2
  selector: {matchLabels: {app: e2e-web}}
  template:
    metadata: {labels: {app: e2e-web}}
    spec:
      terminationGracePeriodSeconds: 0
      containers: [{name: pause, image: $PAUSE}]
EOF
	TIMEOUT=180 wait_for "workload ready" k rollout status -n "$NS" statefulset/db --timeout=5s
	k rollout status -n "$NS" deployment/web --timeout=120s >/dev/null
}

# --- scenarios -------------------------------------------------------------

scenario_idle() {
	echo "- no rules: nothing written"
	hold 5 "no nodeecho field manager on any pod" no_manager_in_namespace
}

scenario_healthy() {
	local variant=$1
	echo "- rules loaded on healthy nodes"
	db_rules | set_rules
	for p in db-0 db-1 db-2; do
		local n
		n=$(node_of "$p")
		wait_for "$p: rack copied from $n" label_is "$p" rack.example.org/id "$(node_label "$n" rack.example.org/id)"
		wait_for "$p: position annotation copied" annotation_is "$p" rack.example.org/position \
			"$(k get node "$n" -o json | jq -r '.metadata.annotations["rack.example.org/position"]')"
		wait_for "$p: zone copied as an annotation" annotation_is "$p" topology.kubernetes.io/zone "$(node_label "$n" topology.kubernetes.io/zone)"
		label_absent "$p" nodeecho.fyannk.dev/unschedulable || fail "$p: unschedulable on a schedulable node"
		annotation_absent "$p" nodeecho.fyannk.dev/taints || fail "$p: taints annotation without a listed taint"
	done
	ok "no unschedulable label, no taints annotation on healthy nodes"
	for p in $(k get pods -n "$NS" -l app=e2e-web -o name); do
		no_nodeecho_manager "${p#pod/}" || fail "unselected $p written"
	done
	ok "unselected pods untouched"

	local zone
	zone=$(node_label "$(node_of db-0)" topology.kubernetes.io/zone)
	case $variant in
	k8s-1.34)
		wait_for "zone label copied by NodeEcho before 1.35" owns_label db-0 nodeecho/db topology.kubernetes.io/zone
		label_is db-0 topology.kubernetes.io/zone "$zone" || fail "zone value"
		;;
	k8s-1.36)
		label_is db-0 topology.kubernetes.io/zone "$zone" ||
			fail "Kubernetes did not copy the zone: is PodTopologyLabelsAdmission enabled in this k3s?"
		ok "zone label set by Kubernetes at binding"
		hold 5 "NodeEcho leaves the zone label to Kubernetes" not_owns_label db-0 nodeecho/db topology.kubernetes.io/zone
		;;
	gate-off)
		label_absent db-0 topology.kubernetes.io/zone || fail "zone label present although the feature is off"
		wait_for "KubernetesTopologyMissing Warning Event" has_event db-0 KubernetesTopologyMissing
		db_rules true | set_rules
		wait_for "forceKubernetesManaged copies the zone" label_is db-0 topology.kubernetes.io/zone "$zone"
		wait_for "and the region" label_is db-0 topology.kubernetes.io/region region-1
		;;
	esac
}

scenario_cordon_taint_label() {
	echo "- cordon, taints, node label changes"
	local n
	n=$(node_of db-0)
	k cordon "$n" >/dev/null
	wait_for "cordon echoed on db-0" label_is db-0 nodeecho.fyannk.dev/unschedulable true
	label_absent db-1 nodeecho.fyannk.dev/unschedulable || fail "cordon echoed on another node's pod"
	ok "pods on other nodes unaffected"
	k uncordon "$n" >/dev/null
	wait_for "uncordon removes the label" label_absent db-0 nodeecho.fyannk.dev/unschedulable

	k taint node "$n" e2e.example.org/drain=soon:NoSchedule other.example.org/ignored=x:NoSchedule >/dev/null
	wait_for "listed taint echoed, unlisted ignored" annotation_is db-0 nodeecho.fyannk.dev/taints \
		'[{"key":"e2e.example.org/drain","value":"soon","effect":"NoSchedule"}]'
	k taint node "$n" e2e.example.org/drain- other.example.org/ignored- >/dev/null
	wait_for "taints annotation removed" annotation_absent db-0 nodeecho.fyannk.dev/taints

	k label node "$n" --overwrite rack.example.org/id=rack-moved >/dev/null
	wait_for "label change follows the node" label_is db-0 rack.example.org/id rack-moved
	k label node "$n" rack.example.org/id- >/dev/null
	wait_for "label removed from the node is removed from the pod" label_absent db-0 rack.example.org/id
	k label node "$n" rack.example.org/id=rack-back >/dev/null
	wait_for "label back" label_is db-0 rack.example.org/id rack-back
}

scenario_policy() {
	echo "- admission policy reserving nodeecho.fyannk.dev/"
	local out
	if out=$(k label pod -n "$NS" db-0 nodeecho.fyannk.dev/unschedulable=true 2>&1); then fail "forging a reserved label is allowed"; fi
	grep -q 'written by NodeEcho only' <<<"$out" || fail "unexpected denial: $out"
	ok "forging a reserved label is denied, with the policy message"
	k label pod -n "$NS" db-0 e2e.example.org/free=yes >/dev/null || fail "an unrelated label is denied"
	ok "unrelated labels are allowed on a pod carrying NodeEcho keys"
	k cordon "$(node_of db-0)" >/dev/null
	wait_for "NodeEcho itself still writes" label_is db-0 nodeecho.fyannk.dev/unschedulable true
	if k label pod -n "$NS" db-0 nodeecho.fyannk.dev/unschedulable- >/dev/null 2>&1; then fail "removing a reserved label is allowed"; fi
	ok "removing a reserved label is denied"
	if k annotate pod -n "$NS" db-0 nodeecho.fyannk.dev/taints='[]' >/dev/null 2>&1; then fail "forging a reserved annotation is allowed"; fi
	ok "forging a reserved annotation is denied"
	if k run forged -n "$NS" --image="$PAUSE" --labels=nodeecho.fyannk.dev/unschedulable=true >/dev/null 2>&1; then
		fail "a pod created with a reserved label is allowed"
	fi
	ok "creating a pod with a reserved label is denied"
	k uncordon "$(node_of db-0)" >/dev/null
	wait_for "uncordon still removes it" label_absent db-0 nodeecho.fyannk.dev/unschedulable
}

scenario_writer_scope() {
	echo "- admission policy limiting NodeEcho's own writes"
	meta() { as_nodeecho PATCH db-0 "{\"metadata\":$1}"; }
	meta '{"labels":{"e2e.example.org/probe":"1"}}' >/dev/null || fail "an allowed key is denied"
	meta '{"labels":{"e2e.example.org/probe":null}}' >/dev/null || fail "removing an allowed key is denied"
	ok "allowed keys can be written and removed"
	denied "may not change app" meta '{"labels":{"app":"hijacked"}}' || fail "NodeEcho could change a selector label"
	ok "changing a label outside the allowed list is denied"
	denied "may not change" meta '{"annotations":{"note":"x"}}' || fail "NodeEcho could add an annotation"
	ok "adding an annotation outside the allowed list is denied"
	denied "spec" as_nodeecho PATCH db-0 '{"spec":{"activeDeadlineSeconds":36000}}' ||
		fail "NodeEcho could change a pod spec"
	ok "changing a pod spec is denied"
	denied "finalizers" meta '{"finalizers":["example.org/hold"]}' ||
		fail "NodeEcho could add a finalizer"
	ok "adding a finalizer is denied"
	denied "forbidden" as_nodeecho DELETE db-0 "" "?dryRun=All" || fail "NodeEcho could delete a pod"
	ok "deleting a pod is denied"

	(db_rules "$FORCE" && cat <<RULES
- name: outside
  podSelector: {matchLabels: {app: e2e-db}}
  labels: {copy: [rack.example.org/id], prefix: outside.}
RULES
	) | set_rules
	wait_for "a rule writing outside the allowed list gets a Warning Event" event_says db-0 KeySkipped "rule outside: write rejected"
	label_absent db-0 outside.rack.example.org/id || fail "the key outside the allowed list was written"
	db_rules "$FORCE" | set_rules
	wait_for "rule removed" metric_is nodeecho_rules_active 2
}

scenario_heavy_pod() {
	echo "- the policies on a large pod: 20 containers x 100 env vars, 1000 annotations"
	jq -n --arg image "$PAUSE" '{
		apiVersion: "v1", kind: "Pod",
		metadata: {name: "heavy", labels: {app: "e2e-db"},
			annotations: ([range(1000)] | map({key: "e2e.example.org/a\(.)", value: "value-\(.)"}) | from_entries)},
		spec: {terminationGracePeriodSeconds: 0,
			containers: [range(20)] | map({name: "c\(.)", image: $image,
				env: ([range(100)] | map({name: "V\(.)", value: "x"}))})}
	}' | k apply -n "$NS" -f - >/dev/null || fail "the policies reject a large pod"
	ok "a large pod is created"
	TIMEOUT=120 wait_for "the large pod runs" pod_running heavy
	wait_for "NodeEcho writes on the large pod" label_is heavy rack.example.org/id "$(node_label "$(node_of heavy)" rack.example.org/id)"
	k annotate pod -n "$NS" heavy e2e.example.org/a0=changed --overwrite >/dev/null || fail "updating a large pod is rejected"
	ok "updating a large pod passes the reserved-keys policy"
	k cordon "$(node_of heavy)" >/dev/null
	wait_for "NodeEcho's writes on the large pod pass the writer-scope policy" label_is heavy nodeecho.fyannk.dev/unschedulable true
	k uncordon "$(node_of heavy)" >/dev/null
	wait_for "and are removed" label_absent heavy nodeecho.fyannk.dev/unschedulable
	k delete pod -n "$NS" heavy --wait=false >/dev/null
	k rollout restart -n "$NS" deployment/web >/dev/null
	TIMEOUT=120 wait_for "a rolling update of unrelated pods completes under both policies" k rollout status -n "$NS" deployment/web --timeout=5s
}

scenario_foreign() {
	echo "- a value from the pod template is never overwritten"
	k run templated -n "$NS" --image="$PAUSE" --labels=app=e2e-db,rack.example.org/id=from-template \
		--overrides='{"spec":{"terminationGracePeriodSeconds":0}}' >/dev/null
	TIMEOUT=90 wait_for "templated pod running" pod_running templated
	wait_for "KeySkipped Warning Event" has_event templated KeySkipped
	hold 5 "template value kept" label_is templated rack.example.org/id from-template
	k delete pod -n "$NS" templated --wait=false >/dev/null
}

scenario_restart() {
	echo "- restart: no churn, rollout completes, the new leader works"
	local n
	n=$(node_of db-1)
	k cordon "$n" >/dev/null
	wait_for "cordon echoed on db-1" label_is db-1 nodeecho.fyannk.dev/unschedulable true
	RV_BEFORE=$(resource_versions)
	k rollout restart -n "$SYS" deployment/nodeecho >/dev/null
	TIMEOUT=120 wait_for "rolling restart completes" k rollout status -n "$SYS" deployment/nodeecho --timeout=5s
	TIMEOUT=60 wait_for "new leader ready" leader_ready
	hold 10 "no pod written after the restart" rv_unchanged
	k uncordon "$n" >/dev/null
	wait_for "the new leader removes the label" label_absent db-1 nodeecho.fyannk.dev/unschedulable
}

scenario_failover() {
	echo "- two replicas: standby ready, failover"
	k scale -n "$SYS" deployment/nodeecho --replicas=2 >/dev/null
	TIMEOUT=120 wait_for "both replicas ready" k rollout status -n "$SYS" deployment/nodeecho --timeout=5s
	local old
	old=$(leader)
	k delete pod -n "$SYS" "$old" --wait=false >/dev/null
	TIMEOUT=60 wait_for "another replica takes the lease" lease_moved "$old"
	local n
	n=$(node_of db-2)
	k cordon "$n" >/dev/null
	wait_for "echoed by the new leader" label_is db-2 nodeecho.fyannk.dev/unschedulable true
	k uncordon "$n" >/dev/null
	wait_for "and removed" label_absent db-2 nodeecho.fyannk.dev/unschedulable
	k scale -n "$SYS" deployment/nodeecho --replicas=1 >/dev/null
	TIMEOUT=120 wait_for "back to one replica" k rollout status -n "$SYS" deployment/nodeecho --timeout=5s
	TIMEOUT=60 wait_for "leader ready" leader_ready
}

scenario_metrics() {
	echo "- metrics"
	wait_for "nodeecho_config_valid 1" metric_is nodeecho_config_valid 1
	wait_for "nodeecho_rules_active 2" metric_is nodeecho_rules_active 2
	metrics | grep -q '^nodeecho_pod_writes_total{result="ok",rule="db"}' || fail "no pod write counted"
	ok "pod writes counted"
	metrics | grep -q '^nodeecho_keys_skipped_total{reason="owned-by-other",rule="db"}' || fail "no skipped key counted"
	ok "skipped keys counted"
	metrics | grep -q '^nodeecho_build_info{go_version="go[0-9.]*",version="e2e"} 1$' || fail "no build info"
	ok "build info published"
	metrics | grep -q '^nodeecho_config_loads_total{result="valid"}' || fail "no configuration load counted"
	ok "configuration loads counted"

	[[ $(forward 8443 http_code https /metrics) == 401 ]] || fail "metrics served without a token"
	ok "metrics refused without a token (401)"
	[[ $(forward 8443 http_code https /metrics -H "Authorization: Bearer $NOBODY_TOKEN") == 403 ]] ||
		fail "metrics served to an identity without the reader role"
	ok "metrics refused to an identity without the reader role (403)"
	# Plain HTTP to the HTTPS port: net/http logs a TLS handshake error
	# through the standard library's log package.
	forward 8443 http_code http /metrics >/dev/null || true
	wait_for "standard library log lines are structured too" stdlib_line_logged

	# Every metric the alerts and the dashboard query must exist.
	local exposed missing=()
	exposed=$(metrics | grep -v '^#' | sed 's/[{ ].*//' | sort -u)
	while read -r m; do
		[[ $m == up ]] && continue
		grep -qx "$m" <<<"$exposed" || missing+=("$m")
	done < <(grep -ohE '[a-z][a-z0-9_]*\{job=' "$ROOT/config/prometheus/alerts.yaml" "$ROOT/config/grafana/nodeecho.json" |
		sed 's/{job=//' | sort -u)
	((${#missing[@]} == 0)) || fail "metrics used by the alerts or the dashboard but not exposed: ${missing[*]}"
	ok "every metric of the alerts and the dashboard is exposed"
}

stdlib_line_logged() {
	k logs -n "$SYS" "$(leader)" | jq -R -e -s 'split("\n") | map(select(. != "") | fromjson?) |
		any(.[]; .logger == "stdlib" and (.line | contains("TLS handshake error")))' >/dev/null
}

scenario_pprof() {
	echo "- pprof"
	forward 6060 http_body http /debug/pprof/ | grep -q heap || fail "no pprof index"
	ok "pprof index served on the pod's loopback"
	[[ $(forward 6060 http_code http "/debug/pprof/heap?debug=1") == 200 ]] || fail "no heap profile"
	ok "heap profile served"
}

scenario_bad_config() {
	echo "- invalid configurations keep the last valid one"
	local n
	n=$(node_of db-0)
	k cordon "$n" >/dev/null
	wait_for "cordon echoed" label_is db-0 nodeecho.fyannk.dev/unschedulable true

	printf 'version: 2\nrules: []\n' | set_rules
	wait_for "nodeecho_config_valid 0" metric_is nodeecho_config_valid 0
	hold 5 "keys kept under an unknown version" label_is db-0 nodeecho.fyannk.dev/unschedulable true
	leader_ready || fail "leader not ready under an invalid configuration"
	ok "still ready: a valid configuration was loaded before"

	printf 'version: 1\n' | set_rules
	hold 5 "a missing rules field removes nothing" label_is db-0 nodeecho.fyannk.dev/unschedulable true

	db_rules "$FORCE" | sed 's/podSelector: {matchLabels: {app: e2e-db}}/podSelector: {}/' | set_rules
	wait_for "typo: config valid again" metric_is nodeecho_config_valid 1
	wait_for "typo: the rules are refused" metric_is nodeecho_rules_refused 2
	k uncordon "$n" >/dev/null
	wait_for "the refused rules keep running their last version" label_absent db-0 nodeecho.fyannk.dev/unschedulable
	db_rules "$FORCE" | set_rules
	wait_for "rules fixed" metric_is nodeecho_rules_refused 0
}

scenario_missing_config() {
	echo "- missing ConfigMap"
	local n
	n=$(node_of db-0)
	k cordon "$n" >/dev/null
	wait_for "cordon echoed" label_is db-0 nodeecho.fyannk.dev/unschedulable true
	k delete configmap -n "$SYS" nodeecho >/dev/null
	hold 5 "keys kept without a ConfigMap" label_is db-0 nodeecho.fyannk.dev/unschedulable true

	RV_BEFORE=$(resource_versions)
	k rollout restart -n "$SYS" deployment/nodeecho >/dev/null
	TIMEOUT=120 wait_for "rollout completes without a ConfigMap" k rollout status -n "$SYS" deployment/nodeecho --timeout=5s
	TIMEOUT=60 wait_for "the leader reports not ready" leader_not_ready
	k uncordon "$n" >/dev/null
	hold 8 "nothing written or removed before a configuration" rv_unchanged
	k logs -n "$SYS" "$(leader)" | grep -q 'Rules ConfigMap not found' || fail "no log line for the missing ConfigMap"
	ok "the missing ConfigMap is logged"

	db_rules "$FORCE" | set_rules
	TIMEOUT=60 wait_for "ready once the ConfigMap exists" leader_ready
	wait_for "then the uncordon is echoed" label_absent db-0 nodeecho.fyannk.dev/unschedulable
}

scenario_bulk() {
	((BULK > 0)) || return 0
	echo "- $BULK pods, every node cordoned at once"
	k create deployment bulk -n "$NS" --image="$PAUSE" --replicas="$BULK" >/dev/null
	k patch deployment bulk -n "$NS" --type=merge -p '{"spec":{"template":{"spec":{"terminationGracePeriodSeconds":0}}}}' >/dev/null
	TIMEOUT=300 wait_for "$BULK pods ready" k rollout status -n "$NS" deployment/bulk --timeout=5s
	(db_rules "$FORCE" && cat <<EOF
- name: bulk
  podSelector: {matchLabels: {app: bulk}}
  unschedulable: {key: e2e.example.org/cordoned}
EOF
	) | set_rules
	local start nodes
	nodes=$(k get nodes -o name)
	start=$(date +%s%N)
	# shellcheck disable=SC2086
	k cordon $nodes >/dev/null
	TIMEOUT=120 wait_for "all $BULK pods echoed" bulk_count_is "$BULK"
	echo "       took $((($(date +%s%N) - start) / 1000000))ms"
	start=$(date +%s%N)
	# shellcheck disable=SC2086
	k uncordon $nodes >/dev/null
	TIMEOUT=120 wait_for "all $BULK pods cleaned" bulk_count_is 0
	echo "       took $((($(date +%s%N) - start) / 1000000))ms"
	k delete deployment bulk -n "$NS" >/dev/null
}

scenario_stop_writing() {
	echo "- rules: [] removes every key"
	k cordon "$(node_of db-0)" >/dev/null
	wait_for "cordon echoed" label_is db-0 nodeecho.fyannk.dev/unschedulable true
	printf 'version: 1\nrules: []\n' | set_rules
	for p in db-0 db-1 db-2; do
		wait_for "$p: every nodeecho key and manager gone" no_nodeecho_manager "$p"
	done
	label_absent db-0 nodeecho.fyannk.dev/unschedulable || fail "unschedulable left behind"
	label_absent db-0 rack.example.org/id || fail "rack left behind"
	annotation_absent db-0 topology.kubernetes.io/zone || fail "zone annotation left behind"
	ok "no key left behind"
	k uncordon "$(node_of db-0)" >/dev/null
}

scenario_health() {
	echo "- NodeEcho health"
	local logs
	logs=$(k logs -n "$SYS" -l app.kubernetes.io/name=nodeecho --tail=-1)
	if grep -qiE 'panic|Observed a panic' <<<"$logs"; then fail "panic in the logs"; fi
	ok "no panic"
	local errors
	errors=$(grep -c 'Reconciler error' <<<"$logs" || true)
	((errors == 0)) || fail "$errors reconciler errors: $(grep 'Reconciler error' <<<"$logs" | head -3)"
	ok "no reconciler error"
	[[ $(k get pods -n "$SYS" -o jsonpath='{range .items[*]}{.status.containerStatuses[0].restartCount}{end}') =~ ^0+$ ]] ||
		fail "NodeEcho restarted"
	ok "no container restart"

	# Every line is a JSON object with an ISO 8601 time, a level and a
	# message, klog's included.
	local bad
	bad=$(jq -R -r 'try (fromjson | if (has("level") and has("msg") and (.ts | type == "string") and
		(.ts | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+"))) then empty else "missing fields: \(.)" end)
		catch "not JSON: \(.)"' <<<"$logs" | head -3)
	[[ -z $bad ]] || fail "log lines out of format: $bad"
	ok "every log line is structured JSON ($(wc -l <<<"$logs") lines)"
	jq -e -s 'any(.[]; .msg == "Starting manager" and .version == "e2e" and (.goVersion | startswith("go")))' \
		<<<"$logs" >/dev/null || fail "no startup line with the version"
	ok "the startup line carries the version"
}

run_variant() {
	local variant=$1
	echo "== $variant"
	case $variant in
	k8s-1.34) create_cluster "$K3S_OLD" "nodeecho-e2e-old" ;;
	k8s-1.36) create_cluster "$K3S_NEW" "nodeecho-e2e-new" ;;
	gate-off)
		create_cluster "$K3S_NEW" "nodeecho-e2e-gate-off" \
			--k3s-arg "--kube-apiserver-arg=feature-gates=PodTopologyLabelsAdmission=false@server:*"
		;;
	*) echo "unknown variant $variant" >&2 && exit 1 ;;
	esac
	echo "   $(k version -o json | jq -r .serverVersion.gitVersion), kubeconfig $KCFG"
	FORCE=false
	[[ $variant == gate-off ]] && FORCE=true

	deploy
	workload
	scenario_idle
	scenario_healthy "$variant"
	scenario_cordon_taint_label
	scenario_policy
	scenario_writer_scope
	scenario_heavy_pod
	scenario_foreign
	scenario_metrics
	scenario_pprof
	scenario_restart
	scenario_failover
	scenario_bad_config
	scenario_missing_config
	scenario_bulk
	scenario_stop_writing
	scenario_health

	if [[ ${E2E_KEEP:-0} != 1 ]]; then
		k3d cluster delete "$CLUSTER" >/dev/null
		CLUSTERS=("${CLUSTERS[@]/$CLUSTER/}")
	fi
	KCFG=""
}

# --- main ------------------------------------------------------------------

if [[ ${E2E_SKIP_BUILD:-0} != 1 ]]; then
	echo "building $IMG"
	# Proxy settings, when set, reach the build (go mod download).
	proxy_args=()
	for v in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
		[[ -n ${!v:-} ]] && proxy_args+=(--build-arg "$v")
	done
	docker build -q -t "$IMG" --build-arg VERSION=e2e "${proxy_args[@]}" "$ROOT" >/dev/null
fi
docker image inspect "$PAUSE" >/dev/null 2>&1 || docker pull -q "$PAUSE" >/dev/null

for v in $VARIANTS; do
	run_variant "$v"
done
echo "PASS: $PASSED checks across: $VARIANTS"
