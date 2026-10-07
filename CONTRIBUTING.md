# Contributing

Thanks for helping out. This is a small library, so the process is light.

## Setup

Go 1.22 or newer. There are no third-party dependencies, and new ones need a
very good reason.

```sh
go vet ./...
go test -race ./...
golangci-lint run        # v2, config in .golangci.yml
```

Tests run against `httptest` servers and need no network or credentials.

## Pull requests

1. Open an issue first for anything beyond a small fix.
2. Add or update tests. Every endpoint is tested against a fake server that
   checks the method, path, headers and body that actually go on the wire.
3. Keep `gofmt` and the linter clean, and update the README when behaviour
   changes. Do not edit [CHANGELOG.md](CHANGELOG.md): it is generated.
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
   messages and for the **PR title** (a check enforces it; with squash merging
   the title becomes the commit message), for example `feat: add transaction
   export` or `fix: escape ids in request paths`. Mark breaking changes with
   `!` (`feat!: ...`) or a `BREAKING CHANGE:` footer.

## Releases

Releases are automated with
[release-please](https://github.com/googleapis/release-please). Merged commits
on `main` accumulate in an open "release" pull request that bumps the version
and writes the changelog; merging that PR tags `vX.Y.Z` and publishes the
GitHub release. Until v1.0.0, `feat` bumps the minor version, `fix` the patch,
and a breaking change also the minor.

| Commit type | Effect | In the changelog |
| --- | --- | --- |
| `feat` | minor bump | Features |
| `fix` | patch bump | Bug Fixes |
| `perf`, `revert` | patch bump | listed |
| `docs`, `test`, `ci`, `build`, `chore`, `refactor`, `style` | none by itself | hidden |

## Platega API behaviour

Platega's documentation is incomplete in places (see "Known gaps" in the
README). When a change depends on how the API really behaves:

- link the documentation page, or paste a **redacted** real response in the PR;
- never include real credentials, card numbers or payer data in fixtures;
- if you could not verify it against the live API, say so, and add it to
  "Known gaps".

## Design notes

- One package at the module root, standard library only.
- Every network call takes a `context.Context` first.
- State-changing requests are never retried automatically.
- Names follow Go conventions; where the API misspells a field (`mechantId`,
  `comission`), the Go field is spelled correctly and the JSON tag keeps the
  API's spelling, with a comment.

## Security issues

Do not open a public issue. See [SECURITY.md](SECURITY.md).
