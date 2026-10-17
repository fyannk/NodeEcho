---
sidebar_position: 3
title: Eviction webhook
---

# Eviction webhook

A cordon or a taint says a node is going away; an eviction request says a
pod is. The optional eviction webhook catches every request of the eviction
API, from `kubectl drain`, a cluster autoscaler, Karpenter or any other
client, and for the pods a rule with an `eviction` block selects:

- records when NodeEcho received the first request, in the pod annotation
  `nodeecho.fyannk.dev/eviction-requested`, for instance
  `2026-10-16T09:30:00Z`;
- answers it as the rule says: `allow` by default, or `deny`, with HTTP 429
  and a message, as a PodDisruptionBudget does.

The [contract](../reference/contract.md#eviction-requests) has the exact
rules.

```yaml
- name: cnpg-instances
  podSelector: {matchLabels: {cnpg.io/podRole: instance}}
  unschedulable: {}
  eviction: {}                    # record, and let the eviction through
- name: hold-primaries
  podSelector: {matchLabels: {cnpg.io/instanceRole: primary}}
  eviction:
    key: example.org/eviction-refused-at
    response: deny
    message: "switch over before evicting the primary"
```

A refusal is a 429, so `kubectl drain` and the autoscalers retry it every few
seconds, as for a PodDisruptionBudget, until the rule or the pod changes.
Refusing is a strong tool: a pod nobody can evict blocks node maintenance.
Keep `deny` for pods whose consumer moves them away on its own once it sees
the mark.

## Safety

- `failurePolicy: Ignore`: when NodeEcho is down, or not reachable, every
  eviction goes through.
- An instance that has not loaded its rules yet lets every eviction through.
- A dry-run eviction (`?dryRun=All`) is answered but recorded nowhere
  (`sideEffects: NoneOnDryRun`).
- The webhook only receives `pods/eviction`; it never sees pod deletions,
  which do not go through the eviction API.

## Enable it

Every instance serves the webhook, the standby ones included, once it has a
certificate. Each needs one, from one of three providers.

### With kustomize

```yaml
resources:
  - github.com/fyannk/NodeEcho/config/default?ref=<commit>
components:
  - github.com/fyannk/NodeEcho/config/components/eviction-webhook?ref=<commit>
  # Uncomment to have cert-manager issue the certificate instead:
  # - github.com/fyannk/NodeEcho/config/components/cert-manager?ref=<commit>
```

### With Helm

```yaml
webhook:
  enabled: true
  certificates:
    provider: self            # or cert-manager
```

### With OLM

The bundle declares the webhook; OLM issues its certificate. Nothing to do.

## Certificates

| Provider | Who issues and rotates | Where |
| --- | --- | --- |
| `self` (kustomize, Helm default) | NodeEcho's leader: a CA and a server certificate in the Secret `nodeecho-webhook-cert`, rotated before expiry, CA injected into the webhook configuration | [cert-controller](https://github.com/open-policy-agent/cert-controller) |
| `cert-manager` | cert-manager: a self-signed Issuer and a Certificate into the same Secret, CA injected by its CA injector | needs cert-manager |
| OLM | OLM, for the bundle's webhook | automatic |

Each instance reads the certificate from `/tmp/k8s-webhook-server/serving-certs`,
mounted from the Secret, or by OLM. NodeEcho starts serving the webhook once
the files hold a key pair, and reloads them when they are rotated; an
instance with the webhook enabled is ready only once it serves it.

A fresh install with `self` takes up to a minute to become ready: the leader
writes the certificate into the Secret, then the kubelet updates the mounted
files.

## Check it

```bash
kubectl get validatingwebhookconfiguration nodeecho-eviction \
  -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | head -c 20; echo
kubectl get pod <pod> -o jsonpath='{.metadata.annotations.nodeecho\.fyannk\.dev/eviction-requested}'
kubectl get events --field-selector involvedObject.name=<pod>,reason=EvictionRequested
```

The metric `nodeecho_evictions_total{rule, response}` counts the requests per
rule, and the log line `Eviction request received` names the pod, the rule
and the response.
