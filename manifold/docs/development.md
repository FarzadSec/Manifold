# Developing Manifold

The rules an agent or a developer must follow while changing code are in [`CLAUDE.md`](../../CLAUDE.md) and [`frontend/CLAUDE.md`](../../frontend/CLAUDE.md). This page covers setup, verification, branches and upstream merges. The plan each phase is checked against is [`ARCHITECTURE.md`](../ARCHITECTURE.md).

## Layout

| Path | Holds |
|---|---|
| `main.go` | Entry point and the program's own command line |
| `install.sh`, `manifold.sh`, `manifold.service` | Installer, management command and systemd unit |
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

`.github/workflows/ci.yml` is the gate. What it runs depends on where the push lands:

| Push to | Jobs | Cost per run |
|---|---|---|
| A phase branch (`p0/…`, `p1/…`) or `merge/…` | Go tests, linters, generated-file check, shell lint, frontend | About 20 runner minutes |
| `main`, or any pull request | The above, plus the race detector, two PostgreSQL jobs, the two-panel node test, the fuzz smoke test and the vulnerability check | About 52 runner minutes |

Standard runners are free for a public repository. The two tiers still keep a phase branch's feedback fast; if the repository is made private again, its monthly allowance of runner minutes is the reason not to push after every edit.

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
    deploy/test/installer-panel-port.sh deploy/test/smoke-noninteractive.sh \
    .oss-scanner .github/ISSUE_TEMPLATE .github/claude .github/FUNDING.yml \
    .github/pull_request_template.md .github/dependabot.yml \
    .github/workflows/claude-issue-analyst.yml .github/workflows/claude-pr-review.yml \
    .github/workflows/docs-ci.yml .github/workflows/docs-deploy.yml \
    .github/workflows/docker.yml .github/workflows/codeql.yml \
    .github/workflows/mutation.yml .github/workflows/cleanup_caches.yml \
    CONTRIBUTING.md SECURITY.md REVIEW.md README.*_*.md bot_context_test.go \
    scripts_base_path_test.go update.sh x-ui.sh x-ui.rc x-ui.service.arch \
    x-ui.service.debian x-ui.service.rhel windows_files winres \
    internal/web/service/panel/sponsor.go internal/web/service/panel/sponsor_test.go \
    internal/web/service/panel/panel_unix.go internal/web/service/panel/panel_other.go \
    frontend/src/components/sponsor frontend/src/pages/sponsors \
    frontend/src/pages/index/PanelUpdateModal.tsx frontend/public/icons/3x-ui-*.png
  ```

- **Files Manifold rewrote** (`README.md`, `CLAUDE.md`, `Makefile`, `install.sh`, `Dockerfile`, `DockerEntrypoint.sh`, `docker-compose.yml`, `.env.example`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `internal/web/service/panel/panel.go`): keep Manifold's side, then carry over anything upstream changed that still applies.
- **Names** upstream adds in new code (`3x-ui`, `x-ui` in strings a person reads, in any locale file) are renamed to Manifold as part of the merge.

Run `make verify` on the merge branch before it reaches `main`.

## Reading the base code's own documents

They are kept out of `main` but stay readable on `upstream-main`:

```bash
git show upstream-main:docs/architecture.md     # code map: request lifecycle, jobs, layering
git show upstream-main:CONTRIBUTING.md          # environment variables, test philosophy
```

## Names and versions

- The program name is the content of `internal/config/name`. It names the database file.
- Operators set `MANIFOLD_<NAME>` environment variables. The settings read `XUI_<NAME>`, as upstream does; `config.AdoptManifoldEnv` copies one onto the other at start-up and after every env file load. New code keeps reading `XUI_<NAME>` through `internal/config`.
- `internal/config/product_version` is Manifold's version: the one the interface, the bots and `manifold -v` show, and the one release tags must equal.
- `internal/config/version` is the base version. A master decides which protocols a node can take from it, so it follows the code base and is not shown as the product's version.

## Releasing

Set the new version in `internal/config/product_version` and merge it to `main`. Nothing else is needed.

`.github/workflows/release.yml` then builds static amd64 and arm64 packages, installs the amd64 package on the runner with `install.sh`, checks that the panel answers, that the installer's login works and that an update keeps the settings. If all of that passes and no release named `v<product_version>` exists, it publishes one and creates its tag. A last job installs the published release on a clean runner with the command from the README.

Packaging changes that do not change the version run the same build and install and publish nothing. A `vX.Y.Z` tag pushed by hand publishes that version too, as long as it equals `product_version`.
