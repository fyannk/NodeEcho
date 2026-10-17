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
	--set 'metrics.discovery={prometheus,opentelemetry}' >"$WORK/helm-full.yaml"
helm template nodeecho "$CHART" --namespace nodeecho-system --set webhook.enabled=true \
	--set webhook.certificates.provider=cert-manager --set metrics.secure=false >"$WORK/helm-plain.yaml"

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
    ("deploy/olm/bundle/manifests/nodeecho-rules_v1_configmap.yaml", "config/manager/rules.yaml"),
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
if not find(plain, "Certificate"):
    problems.append("the cert-manager variant has no Certificate")
pdep = find(plain, "Deployment")["spec"]["template"]["spec"]["containers"][0]
if "--metrics-secure=false" not in pdep["args"] or pdep["ports"][0] != {"name": "http-metrics", "containerPort": 8080}:
    problems.append("plain metrics do not switch to http-metrics on 8080")

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
