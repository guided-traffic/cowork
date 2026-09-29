# Developer documentation

Overviews for people changing this code. The detail is in the code; what lives here is the
shape of things — how the pieces fit, which invariants hold, and why a design that looks odd
is the way it is.

**What belongs here:** anything a future developer needs before touching a subsystem, that the
code cannot state on its own. A package map. The startup sequence. The fixtures a test tier
gives you. The hard-won knowledge from a defect that was expensive to find.

**What does not:** decisions (those are [ADRs](../adr/README.md)), work lists (those are
[tickets](../tickets/README.md), archived when the work lands), what somebody running cowork
needs (that is [docs/operations/](../operations/README.md), with the reference tables in
[README.md](../../README.md)), and the security design (that is [docs/security/](../security/README.md)).
[ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)
is the rule that separates those homes.

A page here **may and should** point at files and functions. That is the point of it. It also
means it goes stale when the tree moves, so whoever moves the tree updates the page in the same
change.

| Page | Read it when |
|---|---|
| [package-map.md](package-map.md) | You are new, or you are looking for where something lives |
| [architecture.md](architecture.md) | You want the picture: what runs where, what a request goes through, what happens at start |
| [testing.md](testing.md) | You are adding a test, choosing a tier, or a suite is failing and you need to know what it is for and what it needs |

## What has no page here

The contributor-facing material that is not per-subsystem — repository layout, the build and
test matrix, continuous integration and the release, the extension checklists, the conventions,
the toolchain versions — is [DEVELOPER.md](../../DEVELOPER.md).

There are no subsystems beyond the three pages above yet: no domain, no authentication, no
API beyond health and version. The order in which they are to be built is
[docs/planning/project-plan.md](../planning/project-plan.md); each gets its page here when it
exists.
