<!-- One change per pull request. CONTRIBUTING.md says what is expected. -->

## What and why



## Checks

- [ ] `make test lint vuln test-alerts validate-packaging` pass
- [ ] `make e2e` run (which variants), or not applicable because
- [ ] a change of behavior updates `web/docs/reference/contract.md` and the site
- [ ] a change in `config/` has its counterpart in `deploy/` (Helm chart, OLM bundle)
- [ ] every commit is signed off (DCO) with a Conventional Commits subject

## Disclosure

<!-- Mandatory. Either "No AI assistance." or which tool did what, for
     instance "Claude Code drafted the tests and the doc paragraph; I wrote
     the code." Assisted commits carry an `Assisted-by: <tool>` trailer. -->

