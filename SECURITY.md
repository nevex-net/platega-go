# Security policy

This library handles merchant credentials and payment callbacks, so security
reports are taken seriously.

## Reporting a vulnerability

**Please do not open a public issue.** Use GitHub's private reporting instead:
the repository's **Security** tab → **Report a vulnerability**.

Include what you found, how to reproduce it and which version is affected. You
can expect an acknowledgement within a few days. Fixes ship as a patch release
with a note in the [changelog](CHANGELOG.md), and you will be credited unless
you prefer otherwise.

## Supported versions

Only the latest released version receives fixes.

## Scope

In scope: bugs in this library, for example weak callback authentication,
credential or signature leakage in errors or logs, request-signing mistakes, or
unsafe handling of API responses.

Out of scope: vulnerabilities in the Platega service itself (report those to
Platega) and in your own application code.

## Handling credentials

- Never put a merchant secret, payout secret or real card number in an issue,
  pull request, test fixture or log output.
- Redact `X-MerchantId`, `X-Secret` and `Authorization` values in anything you
  paste.
- If a secret was exposed, rotate it in the Platega dashboard first.
