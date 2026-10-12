# Security policy

## Reporting a vulnerability

Please **do not** open a public issue for a security problem. Use GitHub's private reporting:
**Security → Report a vulnerability** on this repository. Describe what you found, how to reproduce it and
what you think the impact is. You will get an answer within a few days; a fix is released and credited (if you
wish) before the details are made public.

## What is in scope

Vinaire sits in front of Kubernetes clusters, so anything that lets someone do more than their own Kubernetes
RBAC allows is in scope — a bypass of the sign-in, a way to read a Secret or a stored credential without the
audited *reveal*, a request that makes Vinaire reach an address it must not (SSRF), cross-site scripting or
request forgery, a flaw in how credentials are stored or logged, an unsigned or tampered release artifact.

The threat model, what has been verified and how to verify it again, and the risks that remain are in
[docs/security.md](./docs/security.md).

## Supported versions

The latest release receives fixes. Release artifacts are signed; see the README for how to verify them.
