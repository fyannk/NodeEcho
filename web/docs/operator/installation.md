---
sidebar_position: 1
title: Installation
---

# Installation

NodeEcho installs with kustomize, from `config/` in the repository. There is
no released image yet: build one with `make docker-build IMG=<your image>`,
push it to a registry your cluster pulls from, and point the Deployment at
it.

## Requirements

- Kubernetes 1.30 or later for the admission policies
  (`ValidatingAdmissionPolicy` is GA in 1.30). Tested on 1.34 and 1.36.
- Cluster-admin rights to install: NodeEcho needs a ClusterRole.

## Install

```bash
kubectl apply -k config/default
kubectl -n nodeecho-system set image deployment/nodeecho manager=<your image>
kubectl -n nodeecho-system rollout status deployment/nodeecho
```

Or, better, a kustomize overlay of your own that pins the image:

```yaml
resources:
  - github.com/fyannk/NodeEcho/config/default?ref=<commit>
  - github.com/fyannk/NodeEcho/config/policy?ref=<commit>
images:
  - name: nodeecho
    newName: registry.example.org/nodeecho
    newTag: <tag>
```

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

Then install the [admission policies](admission-policies.md) from
`config/policy`, after adding your rules' keys to them, and, with the
Prometheus Operator, the [monitoring](observability.md) from
`config/prometheus` and `config/grafana`.

## Namespace and names

The rules ConfigMap is read from NodeEcho's own namespace, given by the
`POD_NAMESPACE` environment variable. Both admission policies name the
ServiceAccount `system:serviceaccount:nodeecho-system:nodeecho`: installing
elsewhere means editing them too.

## High availability

Leader election is on. A second replica waits on the Lease and takes over
within seconds when the leader goes away; it reports ready while it waits,
so that rolling updates complete. Scale with:

```bash
kubectl -n nodeecho-system scale deployment/nodeecho --replicas=2
```

## Upgrade

Apply the new manifests and image. On start, NodeEcho reconciles every pod
and writes only the ones that differ; an upgrade with an unchanged contract
writes nothing.

## Uninstall

```bash
kubectl delete -k config/policy
kubectl delete -k config/default
```

Uninstalling leaves the keys on the pods, as a stopped NodeEcho does. To
remove them first, set `rules: []` and wait for NodeEcho to clean up, then
uninstall.
