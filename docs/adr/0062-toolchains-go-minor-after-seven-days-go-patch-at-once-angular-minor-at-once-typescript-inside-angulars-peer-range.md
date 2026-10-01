# ADR 0062: Toolchains — a Go Minor Automerges After Seven Days, a Go Patch at Once, an Angular Minor at Once, Go Tools Before Go, and TypeScript Stays Inside Angular's Peer Range

## Status

Accepted. Date: 2026-10-01. Decided by the owner as the answer to the catalog question
"toolchain version policy beyond ADR 0001 D9": the recommendation in full — a release age for
Go minors, no age for Go patches and Angular minors — over automerging Go minors at once, over
manual Go minors, and over a release age for Angular minors too.

**Implemented** in [`renovate.json`](../../renovate.json) in the change that wrote this record:
the `golang-version` minor rule carries `minimumReleaseAge: 7 days`, the Makefile-pinned Go
tools automerge minors at once, `typescript` is held to Angular's peer range, and `npm`
(`packageManager`) automerges minor and patch. Not verified until Renovate's next run
evaluates the rules against the open pull requests.

## Context

[ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D9 makes both toolchains track the newest release. Go has no majors; a minor (1.27 → 1.28)
is the deepest change it ever ships — compiler, vet, the toolchain line in `go.mod` — and
`golangci-lint` has to know the new version before a Go bump can be green. Angular minors
arrive every six weeks, are shallow, and pull PrimeNG and the CDK through their peer ranges;
the Angular group is gated by lint, unit tests, build and the end-to-end tier. TypeScript is
pinned by Angular's peer range (`>=6.0 <6.1` for Angular 22), and Renovate opened a
TypeScript 7 pull request that can never be green. Patches of either toolchain are security
fixes more often than not.

## Decision

**D1 — Go patch releases automerge at once;** they are the security channel.

**D2 — Go minor releases automerge after a release age of seven days.** Regressions found in
a new minor's first week do not reach the repository; afterwards the bump lands without a
click. The grouped "Go version" pull request moves `backend/go.mod`, the `Containerfile`, the
workflow and the badge together ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D9).

**D3 — The Makefile-pinned Go tools automerge minors and patches at once, independently of
Go.** `golangci-lint`, `gocyclo`, `gosec`, `govulncheck` may and should run ahead of the Go
version they must understand.

**D4 — Angular minors and patches automerge at once** in the "Angular" group (framework, CLI,
build, angular-eslint, PrimeNG, `@primeuix/themes`, PrimeIcons); Angular majors wait for a
person ([ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D9 holds, the gates decide).

**D5 — TypeScript stays inside Angular's peer range.** Renovate's `allowedVersions` for
`typescript` mirrors `@angular/compiler-cli`'s peer range and is updated in the same change
as an Angular major; a TypeScript major outside the range is not proposed.

**D6 — `npm` (the `packageManager` field) automerges minor and patch; a major waits.**

**D7 — Node.js in the frontend `Containerfile` and the workflow follows the dockerfile and
regex managers already configured:** minors and patches automerge, a major waits, as for
every base image.

## Consequences

- A new Go minor reaches the repository one week after release without anyone's click,
  unless CI says no; a Go patch and an Angular minor the same day.
- The open TypeScript 7 pull request is closed by the rule, not by hand, at Renovate's next
  run; the owner may close it earlier.
- D3 makes the Go bump greenable: the linter is already at the version that knows the new
  Go.
- The rules are configuration; `renovate.json` carries a description per rule naming this
  record.

## Alternatives Considered

- **Go minors at once.** Literal "newest release"; a minor that changes vet or needs a newer
  linter leaves a red pull request open until someone notices. Lost to a week's age.
- **Go minors by hand.** A person reads the release notes and clicks; the owner becomes the
  bottleneck for something CI can decide. Lost.
- **A release age for Angular minors too.** Shallow, frequent, fully gated; a delay buys
  nothing. Lost.

## Residual risks

- A release age trusts the ecosystem to find regressions in a week; one found later lands
  like any other bump and is reverted like any other.
- D5's `allowedVersions` is a string that must change with every Angular major; the Angular
  major pull request's checklist says so.

## References

- [`renovate.json`](../../renovate.json) — the rules
- [ADR 0001](0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md) D9 — the policy this record details
- [ADR 0052](0052-primeng-with-the-angular-cdk-a-themes-preset-and-dark-mode-from-the-start.md) D7 — the Angular group's members
- [ADR 0003](0003-test-and-ci-policy.md) D4 — the gates that let automerge be safe
