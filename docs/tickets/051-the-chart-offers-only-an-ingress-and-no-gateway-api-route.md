---
id: T51
title: the chart offers only an Ingress and no Gateway API route, while the Ingress controller most clusters ran is retired
state: analysed
severity: medium
security: hardening
threat: would additionally cover an installation that keeps running a retired Ingress controller, without security fixes, because the chart offers it no route of the Gateway API
urgency: icebox       # rule 5: needs the owner's product call
effort: S
blocked-by: decision
filed-from: the change that routes /api/ and /auth/ at the Ingress, 2026-10-04
opened: 2026-10-04
decided:
done:
---

## Current state

The chart routes `/api/` and `/auth/` to the backend Service and `/` to the frontend Service with
one `networking.k8s.io/v1` Ingress ([`ingress.yaml`](../../deploy/helm/cowork/templates/ingress.yaml),
[ADR 0001](../adr/0001-two-containers-a-go-backend-and-an-nginx-frontend-installed-by-one-helm-chart.md)
D3). It works with any Ingress controller that honours path rules, does not buffer
`text/event-stream` and lets bodies of the backend's upload limit through
([installation.md](../operations/installation.md)).

Kubernetes retired ingress-nginx in March 2026: no releases and no security fixes after it
([announcement](https://kubernetes.io/blog/2025/11/11/ingress-nginx-retirement/),
[statement](https://kubernetes.io/blog/2026/01/29/ingress-nginx-statement/)), and names the Gateway
API as the successor of Ingress. The chart offers no `HTTPRoute`, so an installation on a Gateway
needs a route of its own, outside the chart.

## Required changes

Depends on the answer.

1. As Q1 decides: an `HTTPRoute` (`gateway.networking.k8s.io/v1`) with the same three path rules,
   attached to a Gateway the operator names (`parentRefs`), beside or instead of the Ingress;
   values, `make helm-lint helm-template` with a ci values file for it, the reference in the
   README and the installation page; ADR 0001 D3 amended with the answer.
2. Verified against one Gateway API implementation in a throwaway cluster, as the Ingress was:
   the UI, a login, the event stream past the read timeout, an upload at the backend's limit.

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

**Answer:** _open_

## Related

- T29 — the end-to-end tier, which runs the images behind a stand-in for the Ingress
