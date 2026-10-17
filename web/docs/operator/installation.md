---
sidebar_position: 1
title: Installation
---

# Installation

Three install paths say the same thing, and `make validate-packaging` checks
they keep doing so: kustomize manifests, a Helm chart, and an OLM bundle and
catalog.

| Path | Best for | Options |
| --- | --- | --- |
| Helm, `deploy/helm/nodeecho` | most clusters | everything in values: rules, metrics, webhook, policies, monitoring |
| kustomize, `config/` | GitOps overlays | components for each option |
| OLM, `deploy/olm` | OpenShift and OLM clusters | webhook certificate from OLM |

Images are published for `linux/amd64` and `linux/arm64`:

- `ghcr.io/fyannk/nodeecho:<version>`;
- `ghcr.io/fyannk/nodeecho-bundle:<version>` and `ghcr.io/fyannk/nodeecho-catalog:<version>` for OLM;
- the chart at `oci://ghcr.io/fyannk/charts/nodeecho`.

No version is released yet: until then, build the image with
`make docker-build IMG=<your image>`.

### Verify what you install

Every image and the chart are signed keyless with [cosign](https://docs.sigstore.dev/)
by the GitHub Actions workflow that built them; the signature binds the
digest to the repository, the workflow file and the git ref, and is recorded
in the Rekor transparency log. The same workflows record a GitHub build
provenance attestation for each image and for the chart. The NodeEcho image
also carries an SBOM (SPDX) and a SLSA provenance attestation attached by
BuildKit.

```bash
# A released image, or the chart: built by the images or release workflow on a tag.
cosign verify ghcr.io/fyannk/nodeecho:<version> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/fyannk/NodeEcho/\.github/workflows/(images|release)\.yml@refs/tags/v'
cosign verify ghcr.io/fyannk/charts/nodeecho:<version> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/fyannk/NodeEcho/\.github/workflows/release\.yml@refs/tags/v'

# GitHub's own build provenance, for an image and for the chart archive of a release.
gh attestation verify oci://ghcr.io/fyannk/nodeecho:<version> --owner fyannk
gh attestation verify nodeecho-<version>.tgz --owner fyannk

# The SBOM and the provenance of the image.
docker buildx imagetools inspect ghcr.io/fyannk/nodeecho:<version> --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/fyannk/nodeecho:<version> --format '{{ json .Provenance }}'
```

The `latest` image is built from `main` on every push and signed the same
way, with `@refs/heads/main` as the identity suffix. An admission policy
such as Kyverno's `verifyImages` or Sigstore's policy-controller can require
these signatures before a NodeEcho pod starts.

## Requirements

- Kubernetes 1.30 or later; tested on 1.34 and 1.36.
- Cluster-admin rights to install: NodeEcho needs a ClusterRole.
- For the eviction webhook with `cert-manager` certificates: cert-manager.
- For the ServiceMonitor and alerts: the Prometheus Operator.

## With Helm

```bash
helm install nodeecho oci://ghcr.io/fyannk/charts/nodeecho \
  --namespace nodeecho-system --create-namespace -f values.yaml
```

A typical `values.yaml`:

```yaml
rules:
  - name: cnpg-instances
    podSelector: {matchLabels: {cnpg.io/podRole: instance}}
    unschedulable: {}
    taints: {copy: [node.kubernetes.io/unschedulable, karpenter.sh/disrupted]}
    eviction: {}
webhook:
  enabled: true                 # the eviction webhook, certificate by NodeEcho
policies:
  reservedKeys: {enabled: true}
  writerScope: {enabled: true}  # allowed keys computed from the rules
metrics:
  serviceMonitor: {enabled: true}
  prometheusRule: {enabled: true}
  readers: [{name: prometheus-k8s, namespace: monitoring}]
dashboard: {enabled: true}
```

Every value is described in the chart's
[`values.yaml`](https://github.com/fyannk/NodeEcho/blob/main/deploy/helm/nodeecho/values.yaml).
The chart names its resources after the release; the admission policies name
the release's own namespace and ServiceAccount.

Rules set in values are written to the ConfigMap on each upgrade; to manage
the rules elsewhere, leave `rules` empty and edit the ConfigMap, NodeEcho
watches it either way.

## With kustomize

```yaml
resources:
  - github.com/fyannk/NodeEcho/config/default?ref=<commit>
  - github.com/fyannk/NodeEcho/config/policy?ref=<commit>
components:
  - github.com/fyannk/NodeEcho/config/components/eviction-webhook?ref=<commit>
images:
  - name: nodeecho
    newName: ghcr.io/fyannk/nodeecho
    newTag: <version>
```

| Directory | Adds |
| --- | --- |
| `config/default` | NodeEcho in `nodeecho-system`, with its rules ConfigMap (`rules: []`) |
| `config/policy` | the two [admission policies](admission-policies.md) |
| `config/components/eviction-webhook` | the [eviction webhook](eviction-webhook.md), certificate by NodeEcho |
| `config/components/cert-manager` | after the previous one: certificate by cert-manager |
| `config/components/metrics-http` | plain HTTP metrics on 8080, see [Observability](observability.md) |
| `config/prometheus`, `config/grafana` | ServiceMonitor, alerts, dashboard |

`config/default` installs:

| Object | Purpose |
| --- | --- |
| Namespace `nodeecho-system` | NodeEcho's namespace |
| ServiceAccount `nodeecho` | its identity |
| ClusterRole and binding | `list`, `watch` on Nodes and Namespaces; `list`, `watch`, `patch` on Pods; `create`, `patch` on Events; `create` on TokenReviews and SubjectAccessReviews, for the metrics endpoint |
| Role and binding | read ConfigMaps and manage the leader election Lease, in its own namespace only |
| Deployment `nodeecho` | one replica, leader election on, non-root, read-only root file system |
| Service `nodeecho-metrics` | the HTTPS metrics endpoint, port 8443 |
| ClusterRole `nodeecho-metrics-reader` | `GET /metrics`, to bind to your scraper |
| ConfigMap `nodeecho` | the rules, `rules: []` to start with |

The admission policies name the ServiceAccount
`system:serviceaccount:nodeecho-system:nodeecho`: installing elsewhere means
editing them too. The Helm chart does it for you.

## With OLM

Add the catalog, then subscribe; NodeEcho installs in all-namespaces mode:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata: {name: nodeecho, namespace: olm}          # openshift-marketplace on OpenShift
spec:
  sourceType: grpc
  image: ghcr.io/fyannk/nodeecho-catalog:<version>
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata: {name: nodeecho, namespace: operators}    # openshift-operators on OpenShift
spec:
  channel: alpha
  name: nodeecho
  source: nodeecho
  sourceNamespace: olm
```

The bundle installs NodeEcho, its rules ConfigMap `nodeecho` (empty), the
metrics Service and reader role, and the eviction webhook, whose certificate
OLM issues. Edit the ConfigMap to add rules. The admission policies and the
monitoring are not part of the bundle: apply `config/policy` and
`config/prometheus` beside it, adapting the ServiceAccount's namespace in the
policies.

## High availability

Leader election is on. A second replica waits on the Lease and takes over
within seconds when the leader goes away; it reports ready while it waits,
so that rolling updates complete, and it answers the eviction webhook too.

## Upgrade

Apply the new manifests and image. On start, NodeEcho reconciles every pod
and writes only the ones that differ; an upgrade with an unchanged contract
writes nothing.

## Uninstall

Uninstalling leaves the keys on the pods, as a stopped NodeEcho does. To
remove them first, set `rules: []` and wait for NodeEcho to clean up, then
uninstall. With the `self` certificate provider, the Helm chart keeps the
webhook's Secret (`helm.sh/resource-policy: keep`); delete it by hand.
