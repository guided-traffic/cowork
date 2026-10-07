# Operating cowork

Reference for the people who install cowork and keep it running. [README.md](../../README.md)
is the short version — what cowork is, how to start it, and the complete configuration, Helm
values, API and CLI reference. Everything it links to for detail is here. A page here explains
a setting; it never restates the reference tables, which live in the README and nowhere else
([ADR 0002](../adr/0002-documentation-has-five-homes-and-tickets-are-work-lists-that-get-archived.md)).

| Page | Read it when |
|---|---|
| [installation.md](installation.md) | You are installing cowork from the checked-out chart: the database and its two roles, the Secrets, the local administrator, the identity provider and its Dex reference fixture, the object storage and the example manifests for CloudNativePG and MinIO, the chat's model, how the schema is migrated — on start or in a Helm hook Job, with Argo CD and Flux —, exposing it through an Ingress and its controller's limits, the trusted proxies, which network policies are yours, upgrading or uninstalling |
| [runtime.md](runtime.md) | You want to know what happens when the pods start, how the schema migration runs — and the bootstrap after it in the migration Job — and how a failed one is repaired, what the probes answer, what a failed login shows — the local one and the identity provider's — and how a session keeps up with the provider's groups, which limits hold a request, what answers what — the backend, the Ingress controller, nginx —, how the event stream and the chat's stream behave behind an Ingress, how the processes shut down and what they log |
| [chat.md](chat.md) | You give the UI its assistant: the provider inside or outside the installation and the tenants' consent, LM Studio and the context it loads a model with, OpenAI, Anthropic, the chart's `chat` values, the stream behind an Ingress, the limits, the log, and what to check when the assistant does not answer |
| [claude-code.md](claude-code.md) | You set up Claude Code to work from an installation: the `cowork-mcp` binary, the token, the plugin with its hooks and skills or the same by hand, the `CLAUDE.md` block and `.cowork.yaml` of a repository, and what to check when a session starts without its ticket |

## The other documentation

| Where | What |
|---|---|
| [README.md](../../README.md) | What cowork is, the fast start, the naming conventions, the complete reference |
| [docs/security/](../security/README.md) | Trust boundaries, where the credentials live, and what each mechanism leaves open. Reporting a vulnerability is [SECURITY.md](../../SECURITY.md) |
| [docs/adr/](../adr/README.md) | Why cowork behaves the way it does, and what was rejected |
| [docs/developer/](../developer/README.md) | Changing the code |
