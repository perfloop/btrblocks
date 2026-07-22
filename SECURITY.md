# Security policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately through
[GitHub Security Advisories](https://github.com/axiomhq/btrblocks/security/advisories/new).
Do not open a public issue before a fix and disclosure plan are available.

Include the affected version or commit, the smallest reproducer available,
the expected impact, and whether untrusted encoded bytes are required.

## Supported versions

Until the first tagged release, only the current default branch is supported.
After release, the latest minor release receives security fixes. Data written
by released version 1 writers remains readable according to `FORMAT.md`, but
callers should upgrade the library to receive decoder and dependency fixes.

## Untrusted data

Use explicit `ReadOptions` for persisted or network-provided data. Keep
`MaxLength`, `MaxBytes`, `MaxDecodedBytes`, `MaxDepth`, and `MaxWork` within the
owning service's memory and latency budgets. `MaxWork` bounds the total
validation work one decode may perform (`MaxDecodedBytes` bounds a single scan,
`MaxWork` their sum), which is what limits the CPU a small crafted stream can
demand. The zero value applies conservative defaults, but application-specific
limits are preferred.
