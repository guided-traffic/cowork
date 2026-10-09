---
id: T72
title: the backend pod template carries unsalted SHA-256 hashes of the inline credentials, readable without access to Secrets
state: done
severity: low
security: boundary
threat: a namespace viewer without access to Secrets (Kubernetes' built-in view role) reads checksum/local-admin-secret (SHA-256 of user:password), checksum/database-secret and checksum/database-owner-secret from the backend pod template or an old ReplicaSet and guesses the local administrator's and both database roles' passwords offline
urgency: later         # decided 2026-10-07, waits for the build; dormant unless the inline values are used
effort: XS
blocked-by:
filed-from: the security pages reviewed against the code, 2026-10-07
opened: 2026-10-07
decided: 2026-10-07
done: 2026-10-09
shipped: 0.13.0: the backend pod template carries one annotation with the release revision while an inline credential is set, and no hash of a credential
---

## Current state

deploy/helm/cowork/templates/backend-deployment.yaml:17-25 renders the three annotations from the
inline values, so that a changed value rolls the pods (README's objects table).

## Open questions

### Q1: Do the pod's annotations keep a checksum of an inline credential?

- **(a) Drop the three annotations**; a changed inline value takes a restart, as a referenced Secret already does; anyone who used an inline password rotates it.
- **(b) Keep them** and name the exposure in docs/security/trust-boundaries.md.

Recommended: **(a)** — the inline path is the throw-away one already; a hash of a password in a field every viewer reads is the one exposure it adds beyond `helm get values`.

- **(c) An annotation with `.Release.Revision` instead of the hashes, while an inline value is set**:
  every upgrade rolls the pods, a changed value takes effect at once, and no hash is in the template.
  Cost: on the inline path every upgrade restarts the pods; under `helm template` and Argo CD the
  revision stays 1 and nothing rolls.

A checksum over everything does not help: every other input is readable by the same viewer or is the
public chart template, so the password stays the only unknown; only a high-entropy secret the viewer
cannot read would salt it, and the chart sees none at render time (a `lookup` breaks `helm template`,
Argo CD and `--dry-run`). Dropping the annotations alone has a cost the first draft missed: the local
administrator's synchronisation runs at the start, so an inline password rotated by `helm upgrade`
would stay valid until a manual restart. Recommended, revised: **(c)**.

**Answer:** (c) — the owner, 2026-10-07. The three checksum annotations give way to one
annotation with `.Release.Revision`, rendered while any inline credential is set; the README's
objects table and the record of the inline path change with the fix, in the same change.
