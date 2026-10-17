# Contributing to NodeEcho

Thank you for looking into NodeEcho. This page says how a change gets in.
`AGENTS.md` holds the project's rules for anyone working in the repository,
human or agent; read it first. The documentation site (`web/docs/`) explains
the behavior, and `web/docs/reference/contract.md` is what consumers rely on.

Contributions are licensed under the [Apache License 2.0](LICENSE), like the
rest of the project.

## Before you start

- Open an issue for anything beyond a small fix, so the design is discussed
  before the code. NodeEcho stays a plain echo of Node facts; a feature that
  interprets them belongs in the consumer.
- Questions go in an issue too. There is no wiki or discussion board.

## A change

- One change per pull request, on a branch of your fork, against `main`.
- A change of behavior updates the contract and the site in the same change.
- New behavior needs a test that fails without it; a bug fix shows the test
  failing on the old code.
- A change in `config/` usually needs the same change in `deploy/` (Helm
  chart, OLM bundle); `make validate-packaging` checks that they agree.
- Before pushing:

  ```bash
  make test lint vuln test-alerts validate-packaging
  cd web && npm ci && npm run typecheck && npm run build
  ```

  `make e2e` runs the end-to-end suite on throwaway k3d clusters. CI does
  not run it, so say in the pull request whether you did, and which variants.

## Commits

- [Conventional Commits](https://www.conventionalcommits.org/), compact: a
  subject line (`feat:`, `fix:`, `docs:`, `build:`, `ci:`, `test:`,
  `refactor:`, `chore:`), then a short body saying what and why. No issue or
  pull request numbers in the message; they belong in the pull request.
- Every commit is signed off (`git commit -s`), which certifies the
  [Developer Certificate of Origin](https://developercertificate.org/) under
  your real name and email address. A check on the pull request refuses
  commits without it.

## AI assistance is disclosed, every time

Contributions made with the help of an AI tool are welcome, and they are
disclosed wherever they appear, without exception:

- **Commits**: a trailer next to the sign-off naming the tool, for instance
  `Assisted-by: Claude Code` or `Assisted-by: GitHub Copilot`. The tool is
  not an author: no `Co-Authored-By` line for it.
- **Issues and pull requests**: a short `Disclosure` section saying what the
  tool did (drafted the text, wrote the code, wrote the tests, reviewed).
- **Reviews and comments**: a line saying so.

"Assistance" covers any part of the content: text, code, tests, commit
message. A contribution found to be AI-assisted without disclosure is closed.

You remain the author. You have read, understood and tested everything you
submit, and you sign it off under the DCO as your own work. An agent working
in this repository follows `AGENTS.md`.

## Review

The maintainer reviews every pull request. CI must be green; the review
looks at the contract, the tests, the documentation and the packaging.
Expect questions rather than silent rewrites.

## Security

Do not open a public issue for a vulnerability; see [SECURITY.md](SECURITY.md).

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
