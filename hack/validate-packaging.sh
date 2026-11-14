#!/usr/bin/env bash
# Copyright © contributors to the NodeEcho project.
# SPDX-License-Identifier: Apache-2.0
#
# Checks that the three install paths say the same thing: the kustomize
# manifests in config/, the Helm chart in deploy/helm, and the OLM bundle and
# catalog in deploy/olm. They are kept by hand, so the failure to catch is a
# copy that drifted: a version bumped in one place, a permission added to one
# path only, an alert edited without its chart copy.
#
# Needs helm and python3 with PyYAML. operator-sdk and opm, when installed,
# also validate the bundle and the catalog.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
CHART=deploy/helm/nodeecho

helm lint --quiet "$CHART" >/dev/null
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
kubectl kustomize config/default >"$WORK/kustomize.yaml"
# The chart with every option on, and with its defaults.
helm template nodeecho "$CHART" --namespace nodeecho-system >"$WORK/helm-default.yaml"
helm template nodeecho "$CHART" --namespace nodeecho-system --set fullnameOverride=nodeecho \
	--set webhook.enabled=true --set metrics.serviceMonitor.enabled=true --set metrics.prometheusRule.enabled=true \
	--set dashboard.enabled=true --set policies.reservedKeys.enabled=true --set policies.writerScope.enabled=true \
	--set 'metrics.discovery={prometheus,opentelemetry}' --set webhook.failurePolicy=Fail >"$WORK/helm-full.yaml"
helm template nodeecho "$CHART" --namespace nodeecho-system --set webhook.enabled=true \
	--set webhook.certificates.provider=cert-manager --set metrics.secure=false >"$WORK/helm-plain.yaml"
# Rules that write no nodeecho.fyannk.dev/ key, with the writer-scope policy.
cat >"$WORK/rules-values.yaml" <<'YAML'
rules:
  - name: zone
    podSelector: {matchLabels: {app: db}}
    labels: {copy: [topology.kubernetes.io/zone]}
policies:
  writerScope: {enabled: true}
YAML
helm template nodeecho "$CHART" --namespace nodeecho-system --set fullnameOverride=nodeecho \
	-f "$WORK/rules-values.yaml" >"$WORK/helm-rules.yaml"
kubectl kustomize config/policy >"$WORK/kustomize-policy.yaml"
# Each option, by kustomize and by the chart, to compare them object by object.
overlay() { # overlay <name> <component>...
	local dir=$WORK/overlay-$1 root
	mkdir -p "$dir"
	root=$(realpath --relative-to="$dir" "$ROOT")
	{
		echo "resources: [$root/config/default]"
		echo "components:"
		for c in "${@:2}"; do echo "  - $root/config/components/$c"; done
	} >"$dir/kustomization.yaml"
	kubectl kustomize "$dir" >"$WORK/kustomize-$1.yaml"
}
chart() { # chart <name> <helm arguments>...
	helm template nodeecho "$CHART" --namespace nodeecho-system --set fullnameOverride=nodeecho "${@:2}" >"$WORK/chart-$1.yaml"
}
overlay webhook eviction-webhook
chart webhook --set webhook.enabled=true
overlay http metrics-http
chart http --set metrics.secure=false
overlay cert-manager eviction-webhook cert-manager
chart cert-manager --set webhook.enabled=true --set webhook.certificates.provider=cert-manager
overlay http-cert-manager metrics-http eviction-webhook cert-manager
chart http-cert-manager --set metrics.secure=false --set webhook.enabled=true --set webhook.certificates.provider=cert-manager
chart policies --set policies.reservedKeys.enabled=true --set policies.writerScope.enabled=true

WORK=$WORK python3 - <<'PY'
import os, sys, pathlib, yaml

work = pathlib.Path(os.environ["WORK"])
problems = []

def load(path):
    return yaml.safe_load(pathlib.Path(path).read_text())

def docs(path):
    return [d for d in yaml.safe_load_all(pathlib.Path(path).read_text()) if d]

def find(items, kind, name=None):
    for d in items:
        if d["kind"] == kind and (name is None or d["metadata"]["name"] == name):
            return d
    problems.append(f"no {kind} {name or ''} found")
    return {}

chart = load("deploy/helm/nodeecho/Chart.yaml")
csv = load("deploy/olm/bundle/manifests/nodeecho.clusterserviceversion.yaml")
catalog = docs("deploy/olm/catalog/nodeecho/catalog.yaml")

# 1. One version, everywhere.
version = str(chart["appVersion"])
if str(chart["version"]) != version:
    problems.append(f"chart version {chart['version']} != appVersion {version}")
if str(csv["spec"]["version"]) != version:
    problems.append(f"CSV version {csv['spec']['version']} != chart appVersion {version}")
if csv["metadata"]["name"] != f"nodeecho.v{version}":
    problems.append(f"CSV name {csv['metadata']['name']} != nodeecho.v{version}")
image = f"ghcr.io/fyannk/nodeecho:{version}"
if csv["metadata"]["annotations"]["containerImage"] != image:
    problems.append(f"CSV containerImage != {image}")
deployment = csv["spec"]["install"]["spec"]["deployments"][0]["spec"]["template"]["spec"]
if deployment["containers"][0]["image"] != image:
    problems.append(f"CSV deployment image != {image}")
bundles = [d for d in catalog if d["schema"] == "olm.bundle"]
channels = [d for d in catalog if d["schema"] == "olm.channel"]
if [b["name"] for b in bundles] != [f"nodeecho.v{version}"]:
    problems.append(f"catalog bundles {[b['name'] for b in bundles]} != [nodeecho.v{version}]")
elif bundles[0]["image"] != f"ghcr.io/fyannk/nodeecho-bundle:{version}":
    problems.append("catalog bundle image does not carry the version")
elif str(bundles[0]["properties"][0]["value"]["version"]) != version:
    problems.append("catalog bundle version property differs")
if not any(e["name"] == f"nodeecho.v{version}" for c in channels for e in c["entries"]):
    problems.append("no catalog channel carries the bundle")

# 2. Copies of config/ files.
def strip(d):
    d = dict(d)
    d["metadata"] = {k: v for k, v in d["metadata"].items() if k != "namespace"}
    return d
for copy, source in [
    ("deploy/olm/bundle/manifests/nodeecho-metrics_v1_service.yaml", "config/manager/metrics_service.yaml"),
    ("deploy/olm/bundle/manifests/nodeecho-metrics-reader_rbac.authorization.k8s.io_v1_clusterrole.yaml",
     "config/rbac/metrics_reader_role.yaml"),
]:
    if strip(load(copy)) != strip(load(source)):
        problems.append(f"{copy} differs from {source}")
if load("deploy/helm/nodeecho/files/alerts.yaml") != load("config/prometheus/alerts.yaml")["spec"]:
    problems.append("deploy/helm/nodeecho/files/alerts.yaml differs from config/prometheus/alerts.yaml spec")
import json
if json.loads(pathlib.Path("deploy/helm/nodeecho/files/dashboard.json").read_text()) != \
        json.loads(pathlib.Path("config/grafana/nodeecho.json").read_text()):
    problems.append("deploy/helm/nodeecho/files/dashboard.json differs from config/grafana/nodeecho.json")

# 3. The same rights everywhere.
kust = docs(work / "kustomize.yaml")
cluster_rules = find(kust, "ClusterRole", "nodeecho").get("rules")
role_rules = find(kust, "Role", "nodeecho").get("rules")
csv_spec = csv["spec"]["install"]["spec"]
if csv_spec["clusterPermissions"][0]["rules"] != cluster_rules:
    problems.append("CSV clusterPermissions differ from config/rbac/cluster_role.yaml")
if csv_spec["permissions"][0]["rules"] != role_rules:
    problems.append("CSV permissions differ from config/rbac/role.yaml")
helm_default = docs(work / "helm-default.yaml")
if find(helm_default, "ClusterRole", "nodeecho").get("rules") != cluster_rules:
    problems.append("the chart's ClusterRole differs from config/rbac/cluster_role.yaml")
if find(helm_default, "Role", "nodeecho").get("rules") != role_rules:
    problems.append("the chart's Role differs from config/rbac/role.yaml")

# 4. The same Deployment: ports, environment, resources, security.
kdep = find(kust, "Deployment", "nodeecho")["spec"]["template"]["spec"]
kc, cc = kdep["containers"][0], deployment["containers"][0]
for field in ("env", "resources", "securityContext", "livenessProbe", "readinessProbe"):
    if kc.get(field) != cc.get(field):
        problems.append(f"CSV container {field} differs from config/manager/deployment.yaml")
for port in kc["ports"]:
    if port not in cc["ports"]:
        problems.append(f"CSV container lacks port {port}")
if kdep["securityContext"] != deployment["securityContext"]:
    problems.append("CSV pod securityContext differs from config/manager/deployment.yaml")

# 5. The rendered chart: the variants hold together.
full = docs(work / "helm-full.yaml")
plain = docs(work / "helm-plain.yaml")
vwc = find(full, "ValidatingWebhookConfiguration", "nodeecho-eviction")
if vwc and vwc["webhooks"][0]["clientConfig"]["service"]["name"] != "nodeecho-webhook":
    problems.append("the chart's webhook does not point at its Service")
# Ignore is not a setting: with Fail, NodeEcho being down would block every
# eviction in the cluster, its own pods' included.
if vwc and vwc["webhooks"][0]["failurePolicy"] != "Ignore":
    problems.append("the chart's webhook failurePolicy can be set to something else than Ignore")
if not find(plain, "Certificate"):
    problems.append("the cert-manager variant has no Certificate")
pdep = find(plain, "Deployment")["spec"]["template"]["spec"]["containers"][0]
if "--metrics-secure=false" not in pdep["args"] or pdep["ports"][0] != {"name": "http-metrics", "containerPort": 8080}:
    problems.append("plain metrics do not switch to http-metrics on 8080")

# 6. config/default is what an upgrade applies again: it must not carry the
# rules ConfigMap, or it would put `rules: []` back over the user's rules and
# NodeEcho would remove every key from every pod.
if any(d["kind"] == "ConfigMap" and d["metadata"]["name"] == "nodeecho" for d in kust):
    problems.append("config/default carries the rules ConfigMap: applying it again resets the rules")

# The same for the OLM bundle: OLM applies every object of the bundle again
# at each upgrade, which reset the rules to `rules: []`.
for path in sorted(pathlib.Path("deploy/olm/bundle/manifests").glob("*.yaml")):
    d = load(path)
    if d["kind"] == "ConfigMap" and d["metadata"]["name"] == "nodeecho":
        problems.append(f"{path} is the rules ConfigMap: an OLM upgrade would reset the rules")

# 7. The writer-scope policy always lets NodeEcho change its own prefix,
# whatever the rules write today: a rule removed from the values leaves keys
# on the pods, and NodeEcho has to be allowed to remove them.
def allowed(items, name):
    policy = find(items, "ValidatingAdmissionPolicy", name)
    for v in policy.get("spec", {}).get("variables", []):
        if v["name"] == "allowed":
            return v["expression"]
    problems.append(f"{name} has no allowed list")
    return ""

for label, items in (("kustomize", docs(work / "kustomize-policy.yaml")), ("chart", docs(work / "helm-rules.yaml"))):
    expression = allowed(items, "nodeecho-writer-scope")
    if "nodeecho.fyannk.dev/" not in expression:
        problems.append(f"the {label}'s writer-scope policy does not allow the nodeecho.fyannk.dev/ prefix: {expression}")
if "topology.kubernetes.io/zone" not in allowed(docs(work / "helm-rules.yaml"), "nodeecho-writer-scope"):
    problems.append("the chart's writer-scope policy does not allow the keys of its rules")

# 8. The chart and kustomize install the same thing, option by option: what
# the pod runs with, what NodeEcho may do, the webhook and the policies.
# Names of values, images and labels differ on purpose and are not compared.
def norm(text):
    return " ".join(str(text).split())

def container(items):
    return find(items, "Deployment", "nodeecho")["spec"]["template"]["spec"]["containers"][0]

def rights(items, kind):
    """Every (group, resource, name, verb) granted by the Roles or ClusterRoles."""
    out = set()
    for d in items:
        if d["kind"] != kind or d["metadata"]["name"].endswith("-metrics-reader"):
            continue
        for rule in d.get("rules", []):
            for group in rule.get("apiGroups", []):
                for resource in rule.get("resources", []):
                    for name in rule.get("resourceNames", ["*"]):
                        for verb in rule["verbs"]:
                            out.add((group, resource, name, verb))
    return out

for variant in ("webhook", "http", "cert-manager", "http-cert-manager"):
    k = docs(work / f"kustomize-{variant}.yaml")
    h = docs(work / f"chart-{variant}.yaml")
    kc, hc = container(k), container(h)
    for field in ("env", "resources", "securityContext", "livenessProbe", "readinessProbe", "ports", "volumeMounts"):
        if kc.get(field) != hc.get(field):
            problems.append(f"{variant}: the chart's container {field} differs from kustomize: {hc.get(field)} != {kc.get(field)}")
    # The chart spells out defaults kustomize leaves to the binary: every
    # argument kustomize passes must be among the chart's.
    # The cert-manager component turns the rotation off again with a second
    # flag, the last one winning; the chart passes neither.
    off = ("--cert-rotation", "--cert-rotation=false") if "--cert-rotation=false" in kc["args"] else ()
    for arg in kc["args"]:
        if arg not in hc["args"] and arg not in off:
            problems.append(f"{variant}: kustomize passes {arg}, the chart does not")
    for arg in ("--metrics-secure=false", "--cert-rotation", "--webhook-port=9443"):
        if (arg in hc["args"]) != (arg in kc["args"]) and f"{arg}=false" not in kc["args"]:
            problems.append(f"{variant}: {arg} is passed by only one of the chart and kustomize")
    kpod = find(k, "Deployment", "nodeecho")["spec"]["template"]["spec"]
    hpod = find(h, "Deployment", "nodeecho")["spec"]["template"]["spec"]
    for field in ("securityContext", "volumes", "serviceAccountName", "terminationGracePeriodSeconds"):
        if kpod.get(field) != hpod.get(field):
            problems.append(f"{variant}: the chart's pod {field} differs from kustomize")
    for kind in ("ClusterRole", "Role"):
        only_k, only_h = rights(k, kind) - rights(h, kind), rights(h, kind) - rights(k, kind)
        if only_k or only_h:
            problems.append(f"{variant}: {kind} rights differ: only kustomize {sorted(only_k)}, only the chart {sorted(only_h)}")
    for name in ("nodeecho-metrics", "nodeecho-webhook"):
        ks = [d for d in k if d["kind"] == "Service" and d["metadata"]["name"] == name]
        hs = [d for d in h if d["kind"] == "Service" and d["metadata"]["name"] == name]
        if bool(ks) != bool(hs) or (ks and ks[0]["spec"]["ports"] != hs[0]["spec"]["ports"]):
            problems.append(f"{variant}: Service {name} differs between the chart and kustomize")
    kw = [d for d in k if d["kind"] == "ValidatingWebhookConfiguration"]
    hw = [d for d in h if d["kind"] == "ValidatingWebhookConfiguration"]
    if bool(kw) != bool(hw):
        problems.append(f"{variant}: only one of the chart and kustomize has the webhook configuration")
    elif kw:
        a, b = dict(kw[0]["webhooks"][0]), dict(hw[0]["webhooks"][0])
        for w in (a, b):
            w["clientConfig"] = {"service": w["clientConfig"]["service"]}
            # An empty selector and none select the same namespaces.
            if not w.get("namespaceSelector"):
                w.pop("namespaceSelector", None)
        if a != b:
            problems.append(f"{variant}: the webhook differs between the chart and kustomize: {b} != {a}")

# The policies: same matches, same expressions. The allowed and reserved
# lists are compared as lists, the messages not at all: the chart's name
# its own values.
kpol, hpol = docs(work / "kustomize-policy.yaml"), docs(work / "chart-policies.yaml")
for name in ("nodeecho-reserved-keys", "nodeecho-writer-scope"):
    a = find(kpol, "ValidatingAdmissionPolicy", name).get("spec", {})
    b = find(hpol, "ValidatingAdmissionPolicy", name).get("spec", {})
    for field in ("failurePolicy", "matchConstraints", "matchConditions"):
        if a.get(field) != b.get(field):
            problems.append(f"{name}: {field} differs between the chart and config/policy")
    av = {v["name"]: norm(v["expression"]) for v in a.get("variables", [])}
    bv = {v["name"]: norm(v["expression"]) for v in b.get("variables", [])}
    for listed in ("allowed", "prefixes"):
        if listed in av and yaml.safe_load(av.pop(listed)) != yaml.safe_load(bv.pop(listed, "[]")):
            problems.append(f"{name}: the default {listed} list differs between the chart and config/policy")
    if av != bv:
        problems.append(f"{name}: a variable differs between the chart and config/policy")
    if [norm(v["expression"]) for v in a.get("validations", [])] != [norm(v["expression"]) for v in b.get("validations", [])]:
        problems.append(f"{name}: a validation differs between the chart and config/policy")
    ab = find(kpol, "ValidatingAdmissionPolicyBinding", name).get("spec")
    if ab != find(hpol, "ValidatingAdmissionPolicyBinding", name).get("spec"):
        problems.append(f"{name}: the binding differs between the chart and config/policy")

for p in problems:
    print("FAIL", p)
sys.exit(1 if problems else 0)
PY

if command -v operator-sdk >/dev/null; then
	operator-sdk bundle validate deploy/olm/bundle --select-optional suite=operatorframework >/dev/null
fi
if command -v opm >/dev/null; then
	opm validate deploy/olm/catalog
fi
echo "packaging consistent: chart, OLM bundle and catalog at $(sed -n 's/^appVersion: "\(.*\)"/\1/p' "$CHART/Chart.yaml")"
