---
sidebar_position: 1
title: Getting started
---

# Getting started

This tutorial runs NodeEcho on a throwaway [k3d](https://k3d.io) cluster,
writes one rule, and shows a cordon and a taint reaching a pod. It takes
about five minutes and needs Docker, k3d, kubectl and a clone of the
repository.

No image is published yet, so the tutorial builds one.

## 1. A cluster with its own kubeconfig

The cluster gets a kubeconfig of its own, so your default one is left alone:

```bash
k3d cluster create nodeecho-demo --agents 1 \
  --kubeconfig-update-default=false --kubeconfig-switch-context=false
export KUBECONFIG=$(k3d kubeconfig write nodeecho-demo)
kubectl get nodes
```

## 2. Build and load the image

```bash
make docker-build IMG=nodeecho:dev
k3d image import nodeecho:dev -c nodeecho-demo
```

## 3. Install NodeEcho

```bash
kubectl apply -k config/default
kubectl -n nodeecho-system set image deployment/nodeecho manager=nodeecho:dev
kubectl -n nodeecho-system rollout status deployment/nodeecho
```

NodeEcho runs in `nodeecho-system` with the rules ConfigMap `nodeecho`,
which starts with `rules: []`: it is ready, and writes nothing.

## 4. Write a rule

This rule selects the pods labelled `app=demo`, echoes the cordon and the
`example.org/maintenance` taint, and copies the Node's hostname label:

```bash
kubectl -n nodeecho-system apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: nodeecho
data:
  rules.yaml: |
    version: 1
    rules:
    - name: demo
      podSelector: {matchLabels: {app: demo}}
      unschedulable: {}
      taints: {copy: [example.org/maintenance]}
      labels: {copy: [kubernetes.io/hostname], target: annotations}
EOF
```

Start a pod for it:

```bash
kubectl run demo --image=registry.k8s.io/pause:3.10 --labels=app=demo
kubectl wait --for=condition=Ready pod/demo
kubectl get pod demo -o jsonpath='{.metadata.annotations}{"\n"}'
```

The pod carries `kubernetes.io/hostname` as an annotation, and nothing else
from NodeEcho: its node is healthy, so there is nothing to say.

## 5. Cordon and taint the node

```bash
NODE=$(kubectl get pod demo -o jsonpath='{.spec.nodeName}')
kubectl cordon "$NODE"
kubectl get pod demo --show-labels
```

The pod now has `nodeecho.fyannk.dev/unschedulable=true`. Add a listed
taint and an unlisted one:

```bash
kubectl taint node "$NODE" example.org/maintenance=soon:NoSchedule other=x:NoSchedule
kubectl get pod demo -o jsonpath='{.metadata.annotations.nodeecho\.fyannk\.dev/taints}{"\n"}'
```

```json
[{"key":"example.org/maintenance","value":"soon","effect":"NoSchedule"}]
```

Only the listed taint is echoed. Undo both, and the keys go away:

```bash
kubectl uncordon "$NODE"
kubectl taint node "$NODE" example.org/maintenance- other-
kubectl get pod demo --show-labels
```

## 6. See who wrote what

Each rule writes with its own field manager, `nodeecho/<rule name>`:

```bash
kubectl get pod demo --show-managed-fields -o yaml | grep -A3 'manager: nodeecho/'
```

## Clean up

```bash
k3d cluster delete nodeecho-demo
unset KUBECONFIG
```

## Next

- [Configuration](../operator/configuration.md) for selectors, prefixes and
  targets.
- [Admission policies](../operator/admission-policies.md) to protect the keys
  and to limit NodeEcho's own rights.
