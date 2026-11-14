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

`.github/workflows/ci.yml` is the only gate that counts. What it runs depends on where the push lands:

| Push to | Jobs | Cost per run |
|---|---|---|
| A phase branch (`p0/…`, `p1/…`) or `merge/…` | Go tests, linters, generated-file check, shell lint, frontend | About 20 runner minutes |
| `main`, or any pull request | The above, plus the race detector, two PostgreSQL jobs, the two-panel node test, the fuzz smoke test and the vulnerability check | About 52 runner minutes |

A private repository has a monthly allowance of runner minutes, so push a phase branch when the work is ready to be checked, not after every edit.

## Building without the Go module proxy

Some workspaces can reach GitHub but not the Go module proxy. For those, `.github/workflows/vendor.yml` publishes the output of `go mod vendor` to the `build/vendor` branch every time `go.mod` or `go.sum` change:

```bash
git fetch --depth 1 origin build/vendor
git archive FETCH_HEAD vendor | tar -x
echo vendor/ >> .git/info/exclude
export GOFLAGS=-mod=vendor GOPROXY=off GOTOOLCHAIN=local
make dist-stub && go build ./... && make test-go
```

`vendor/` is a local build aid. It is never committed to a product branch.

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
