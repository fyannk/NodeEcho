# Documentation

NodeEcho is currently a design proposal. These notes capture research checked on
**2026-10-16** and distinguish verified upstream behavior from proposed work.

| Document | Contents |
| --- | --- |
| [CloudNativePG analysis](cloudnative-pg.md) | Node dependencies, optional-watch work, topology, and integration points |
| [Ecosystem research](ecosystem.md) | Kubernetes, Strimzi, ECK, Prometheus, and related approaches |
| [Operators survey](operators.md) | What about twenty data and messaging operators read from Nodes, and whether NodeEcho would cover it |
| [Design notes](design.md) | Proposed scope, data contract, consistency, permissions, and validation |
| [Contract v1](contract.md) | The keys a consumer sees on its pods, their values, and what it may rely on |

The research uses primary project documentation, source code, and upstream
issues/PRs. Links to moving documentation are accompanied by source snapshots
where useful. A source being linked does not mean its project endorses NodeEcho.

The implementation will use Go and controller-runtime. No public API, license,
or release commitment has been selected. YAML examples under the proposed design are illustrative and cannot
be installed.
