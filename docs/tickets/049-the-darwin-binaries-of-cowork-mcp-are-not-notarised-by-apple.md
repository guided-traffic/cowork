---
id: T49
title: the darwin binaries of cowork-mcp are not notarised by Apple, so Gatekeeper quarantines them
state: filed
severity: low
security: hardening
threat: would additionally cover a darwin binary replaced on its way to the person who installs it and never checked with `gh attestation verify` — Gatekeeper would refuse it at its first start
urgency: later        # rule 4: a known fix; it waits for an Apple Developer account
effort: S
blocked-by: human
filed-from: the owner's answer to the release binaries' signature, 2026-10-04 (T48 Q2)
opened: 2026-10-04
decided:
done:
---

## Current state

The release job `release-mcp` in [`build.yml`](../../.github/workflows/build.yml) builds
`cowork-mcp` for darwin/amd64 and darwin/arm64 and attaches each binary with its `.sha256` and a
build provenance attestation to the GitHub release
([ADR 0041](../adr/0041-the-mcp-server-speaks-stdio-and-ships-as-a-release-binary-per-platform.md)
D2). The attestation proves which workflow run built the binary, but only to a person who runs
`gh attestation verify`. macOS knows none of it: a downloaded binary carries the quarantine
attribute, and Gatekeeper refuses to start an unsigned, unnotarised one until the person removes the
attribute (`xattr -d com.apple.quarantine`) or allows it in the system settings — which teaches the
habit of waving unknown binaries through.

## Required changes

1. An Apple Developer account (the owner's), a Developer ID Application certificate and an app
   specific password or App Store Connect API key, stored as organisation secrets.
2. The `release-mcp` job signs the darwin binaries with `codesign` (hardened runtime) and submits
   them with `xcrun notarytool` — which needs a macOS runner; the self-hosted runners are Linux, so
   either a hosted `macos-latest` job for the two darwin targets or `rcodesign` on Linux.
3. [claude-code.md](../operations/claude-code.md) drops the quarantine step from the installation;
   [agent-client.md](../security/agent-client.md) says what the signature adds.
4. Verified on a release: a downloaded darwin binary starts without a Gatekeeper prompt, and
   `spctl --assess --type execute` accepts it.

## Related

- T48 — the decisions of phase 5; its Q2 chose the provenance attestation and left this for later
