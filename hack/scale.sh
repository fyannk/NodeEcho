#!/usr/bin/env bash
# Copyright © contributors to the NodeEcho project.
# SPDX-License-Identifier: Apache-2.0
#
# Measures NodeEcho's memory, API requests and latency with many pods, on a
# throwaway single-node k3d cluster. The pods are bound to fake Nodes, Node
# objects without a kubelet: they never run, but NodeEcho, which only reads
# and writes API objects, sees them as on a real cluster.
#
# Like hack/e2e.sh, it uses its own kubeconfig and never touches the default
# one, and deletes the cluster at the end unless SCALE_KEEP=1.
#
# Environment:
#   SCALE_PODS      pods (default 5000), half of them selected by the rule
#   SCALE_NODES     fake Nodes (default 50)
#   SCALE_K3S       k3s image (default rancher/k3s:v1.36.5-k3s1)
#   SCALE_KEEP      1 keeps the cluster
#   E2E_NETWORK     existing Docker network for the k3d node
#   E2E_SKIP_BUILD  1 reuses an existing nodeecho:e2e image

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PODS=${SCALE_PODS:-5000}
NODES=${SCALE_NODES:-50}
K3S=${SCALE_K3S:-rancher/k3s:v1.36.5-k3s1}
IMG=nodeecho:e2e
SYS=nodeecho-system
NS=scale
CLUSTER=nodeecho-scale
WORK=$(mktemp -d "${TMPDIR:-/tmp}/nodeecho-scale.XXXXXX")
KCFG=$WORK/kubeconfig

for tool in docker k3d kubectl jq; do
	command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

cleanup() {
	local code=$?
	if [[ ${SCALE_KEEP:-0} == 1 ]]; then
		echo "kept: kubectl --kubeconfig $KCFG ..."
	else
		k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
		rm -rf "$WORK"
	fi
	exit "$code"
}

k() { kubectl --kubeconfig "$KCFG" "$@"; }
now_ms() { echo $(($(date +%s%N) / 1000000)); }

# count_labelled <selector>; count_annotated <selector> <key>
count_labelled() { k get pods -n "$NS" -l "$1" --no-headers 2>/dev/null | wc -l; }
count_annotated() {
	k get pods -n "$NS" -l "$1" -o json | jq --arg k "$2" '[.items[] | select(.metadata.annotations[$k])] | length'
}

# wait_count <description> <want> <count command...>: waits until the count
# reaches want, and prints how long it took.
wait_count() {
	local desc=$1 want=$2 start got deadline=$((SECONDS + 600))
	shift 2
	start=$(now_ms)
	while :; do
		got=$("$@")
		((got == want)) && break
		((SECONDS < deadline)) || { echo "timed out: $desc ($got/$want)" >&2; exit 1; }
		sleep 0.5
	done
	printf '  %-46s %6d ms\n' "$desc" $(($(now_ms) - start))
}

leader() { k get lease -n "$SYS" nodeecho.fyannk.dev -o jsonpath='{.spec.holderIdentity}' | cut -d_ -f1; }
# Metrics are served over plain HTTP here (see the overlay), so that the API
# server proxy can read them.
metrics() { k get --raw "/api/v1/namespaces/$SYS/pods/$(leader):8443/proxy/metrics"; }

# snapshot <label>: NodeEcho's memory and its API requests so far.
snapshot() {
	local m
	m=$(metrics)
	printf '  %-22s RSS %4d MiB  heap in use %4d MiB  requests:' "$1" \
		$(($(awk '/^process_resident_memory_bytes /{printf "%d", $2}' <<<"$m") / 1048576)) \
		$(($(awk '/^go_memstats_heap_inuse_bytes /{printf "%d", $2}' <<<"$m") / 1048576))
	awk '/^rest_client_requests_total/ && match($0, /method="[A-Z]+"/) {
		n[substr($0, RSTART + 8, RLENGTH - 9)] += $NF } END { for (m in n) printf " %s=%d", m, n[m]; print "" }' <<<"$m"
	printf '  %-22s pod writes %s\n' "" \
		"$(awk '/^nodeecho_pod_writes_total/ {s += $NF} END {print s + 0}' <<<"$m")"
}

set_rules() {
	k create configmap nodeecho -n "$SYS" --from-file=rules.yaml=/dev/stdin --dry-run=client -o yaml | k apply -f - >/dev/null
}

rules() {
	cat <<EOF
version: 1
rules:
- name: db
  podSelector: {matchLabels: {app: scale-db}}
  unschedulable: {}
  taints: {copy: [scale.example.org/drain]}
  labels: {copy: [rack.example.org/id]}
$1
EOF
}

if k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
	echo "cluster $CLUSTER already exists; delete it with: k3d cluster delete $CLUSTER" >&2
	exit 1
fi
trap cleanup EXIT

if [[ ${E2E_SKIP_BUILD:-0} != 1 ]]; then
	proxy_args=()
	for v in HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy; do
		[[ -n ${!v:-} ]] && proxy_args+=(--build-arg "$v")
	done
	docker build -q -t "$IMG" --build-arg VERSION=scale "${proxy_args[@]}" "$ROOT" >/dev/null
fi

extra=()
[[ -n ${E2E_NETWORK:-} ]] && extra+=(--network "$E2E_NETWORK")
https=${HTTPS_PROXY:-${https_proxy:-}}
if [[ -n $https ]]; then
	extra+=(--env "HTTPS_PROXY=$https@server:*" --env "HTTP_PROXY=${HTTP_PROXY:-${http_proxy:-$https}}@server:*"
		--env "NO_PROXY=${NO_PROXY:-${no_proxy:-}},10.42.0.0/16,10.43.0.0/16,.svc,.cluster.local@server:*")
fi
k3d cluster create "$CLUSTER" --image "$K3S" --no-lb --wait --timeout 300s \
	--kubeconfig-update-default=false --kubeconfig-switch-context=false \
	--k3s-arg "--disable=traefik,servicelb,metrics-server,local-storage@server:*" "${extra[@]}" >/dev/null
k3d kubeconfig get "$CLUSTER" >"$KCFG"
chmod 600 "$KCFG"
[[ $(k config current-context) == "k3d-$CLUSTER" ]] || { echo "unexpected kubeconfig context" >&2; exit 1; }
k3d image import -c "$CLUSTER" "$IMG" >/dev/null
echo "$(k version -o json | jq -r .serverVersion.gitVersion): $PODS pods on $NODES fake Nodes, half selected"

overlay=$WORK/overlay
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
        value: "['nodeecho.fyannk.dev/', 'rack.example.org/']"
  - target: {kind: Deployment, name: nodeecho}
    patch: |-
      - op: add
        path: /spec/template/spec/containers/0/args/-
        value: --metrics-secure=false
EOF
k apply -k "$overlay" >/dev/null
k rollout status -n "$SYS" deployment/nodeecho --timeout=180s >/dev/null
rules "" | set_rules
k create namespace "$NS" >/dev/null
sleep 5
snapshot "idle, no pod"

# Fake Nodes, then pods bound to them. The pods tolerate the taints the node
# lifecycle controller puts on Nodes without a kubelet.
for i in $(seq 0 $((NODES - 1))); do
	printf -- '---\napiVersion: v1\nkind: Node\nmetadata:\n  name: fake-%d\n  labels: {rack.example.org/id: rack-%d, type: fake}\n' "$i" $((i % 10))
done | k apply -f - >/dev/null
gen_pods() {
	local from=$1 to=$2 i app
	for ((i = from; i < to; i++)); do
		app=scale-db
		((i % 2)) && app=scale-other
		printf -- '---\napiVersion: v1\nkind: Pod\nmetadata: {name: p%d, labels: {app: %s}}\nspec:\n  nodeName: fake-%d\n  tolerations: [{operator: Exists}]\n  containers: [{name: c, image: example.org/never:pulled}]\n' \
			"$i" "$app" $((i % NODES))
	done
}
start=$(now_ms)
chunk=$(((PODS + 7) / 8))
for w in $(seq 0 7); do
	from=$((w * chunk)) to=$(((w + 1) * chunk))
	((to > PODS)) && to=$PODS
	((from < to)) && gen_pods "$from" "$to" | k create -n "$NS" -f - >/dev/null &
done
wait
printf '  %-46s %6d ms\n' "created $PODS pods" $(($(now_ms) - start))
selected=$((PODS / 2))
wait_count "initial echo of the rack label on $selected pods" "$selected" count_labelled "app=scale-db,rack.example.org/id"
sleep 10
snapshot "steady, all echoed"

nodes=$(k get nodes -l type=fake -o name)
# shellcheck disable=SC2086
k cordon $nodes >/dev/null
wait_count "cordon of every node echoed" "$selected" count_labelled "app=scale-db,nodeecho.fyannk.dev/unschedulable=true"
# shellcheck disable=SC2086
k uncordon $nodes >/dev/null
wait_count "uncordon echoed" 0 count_labelled "app=scale-db,nodeecho.fyannk.dev/unschedulable=true"
# shellcheck disable=SC2086
k taint nodes $nodes scale.example.org/drain=now:NoSchedule >/dev/null
wait_count "taint echoed" "$selected" count_annotated app=scale-db nodeecho.fyannk.dev/taints
sleep 2
snapshot "after cordon and taint"

rules "- name: other
  podSelector: {matchLabels: {app: scale-other}}
  labels: {copy: [rack.example.org/id]}" | set_rules
wait_count "rule added, resync of every pod" $((PODS - selected)) count_labelled "app=scale-other,rack.example.org/id"
snapshot "after a rule change"

k rollout restart -n "$SYS" deployment/nodeecho >/dev/null
start=$(now_ms)
k rollout status -n "$SYS" deployment/nodeecho --timeout=300s >/dev/null
until [[ -n $(leader) ]] && k get --raw "/api/v1/namespaces/$SYS/pods/$(leader):8081/proxy/readyz" >/dev/null 2>&1; do sleep 0.5; done
printf '  %-46s %6d ms\n' "restart until the new leader is ready" $(($(now_ms) - start))
sleep 20
snapshot "after restart, settled"
