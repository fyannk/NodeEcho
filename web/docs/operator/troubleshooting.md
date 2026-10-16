---
sidebar_position: 5
title: Troubleshooting
---

# Troubleshooting

## A key is missing on a pod

Check, in order:

1. **Is the configuration loaded?** `nodeecho_config_valid` is `1`, and the
   leader is ready. Until a first valid configuration is loaded, NodeEcho
   writes nothing.
2. **Is the rule active?** `nodeecho_rules_refused` is `0`; otherwise the log
   says why: `kubectl -n nodeecho-system logs deploy/nodeecho | grep refused`.
3. **Is the pod in scope?** Its namespace, its labels and its Node must match
   the rule's three selectors, and it must be bound to a Node.
4. **Is there anything to say?** A schedulable Node has no unschedulable key;
   a Node without a listed taint has no taints annotation; a Node without a
   listed label has no copy of it.
5. **Was it skipped?** Look for Events on the pod:

   ```bash
   kubectl get events --field-selector involvedObject.name=<pod>,reason=KeySkipped
   ```

## Events

| Reason | Meaning | What to do |
| --- | --- | --- |
| `KeySkipped` … `key set by another writer` | the key already holds another value, written by someone else, for instance from the pod template | remove it from the template, or use another key or a prefix |
| `KeySkipped` … `invalid label value` | the Node value is not a valid label value | copy it as an annotation |
| `KeySkipped` … `write rejected` | the API server refused the write: an admission policy, the annotations size limit, or no `patch` right | read the message; for the writer-scope policy, add the key to its allowed list |
| `KubernetesTopologyMissing` | Kubernetes 1.35+ should have copied zone or region, and did not | if `PodTopologyLabelsAdmission` is off, set `labels.forceKubernetesManaged: true` |

## The configuration does not apply

`nodeecho_config_valid` is `0`: the document has an unknown `version`, no
`rules` field, an unknown field or a wrong type, or the ConfigMap or its
`rules.yaml` key is missing. NodeEcho keeps the last valid configuration and
logs the error.

## NodeEcho is not ready

Only the elected instance can be not ready, and only before it has loaded a
first valid configuration. Check that the ConfigMap `nodeecho` exists in
NodeEcho's namespace with a `rules.yaml` key; the log says
`rules ConfigMap not found` otherwise.

## A key comes back after I removed it

That is the echo: while the Node still has the fact, NodeEcho writes it
again. Change the Node, or the rule.

## Inspect ownership

```bash
kubectl get pod <pod> --show-managed-fields -o json \
  | jq '.metadata.managedFields[] | select(.manager | startswith("nodeecho/"))'
```

Each rule owns exactly the keys it wrote, under `nodeecho/<rule name>`.
