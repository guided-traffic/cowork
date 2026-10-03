# Operating cowork

Reference for the people who install cowork and keep it running. [README.md](../../README.md)
is the short version — what cowork is, how to start it, and the complete configuration, Helm
values, API and CLI reference. Everything it links to for detail is here. A page here explains
a setting; it never restates the reference tables, which live in the README and nowhere else
([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).

| Page | Read it when |
|---|---|
| [installation.md](installation.md) | You are installing cowork from the checked-out chart: the database and its two roles, the Secrets, the object storage, how the schema is migrated, exposing it through an Ingress, upgrading or uninstalling |
| [runtime.md](runtime.md) | You want to know what happens when the pods start, how the schema migration runs and how a failed one is repaired, what the probes answer, which limits hold a request, what nginx answers itself, how the event stream behaves behind an Ingress, how the processes shut down and what they log |

## The other documentation

| Where | What |
|---|---|
| [README.md](../../README.md) | What cowork is, the fast start, the naming conventions, the complete reference |
| [docs/security/](../security/README.md) | Trust boundaries, where the credentials live, and what each mechanism leaves open. Reporting a vulnerability is [SECURITY.md](../../SECURITY.md) |
| [docs/adr/](../adr/README.md) | Why cowork behaves the way it does, and what was rejected |
| [docs/developer/](../developer/README.md) | Changing the code |
