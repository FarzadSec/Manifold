# Developing Manifold

The rules an agent or a developer must follow while changing code are in [`CLAUDE.md`](../../CLAUDE.md) and [`frontend/CLAUDE.md`](../../frontend/CLAUDE.md). This page covers setup, verification, branches and upstream merges. The plan each phase is checked against is [`ARCHITECTURE.md`](../ARCHITECTURE.md).

## Layout

| Path | Holds |
|---|---|
| `main.go` | Entry point and command line |
| `internal/` | The panel: database, web server, subscription server, protocol engines, jobs |
| `frontend/` | The React interface, built into `internal/web/dist/` and embedded in the binary |
| `tools/openapigen/` | Generates frontend types and the OpenAPI description from Go structs |
| `manifold/` | Manifold's own documents |

## Toolchain

- Go, at the version in `go.mod`
- Node.js, at the version in `.nvmrc`
- A C compiler: the SQLite driver is built with cgo
- `golangci-lint` for `make lint`
- Chromium for the frontend test suite: `npx playwright install --with-deps chromium`, once

## Commands

| Command | Does |
|---|---|
| `make help` | List every target |
| `make dist-stub` | Create the empty `internal/web/dist/` a fresh clone needs before any Go build |
| `make test-go` | Go tests |
| `make test-fe` | Frontend tests |
| `make lint` | Go and frontend linters |
| `make gen` | Regenerate frontend types and the OpenAPI description after changing Go structs or routes |
| `make build` | Build the interface, then compile every Go package |
| `make verify` | The gate: generated files fresh, linters, formatter, type check, both test suites, build, Storybook |

A change is done when `make verify` passes. Changes to the database layer, node sync or parsers also need `make race` and `make node-e2e`.

## Where verification runs

`.github/workflows/ci.yml` runs the same gate, plus the race detector, a PostgreSQL job and the two-panel node test, on pull requests and on pushes to `main`. GitHub Actions is switched off in this repository until the workflows are trimmed for Manifold; until then, run the gate on a machine that has the toolchain.

## Branches

| Branch | Role |
|---|---|
| `main` | The product |
| `upstream-main` | The code base Manifold started from, untouched. It only ever fast-forwards |
| `p0/foundation`, `p1/wireguard`, … | One branch per phase; work lands on `main` from here |

Commits follow `type(area): summary`, with a body that says why. Types in use: `fix`, `feat`, `chore`, `refactor`, `perf`, `docs`, `style`.

## Taking in upstream changes

```bash
git fetch upstream
git checkout upstream-main
git merge --ff-only upstream/main
git push origin upstream-main

git checkout -b merge/upstream-YYYY-MM-DD main
git merge upstream-main
```

Expect two kinds of conflict:

- **Files Manifold removed** come back as modify/delete conflicts whenever upstream edits them. Remove them again:

  ```bash
  git rm -r --ignore-unmatch docs media deploy/README.md deploy/cloud-init deploy/marketplace \
    .oss-scanner .github/ISSUE_TEMPLATE .github/claude .github/FUNDING.yml \
    .github/pull_request_template.md .github/workflows/claude-issue-analyst.yml \
    .github/workflows/claude-pr-review.yml .github/workflows/docs-ci.yml \
    .github/workflows/docs-deploy.yml CONTRIBUTING.md SECURITY.md REVIEW.md \
    bot_context_test.go README.*_*.md
  ```

- **Files Manifold rewrote** (`README.md`, `CLAUDE.md`, `Makefile`, `docker-compose.yml`): keep Manifold's side, then carry over anything upstream changed that still applies.

Run `make verify` on the merge branch before it reaches `main`.

## Reading the base code's own documents

They are kept out of `main` but stay readable on `upstream-main`:

```bash
git show upstream-main:docs/architecture.md     # code map: request lifecycle, jobs, layering
git show upstream-main:CONTRIBUTING.md          # environment variables, test philosophy
```
