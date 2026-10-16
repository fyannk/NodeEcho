# Node usage by Kubernetes operators

Research checked: **2026-10-12**, from shallow clones of each default branch.
Every link pins the commit that was read. "NodeEcho" below means its two planned
outputs: node labels and annotations copied onto pods, and `spec.unschedulable`
plus listed taints echoed onto pods.

This complements the [ecosystem research](ecosystem.md), which covers
Kubernetes-native facilities and the closest prior art in more depth.

## Summary

One row per operator. Columns:

- **Needs Nodes?**: whether the operator, or Pods it creates, reads the Node API.
- **Optional?**: whether it can run without Node rights, and what is lost.
- **Cordon/taints, Labels, Annotations**: which Node data it reads.
- **Access**: *watch* = a Node change triggers a reconcile; *cached read* =
  Nodes sit in the manager cache but a change triggers nothing; *read* = a
  direct `get`/`list` when needed.
- **Forbidden-Nodes issues/PRs**: upstream items about running without Node
  rights, with their state on 2026-10-16. "None found" means the GitHub
  searches `nodes forbidden`, `nodes clusterrole` and
  `nodes namespace permissions` returned nothing relevant.

| Operator | Needs Nodes? | Optional? | Cordon/taints | Labels | Annotations | Access | Forbidden-Nodes issues/PRs | NodeEcho sufficient? |
|---|---|---|---|---|---|---|---|---|
| CloudNativePG | yes, always | no upstream: startup fails without the rights | yes: `unschedulable` + `DRAIN_TAINTS` keys | yes: configured failure-domain keys | no | watch | [#9346](https://github.com/cloudnative-pg/cloudnative-pg/issues/9346) (closed not planned, 2026-09-30), [PR #9350](https://github.com/cloudnative-pg/cloudnative-pg/pull/9350) (closed, not merged), [PR #10135](https://github.com/cloudnative-pg/cloudnative-pg/pull/10135) `WATCH_NODES` (open) | **yes**, with an operator integration ([analysis](cloudnative-pg.md)) |
| ECK (Elasticsearch) | yes, while `config.exposedNodeLabels` is set (default) | yes: an empty list drops the rule, but Pods using zone awareness or `downward-node-labels` then wait in init | no | yes: listed keys + `zoneAwareness.topologyKey` | no | read (`Get` per Pod) | [#8585](https://github.com/elastic/cloud-on-k8s/issues/8585) (open) | **yes**, with `target: annotations`, if ECK skips its own Node `Get` |
| Strimzi (Kafka) | only for `type: topology-label` racks and NodePort listeners | yes: rack `type: environment-variable`; NodePort still needs addresses | no (Drain Cleaner works on evictions) | yes: `topologyKey` | no | read (init container at start; operator per reconcile) | [PR #12593](https://github.com/strimzi/strimzi-kafka-operator/pull/12593) env-var rack (merged 2026-04-14); [PR #3845](https://github.com/strimzi/strimzi-kafka-operator/pull/3845) `STRIMZI_RBAC_SCOPE` (merged 2021, Nodes stay cluster-wide); [#8583](https://github.com/strimzi/strimzi-kafka-operator/issues/8583), [#11669](https://github.com/strimzi/strimzi-kafka-operator/issues/11669) (closed, fixed by granting) | **partly**: rack yes if labelled at binding; NodePort needs addresses |
| Zalando postgres-operator | yes, always | no: the Node informer always starts | yes: `unschedulable` only | yes: `node_readiness_label`, `master=true` | no | watch | [#2304](https://github.com/zalando/postgres-operator/issues/2304) "Run operator with RoleBinding" (open) | **yes**: `unschedulable` echoed and the readiness label copied |
| Crunchy PGO | no | n/a | no | no | no | n/a | none current ([#180](https://github.com/CrunchyData/postgres-operator/issues/180) is a closed 2018 v3 issue) | not needed |
| Percona PG | no | n/a | no | no | no | n/a | none found | not needed |
| Percona PS-MySQL | no | n/a | no | no | no | n/a | none found | not needed |
| Percona PXC | no | n/a | no | no | no | n/a | none found | not needed |
| StackGres | no | n/a | no | no | no | n/a | not searched (issues on GitLab) | not needed |
| Percona PSMDB (MongoDB) | only in the cluster-wide manifest | yes: errors ignored, member tags lost | no | yes: zone, region | no | read (uncached `Get`) | none found | **yes** (KEP-4742 alone would do) |
| KubeBlocks | ClusterRole always; used only by NodeCountScaler | yes: NodeCountScaler is experimental, off by default | no | no | no | watch (NodeCountScaler only) | none found | **no** (Node count), feature optional |
| MongoDB Kubernetes | telemetry only | yes: `operator.telemetry.enabled: false` or `installClusterRole: false`; provider becomes "unknown" | no | yes | yes | read (`list`, first Node) | [PR #252](https://github.com/mongodb/mongodb-kubernetes/pull/252) telemetry RBAC switch (merged 2025-07-14) | not needed |
| MongoDB Community (legacy) | no | n/a | no | no | no | n/a | none found | not needed |
| cass-operator | no | n/a | no | no | no | n/a | [#681](https://github.com/k8ssandra/cass-operator/issues/681) unused `nodes` rights removed (closed 2024-07-22) | not needed |
| k8ssandra-operator | no | n/a | no | no | no | n/a | none found | not needed |
| CockroachDB (cockroach-operator) | yes, always | no: deploy fails on error | no | no | yes: first Node (distribution) | read (`list`) | none found | **no**: cluster fact; needs an upstream fix |
| TiDB operator v2 | yes, always | no: a read error fails the task | no | yes: PD `location-labels` | no | cached read | v1 only: [#4990](https://github.com/pingcap/tidb-operator/issues/4990) (closed), [#5034](https://github.com/pingcap/tidb-operator/issues/5034) (open), [PR #3896](https://github.com/pingcap/tidb-operator/pull/3896) namespace-scoped mode skipping forbidden operations (merged 2021) | **yes**, with configurable keys |
| Vitess | no | n/a | no | no | no | n/a | none found | not needed |
| RabbitMQ | no | n/a | no | no | no | n/a | none found | not needed |
| OT Redis | no | n/a | no | no | no | n/a | none found | not needed |
| Spotahome Redis | no | n/a | no | no | no | n/a | none found | not needed |
| Altinity ClickHouse | no | n/a | no | no | no | n/a | none found | not needed |
| Solr | no | n/a | no | no | no | n/a | none found | not needed |
| MinIO operator | ClusterRole always; used only for EKS detection | yes: `MINIO_OPERATOR_CSR_SIGNER_NAME` skips it; a list error falls back | no | yes: `eks.amazonaws.com*` | no | read (`list`) | none found | **no**, but the env var already bypasses it |
| Flink operator | only with `rbac.nodesRule.create` (default false) | yes: the autoscaler capacity check is lost | no | yes: `eks.amazonaws.com/nodegroup` | no | read (`list` + metrics) | none found | **no** (cluster capacity) |
| Rook/Ceph | yes, essential | no | yes: `unschedulable`, taints, conditions | yes: CRUSH topology | yes: mon IP | watch (spec changes) | none found | **no**: manages the nodes themselves |
| Redpanda | yes for rack awareness (off by default) and the PVC unbinder (off by default); operator holds `nodes get` | yes: both features are opt-in; the unbinder degrades on Forbidden | yes: PVC unbinder (`unschedulable`, taints, NotReady) | yes: rack key | yes: rack key can be an annotation | read (configurator at start; unbinder `list`) | [PR #1651](https://github.com/redpanda-data/redpanda-operator/pull/1651), [PR #1668](https://github.com/redpanda-data/redpanda-operator/pull/1668) (merged 2026-07): `nodes list/watch` only for multicluster installs | **partly** (addresses) |
| Koperator (Kafka) | ClusterRole always; used with `rackAwareness` or NodePort listeners | yes: both features are opt-in | no | yes: `rackAwareness.labels` | no | read (`Get`) | none found | **partly** (addresses) |
| Scylla operator | yes, essential | no: NodeConfig tuning and orphaned-PV cleanup | taints only (NodeConfig tolerations) | yes: NodeConfig `nodeSelector` | no | watch (informers) | [#1663](https://github.com/scylladb/scylla-operator/issues/1663) namespace-scoped operator (closed not planned, 2024-09-01) | **no** |

Access, issue states and the CloudNativePG row were checked on 2026-10-16
against the default branches; the other facts come from the commits pinned
below.

## Per operator

### ECK, 3.6.0-SNAPSHOT @ db828450a6
- RBAC: `nodes get/list/watch` is added to the ClusterRole only `if .Values.config.exposedNodeLabels`, and the default is `["topology.kubernetes.io/.*", "failure-domain.beta.kubernetes.io/.*"]`. See [cluster-roles.yaml#L15-17](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/deploy/eck-operator/templates/cluster-roles.yaml#L15-L17), [_helpers.tpl#L427-438](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/deploy/eck-operator/templates/_helpers.tpl#L427-L438) and [values.yaml#L326-328](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/deploy/eck-operator/values.yaml#L326-L328).
- What it reads: `Get` of the Pod's Node, then `node.Labels` for the keys listed in the `eck.k8s.elastic.co/downward-node-labels` annotation plus each NodeSet's `zoneAwareness.topologyKey` (default `topology.kubernetes.io/zone`). It writes them as **Pod annotations** with the same key. See [node_labels.go#L57-117](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/pkg/controller/elasticsearch/driver/shared/node_labels.go#L57-L117) and [elasticsearch_types.go#L631-645](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/pkg/apis/elasticsearch/v1/elasticsearch_types.go#L631-L645).
- How the Pod consumes them: the init container waits until the annotations appear in a downward API volume ([prepare_fs_script.go#L57-66](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/pkg/controller/elasticsearch/initcontainer/prepare_fs_script.go#L57-L66)). The `ZONE` env var is `fieldRef: metadata.annotations['<key>']` ([podspec.go#L283-303](https://github.com/elastic/cloud-on-k8s/blob/db828450a6/pkg/controller/elasticsearch/nodespec/podspec.go#L283-L303)).
- The feature is optional. Without Node rights, Pods hang in init: [issue #8585](https://github.com/elastic/cloud-on-k8s/issues/8585) (open).
- NodeEcho: works if it writes **annotations** with the node-label key. The wait-loop design tolerates a post-binding patch. ECK would need to skip its own Node `Get`, or a flag to do so.

### Strimzi, 1.4.0-SNAPSHOT @ 07e5697035
- RBAC:
  - The operator has `nodes list` for "node addresses when NodePort access is configured" ([021-ClusterRole#L30-38](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/packaging/install/cluster-operator/021-ClusterRole-strimzi-cluster-operator-role.yaml#L30-L38)).
  - Brokers and clients have `nodes get` ([030-ClusterRole-strimzi-kafka-broker.yaml](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/packaging/install/cluster-operator/030-ClusterRole-strimzi-kafka-broker.yaml), [033-ClusterRole-strimzi-kafka-client.yaml](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/packaging/install/cluster-operator/033-ClusterRole-strimzi-kafka-client.yaml)). The operator binds these through a ClusterRoleBinding per cluster ([KafkaCluster.java#L1659-1666](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/cluster-operator/src/main/java/io/strimzi/operator/cluster/model/KafkaCluster.java#L1659-L1666)), so the operator also needs rights to create ClusterRoleBindings.
- The `kafka-init` init container reads `node.metadata.labels[topologyKey]` for `broker.rack` and `node.status.addresses` (all types) for NodePort listeners ([InitWriter.java#L49-90](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/kafka-init/src/main/java/io/strimzi/kafka/init/InitWriter.java#L49-L90)). It runs only for `type: topology-label` racks or NodePort listeners ([KafkaCluster.java#L1564-1600](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/cluster-operator/src/main/java/io/strimzi/operator/cluster/model/KafkaCluster.java#L1564-L1600)).
- The operator itself `get`s the broker Nodes to fill NodePort listener status addresses, unless `advertisedHost` is set ([KafkaReconciler.java#L1163-1240](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/cluster-operator/src/main/java/io/strimzi/operator/cluster/operator/assembly/KafkaReconciler.java#L1163-L1240)).
- Existing workaround: since 1.0.0, rack `type: environment-variable` takes the rack from a Pod env var "that do[es] not require ClusterRoleBindings" ([EnvironmentVariableRack.java](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/api/src/main/java/io/strimzi/api/kafka/model/common/EnvironmentVariableRack.java), [CHANGELOG.md#L99](https://github.com/strimzi/strimzi-kafka-operator/blob/07e5697035/CHANGELOG.md#L99), [PR #12593](https://github.com/strimzi/strimzi-kafka-operator/pull/12593), SEP-128). An env var built from `fieldRef metadata.labels[...]` is resolved **when the container starts**, so the label must already be on the Pod at binding time.
- Drain: Strimzi does not read Node drain state. The separate [Drain Cleaner](https://github.com/strimzi/drain-cleaner/blob/3654e5794f/src/main/java/io/strimzi/ValidatingWebhook.java#L230-L240) intercepts evictions and annotates the Pod `strimzi.io/manual-rolling-update=true`. Its RBAC is pods only ([020-ClusterRole.yaml](https://github.com/strimzi/drain-cleaner/blob/3654e5794f/packaging/install/kubernetes/020-ClusterRole.yaml)). This is prior art for drain information on Pods.
- Issues: [#7986](https://github.com/strimzi/strimzi-kafka-operator/issues/7986) (missing broker ClusterRoleBinding) and [#2071](https://github.com/strimzi/strimzi-kafka-operator/issues/2071) (restricting bindings).
- NodeEcho: labels are enough for rack awareness if they are present at binding. NodePort needs addresses.

### Zalando postgres-operator, v2.0.3 @ 80b7a074ad
- RBAC: `nodes get/list/watch` "to check nodes for node readiness label". It is unconditional ([operator-service-account-rbac.yaml#L113-121](https://github.com/zalando/postgres-operator/blob/80b7a074ad/manifests/operator-service-account-rbac.yaml#L113-L121), [chart clusterrole.yaml#L125-133](https://github.com/zalando/postgres-operator/blob/80b7a074ad/charts/postgres-operator/templates/clusterrole.yaml#L125-L133)).
- A cluster-wide Node informer is always started ([controller.go#L423-431](https://github.com/zalando/postgres-operator/blob/80b7a074ad/pkg/controller/controller.go#L423-L431)). A node is "not ready" when `spec.unschedulable` is set and the node lacks `node_readiness_label`; nodes labelled `master=true` are skipped. Primaries are then switched over off that node ([node.go#L17-64](https://github.com/zalando/postgres-operator/blob/80b7a074ad/pkg/controller/node.go#L17-L64)). Rolling updates check the same thing per Pod ([pod.go#L564-570](https://github.com/zalando/postgres-operator/blob/80b7a074ad/pkg/cluster/pod.go#L564-L570)).
- Taints are not read, so Karpenter/autoscaler drains are invisible to it.
- Issue: [#2304 "Run operator with RoleBinding"](https://github.com/zalando/postgres-operator/issues/2304) (open).
- NodeEcho: yes, with `unschedulable` echoed and the user-configured readiness label copied.

### Crunchy PGO @ 0fbac3062d, Percona PG @ 1b8d57da0a, Percona PS-MySQL @ 4f75508717, Percona PXC @ 7813a0c48e, StackGres 1.20.0-SNAPSHOT @ 25a9b5a33e
- No grant of `nodes` in any RBAC manifest or kubebuilder marker, and no Node client call found (grep of Go/Java sources and YAML). Zone spreading is done only through Pod topology constraints.

### Percona PSMDB @ 2d5547962c
- `nodes get/list/watch` appears only in the cluster-wide manifest ([cw-rbac.yaml#L41-48](https://github.com/percona/percona-server-mongodb-operator/blob/2d5547962c/deploy/cw-rbac.yaml#L41-L48)). The namespaced `deploy/rbac.yaml` has none.
- It `Get`s the Pod's Node, uncached ([main.go#L112](https://github.com/percona/percona-server-mongodb-operator/blob/2d5547962c/cmd/manager/main.go#L112), [getters.go#L175-188](https://github.com/percona/percona-server-mongodb-operator/blob/2d5547962c/pkg/psmdb/getters.go#L175-L188)). It reads only `topology.kubernetes.io/region` and `/zone` into replica-set member tags, which also feed `primaryPreferTagSelector`. Errors are ignored ([mgo.go#L327-342](https://github.com/percona/percona-server-mongodb-operator/blob/2d5547962c/pkg/controller/perconaservermongodb/mgo.go#L327-L342)), so it already runs without Node rights, just without the tags.
- NodeEcho: yes, and KEP-4742 alone is enough if the code read Pod labels.

### KubeBlocks 1.0.0 @ af291e4078
- `nodes list/watch` is always present in the ClusterRole ([config/rbac/role.yaml#L53-59](https://github.com/apecloud/kubeblocks/blob/af291e4078/config/rbac/role.yaml#L53-L59), templated by [clusterrole.yaml#L20](https://github.com/apecloud/kubeblocks/blob/af291e4078/deploy/helm/templates/rbac/clusterrole.yaml#L20)).
- The only user is NodeCountScaler: replicas = number of Nodes ([reconciler_scale_target_cluster.go#L53-55](https://github.com/apecloud/kubeblocks/blob/af291e4078/controllers/experimental/reconciler_scale_target_cluster.go#L53-L55)). It runs only with `--experimental` ([main.go#L199,L578](https://github.com/apecloud/kubeblocks/blob/af291e4078/cmd/manager/main.go#L578)).

### MongoDB Kubernetes 1.13.1 @ 386f942c36; legacy Community @ f6a53c9612
- A telemetry-only ClusterRole grants `nodes list`. It is rendered unless `operator.telemetry.enabled: false` ([operator-roles-telemetry.yaml](https://github.com/mongodb/mongodb-kubernetes/blob/386f942c36/helm_chart/templates/operator-roles-telemetry.yaml)). The code lists 1 Node and maps its labels/annotations to a cloud provider, returning "unknown" on error ([cluster.go#L108-133](https://github.com/mongodb/mongodb-kubernetes/blob/386f942c36/pkg/telemetry/cluster.go#L108-L133)).
- The legacy community repo has no Node use.

### cass-operator @ 8db0df4f2b, k8ssandra-operator @ 2b5de2813f
- No `nodes` in RBAC ([cass-operator config/rbac](https://github.com/k8ssandra/cass-operator/tree/8db0df4f2b/config/rbac)). Racks are pinned by node affinity; the rack name comes from the CR, not from the Node. `DatacentersForNode` exists but has no Node watch ([handler.go#L26-35](https://github.com/k8ssandra/cass-operator/blob/8db0df4f2b/pkg/reconciliation/handler.go#L26-L35)).

### CockroachDB cockroach-operator 2.18.5 @ 9dbbc126f7
- `nodes get/list` ([role.yaml#L138-144](https://github.com/cockroachdb/cockroach-operator/blob/9dbbc126f7/config/rbac/role.yaml#L138-L144)). It lists Nodes, reads the first one's `status.nodeInfo.kubeletVersion` and annotations to label the distribution for telemetry ([kubernetes_distro.go#L41-80](https://github.com/cockroachdb/cockroach-operator/blob/9dbbc126f7/pkg/kube/kubernetes_distro.go#L41-L80)).
- The deploy fails if this errors ([deploy.go#L57-60](https://github.com/cockroachdb/cockroach-operator/blob/9dbbc126f7/pkg/actor/deploy.go#L57-L60)).
- Locality is static per CR ([example](https://github.com/cockroachdb/cockroach-operator/blob/9dbbc126f7/examples/3-nodes-insecure.yaml#L33)).
- NodeEcho: no. This needs an upstream fix (tolerate errors) rather than NodeEcho.

### TiDB operator v2 (main) @ 14cd265702
- `nodes get/list/watch` is always in the ClusterRole ([rbac.yaml#L36-38](https://github.com/pingcap/tidb-operator/blob/14cd265702/charts/tidb-operator/templates/rbac.yaml#L36-L38)). The Node cache is commented "need to sync some labels from nodes to TiKV, TiDB" ([main.go#L774-776](https://github.com/pingcap/tidb-operator/blob/14cd265702/cmd/tidb-operator/main.go#L774-L776)).
- The keys come from PD `replication.location-labels`; `zone`, `region` and `host` are aliased to the well-known labels ([node.go#L20-50](https://github.com/pingcap/tidb-operator/blob/14cd265702/pkg/utils/k8s/node.go#L20-L50)). A Node read error fails the task ([task.go#L228-256](https://github.com/pingcap/tidb-operator/blob/14cd265702/pkg/controllers/common/task.go#L228-L256)).
- Issues (v1.4): [#4990](https://github.com/pingcap/tidb-operator/issues/4990) (panics without cluster permissions, closed), [#5034](https://github.com/pingcap/tidb-operator/issues/5034).
- NodeEcho: yes, with keys configured per consumer. `host` needs `kubernetes.io/hostname`, which KEP-4742 does not copy.

### Vitess @ 4b576e7e05, RabbitMQ @ 89f55c3297, OT Redis @ 71954323c9, Spotahome Redis @ 632aa3da88, Altinity ClickHouse @ a89fa9e2ed, Solr @ ed5c5c7d28
- No `nodes` RBAC and no Node client calls found.

### MinIO operator 7.1.1 @ 34dc398019
- `nodes get/watch/list` ([cluster-role.yaml#L21-29](https://github.com/minio/operator/blob/34dc398019/resources/base/cluster-role.yaml#L21-L29)). It is used only to detect EKS (kubeletVersion, `eks.amazonaws.com*` labels) when choosing the CSR signer.
- It is skipped when `MINIO_OPERATOR_CSR_SIGNER_NAME` is set; on a list error it logs and defaults to `kubernetes.io/kubelet-serving` ([csr.go#L59-133](https://github.com/minio/operator/blob/34dc398019/pkg/controller/certificates/csr.go#L59-L133)).

### Flink operator 1.17-SNAPSHOT @ 284a8d005f
- `nodes list` only `if .Values.rbac.nodesRule.create`, which defaults to false ([_helpers.tpl#L85-92](https://github.com/apache/flink-kubernetes-operator/blob/284a8d005f/helm/flink-kubernetes-operator/templates/rbac/_helpers.tpl#L85-L92)). It reads allocatable, node metrics and `eks.amazonaws.com/nodegroup` ([ClusterResourceManager.java#L159-185](https://github.com/apache/flink-kubernetes-operator/blob/284a8d005f/flink-kubernetes-operator/src/main/java/org/apache/flink/kubernetes/operator/resources/ClusterResourceManager.java#L159-L185)).

### Rook/Ceph @ 863ff2c8c7
- Rook really needs Nodes:
  - valid-node selection from taints, `unschedulable` and Ready conditions ([node.go](https://github.com/rook/rook/blob/863ff2c8c7/pkg/operator/k8sutil/node.go#L165-L285));
  - CRUSH topology from labels ([topology.go#L53](https://github.com/rook/rook/blob/863ff2c8c7/pkg/operator/ceph/cluster/osd/topology/topology.go#L53));
  - mon IPs from `status.addresses` or a Node annotation ([mon/node.go#L38-60](https://github.com/rook/rook/blob/863ff2c8c7/pkg/operator/ceph/cluster/mon/node.go#L38-L60));
  - drain detection for OSD PDBs ([osd.go#L539-547](https://github.com/rook/rook/blob/863ff2c8c7/pkg/operator/ceph/disruption/clusterdisruption/osd.go#L539-L547)).
- Out of scope for NodeEcho.

### Others found
- **Redpanda** @ b1dc90d63b:
  - The chart's configurator script `curl`s `/api/v1/nodes/$NODE` with the Pod token and greps a configurable key `rackAwareness.nodeAnnotation` (default `topology.kubernetes.io/zone`, label or annotation) ([scripts.go#L301-312](https://github.com/redpanda-data/redpanda-operator/blob/b1dc90d63b/charts/redpanda/scripts.go#L301-L312), [values.yaml#L140](https://github.com/redpanda-data/redpanda-operator/blob/b1dc90d63b/charts/redpanda/chart/values.yaml#L140)).
  - The legacy configurator reads zone labels and addresses ([configurator.go#L361-372](https://github.com/redpanda-data/redpanda-operator/blob/b1dc90d63b/operator/cmd/configurator/configurator.go#L361-L372)).
  - The PVC unbinder reads `unschedulable`, conditions and taints, and degrades on Forbidden ([pvcunbinder.go#L1060-1070, L1795-1840](https://github.com/redpanda-data/redpanda-operator/blob/b1dc90d63b/operator/internal/controller/pvcunbinder/pvcunbinder.go#L1795-L1840)).
- **Koperator** @ cada71e3a9: reads the labels listed in `spec.rackAwareness.labels` ([zoneandregion.go#L25-39](https://github.com/adobe/koperator/blob/cada71e3a9/pkg/k8sutil/zoneandregion.go#L25-L39), [cr.go#L37-38](https://github.com/adobe/koperator/blob/cada71e3a9/pkg/k8sutil/cr.go#L37-L38)) and the NodePort host from `status.addresses` by type ([kafka.go#L1761-1777](https://github.com/adobe/koperator/blob/cada71e3a9/pkg/resources/kafka/kafka.go#L1761-L1777)).
- **Scylla** @ bc0a3fe8cc: Node informers for node tuning and orphaned-PV cleanup ([controllers.go#L56](https://github.com/scylladb/scylla-operator/blob/bc0a3fe8cc/pkg/controllermanager/controllers.go#L56)). These are real node-level needs.
- **Envoy Gateway** @ f74a274b10 (prior art, not a database): a mutating webhook on `pods/binding` copies the Node's zone into the Binding's annotations ([topology_injector.go#L77-103](https://github.com/envoyproxy/gateway/blob/f74a274b10/internal/provider/kubernetes/topology_injector.go#L77-L103), [webhook](https://github.com/envoyproxy/gateway/blob/f74a274b10/charts/gateway-helm/templates/envoy-proxy-topology-injector-webhook.yaml)). Issue [#7443](https://github.com/envoyproxy/gateway/issues/7443) plans to replace it with KEP-4742.

## What a generic projector must offer

1. **Correction on KEP-4742.** In v1.35 the plugin copies only `topology.kubernetes.io/zone` and `topology.kubernetes.io/region`, **not** `kubernetes.io/hostname`, and the list is not configurable ([admission.go#L43-45](https://github.com/kubernetes/kubernetes/blob/v1.35.0/plugin/pkg/admission/podtopologylabels/admission.go#L43-L45)). It is beta and on by default from 1.35 ([kube_features.go#L1650-1653](https://github.com/kubernetes/kubernetes/blob/v1.35.0/pkg/features/kube_features.go#L1650-L1653)).
2. **Configurable label keys.** Zone and region dominate, but every rack feature takes user keys: Strimzi `topologyKey`, ECK annotation, TiDB `location-labels`, Koperator `labels`, Redpanda `nodeAnnotation`, Zalando readiness label, CNPG. NodeEcho needs a per-consumer list of keys, and should allow Node annotations as well as labels (Redpanda, Rook).
3. **Same key, but labels *and* annotations.** Every consumer keeps the node-label key unchanged. ECK expects **annotations**; Strimzi env-var, TiDB, PSMDB and CNPG would read labels. Offer both targets.
4. **Timing: inject at binding.** Env-var consumers (Strimzi `environment-variable`, ECK `ZONE`) resolve `fieldRef` at container start, so a patch after binding races. The binding subresource copies Binding **annotations** onto the Pod, and with `PodTopologyLabelsAdmission` on it copies **all** Binding labels ([storage.go#L253-260](https://github.com/kubernetes/kubernetes/blob/v1.35.0/pkg/registry/core/pod/storage/storage.go#L253-L260)). A mutating webhook on `pods/binding`, as Envoy Gateway does, can therefore set arbitrary node labels and annotations atomically at binding. A post-binding reconciler is still needed for later Node label changes and for Pods bound while the webhook was down.
5. **Drain signal.** Few operators read one: Zalando (`unschedulable` only), Rook (`unschedulable`), Redpanda (`unschedulable` + NotReady + taints), CNPG (`unschedulable` + taints). Strimzi instead marks Pods on eviction (Drain Cleaner). Echoing `unschedulable` and a user-listed set of taints covers all but NotReady, and leaves the policy to each consumer. An optional eviction-time mark, Drain Cleaner style, would also catch drains that never cordon.
6. **Node addresses (gap).** Strimzi NodePort, Koperator NodePort, Redpanda external and Rook mons need `status.addresses` by type (InternalIP, ExternalIP, Hostname, InternalDNS, ExternalDNS). NodeEcho could publish them as per-type annotations. Without that, NodePort-style listeners still need Node rights.
7. **Out of reach.** Cluster facts (distribution or provider for Cockroach, MinIO, MongoDB telemetry; Node count for KubeBlocks; capacity for Flink) are not per Pod. Most are optional or have an override; Cockroach needs an upstream fix.
8. **Second-order gain.** Strimzi and Redpanda give Node rights to the *operand* Pods, so the operator must be able to create ClusterRoleBindings. NodeEcho would remove that escalation path too.

## Uncertain

- Zalando's behaviour when `nodes` is forbidden: informer errors only, or broken rolling updates? Not tested; `podIsEndOfLife` returns the error ([pod.go#L564](https://github.com/zalando/postgres-operator/blob/80b7a074ad/pkg/cluster/pod.go#L564)).
- MinIO falling back cleanly on a list error assumes client-go returns a non-nil empty list. Not run.
- The newer CockroachDB operator (cockroachdb/helm-charts, "cockroachdb-parent") is said to derive locality from Node labels. Not read.
- TiDB operator v1.x (release branches) Node use beyond issue #4990. Not read.
- Aerospike's init container (`aerospike-kubernetes-init`, separate repo) is believed to read Node addresses. Not read; the operator repo itself has no Node calls outside tests.
- MariaDB operator: no Node calls found, but not inspected deeply.
- Whether the API server lets a webhook on `pods/binding` mutate Binding labels in every case: proven only for annotations (Envoy). Labels are inferred from the 1.35 storage code.
- Issue searches are keyword based (see the Summary); an item using other wording may be missing.
