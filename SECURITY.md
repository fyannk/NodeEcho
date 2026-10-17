# Security policy

## Reporting a vulnerability

Do not open a public issue. Report it privately, either way:

- GitHub's private vulnerability reporting on this repository ("Report a
  vulnerability" under the Security tab);
- an email to <fyannk@fyannk.dev>.

Say what the issue is, how to reproduce it and what you believe the impact
is. NodeEcho has one maintainer working on it in their spare time: expect an
acknowledgement within a week, and an answer on whether the report is
accepted once it is reproduced. Fixes ship in a new release, with a GitHub
security advisory crediting the reporter unless they prefer not to be named.

## Supported versions

NodeEcho has no release yet; `main` is what exists. Once released, only the
latest release receives fixes.

## Scope

NodeEcho's own code, manifests, Helm chart, OLM bundle and images.

Out of scope: vulnerabilities in dependencies whose affected code NodeEcho
does not call (govulncheck runs in CI and reports the ones it does), and the
configuration of the cluster NodeEcho runs on.

## What NodeEcho touches

NodeEcho reads Nodes and pods cluster-wide and writes labels and annotations
on pods, under one field manager per rule, never on another writer's keys.
Its webhook, when enabled, answers eviction requests with
`failurePolicy: Ignore`. The admission policies in `config/policy` bound
what it may write. Anything beyond that is a bug worth reporting.
