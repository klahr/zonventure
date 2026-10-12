# Security policy

## Reporting a vulnerability

Please report security issues privately — don't open a public issue
or pull request for them.

- Email `hello@zonventure.com` with `[security]` in the subject, or
- use GitHub's **Report a vulnerability** button on the repository's
  Security tab.

Include what you found, how to reproduce it, and the version (shown on
Settings → Software update, or at `/api/version`) or commit you tested.
We aim to acknowledge reports within a few working days and will keep
you updated until a fix ships. We're happy to credit you in the
changelog unless you'd rather stay anonymous.

## Supported versions

Fixes land on `main` and in the next release. Only the latest release
is supported; please upgrade before reporting.

## Scope

In scope: this repository — the `zv` server, its embedded web apps,
and the Docker Compose setup.

Out of scope: issues that need an already-compromised operator account
or host, denial of service by sheer request volume, and
misconfigurations that the docs warn against (for example running with
the default `admin` / `admin` credentials, or `ZV_ENV=dev` in production).

Please test against your own instance, not against zonventure.com or
anyone else's deployment.
