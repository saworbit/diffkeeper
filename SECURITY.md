# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| v2.x    | ✅ Full support    |
| v1.x    | ⚠️ Critical fixes |

## Reporting a Vulnerability

1. Email `security@diffkeeper.dev` (or `shaneawall@gmail.com`) with:
   - Description of the issue
   - Steps to reproduce / proof of concept
   - Impact assessment
2. Encrypt reports with the PGP key published in `docs/patents.md` (optional but appreciated).
3. You will receive an acknowledgement within 72 hours.

Please **do not** open public GitHub issues for security bugs until a fix is
released. Coordinated disclosure timelines are handled case-by-case, but we aim
to ship patches within 14 days for high severity reports.

## After Disclosure

- You will receive a CVE identifier if applicable.
- Fixes are published on the `security/*` branches and merged through reviewed PRs.
- Release notes highlight mitigations, configuration flags, and rollout guidance.
