---
id: T51
title: the chart offers only an Ingress and no Gateway API route, while the Ingress controller most clusters ran is retired
state: dropped
severity: medium
security: hardening
threat: would additionally cover an installation that keeps running a retired Ingress controller, without security fixes, because the chart offers it no route of the Gateway API
urgency: icebox       # rule 5: needs the owner's product call
effort: S
filed-from: the change that routes /api/ and /auth/ at the Ingress, 2026-10-04
opened: 2026-10-04
decided: 2026-10-06
done:
dropped-reason: the owner's answer (a) — the chart offers the Ingress only and an installation on a Gateway writes its own HTTPRoute; ADR 0001 D3
---

## Current state

The chart routes `/api/` and `/auth/` to the backend Service and `/` to the frontend Service with
one `networking.k8s.io/v1` Ingress ([`ingress.yaml`](../../../deploy/helm/cowork/templates/ingress.yaml))
and offers no route of the Gateway API, by the owner's decision
([ADR 0001](../../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3, its Alternatives and its Residual risks). [installation.md](../../operations/installation.md#expose-it)
says so and tells an installation on a Gateway to leave the Ingress off and write its own
`HTTPRoute` with the same three path rules; [trust-boundaries.md](../../security/trust-boundaries.md#the-transport-in-front-of-the-pods)
names what is left: which controller runs, and whether it still gets security fixes, is the
installation's.

## Required changes

None. The owner decided that the chart builds no `HTTPRoute` (Q1).

## Open questions

### Q1: Does the chart offer a route of the Gateway API?

The routing is three path rules either way; what differs is which API the cluster speaks. Body
limits, timeouts and buffering are the implementation's settings in both.

- **(a) The Ingress only:** any maintained Ingress controller works; the docs stay generic. An
  installation on a Gateway writes its own `HTTPRoute`.
- **(b) Both, one switched on:** `ingress.*` as today and `httpRoute.*` with `parentRefs`; the
  operator picks the one their cluster has.
- **(c) The `HTTPRoute` only:** the chart follows Kubernetes' successor and drops the Ingress.

Recommended: **(b)** — the Gateway API is where Kubernetes points every installation that leaves
ingress-nginx, the route is a small template beside the Ingress with the same rules, and an
installation on any maintained Ingress controller keeps working unchanged.

**Answer:** (a) — the chart offers the Ingress only; an installation on a Gateway writes its own
`HTTPRoute`. ADR 0001 D3.

## Related

- T29 — the end-to-end tier, which runs the images behind a stand-in for the Ingress
