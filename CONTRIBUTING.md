# Contributing to g3

## Prerequisites

- **Go 1.26+** (version pinned in `go.mod`)
- **Docker** (for container builds)
- **Make** (all common tasks have Makefile targets)
- **golangci-lint v2.10+** (installed via `make tools`)

## Getting Started

```bash
git clone https://github.com/afreidah/g3.git
cd g3
make tools    # install golangci-lint, mockgen, govulncheck
make test     # run unit tests
make lint     # run linter
make build    # compile binary
```

## Running Locally

Requires a `config.yaml` in the project root with valid Gmail OAuth2 credentials. See the [README](README.md) for configuration format and the full Google Cloud setup process.

```bash
# Obtain a refresh token (one-time)
./g3 auth --client-id YOUR_ID --client-secret YOUR_SECRET

# Start the server
make run
```

## Development Workflow

1. Create an issue describing the change
2. Create a branch: `GH_ISSUE_<number>-<short-description>`
3. Bump the patch version in `.version`
4. Make changes, run `make check` (vet + lint + test)
5. Commit with [Conventional Commits](https://www.conventionalcommits.org/) format
6. Push and open a PR referencing the issue

## Code Style

Follow the conventions in [`style-guide.md`](style-guide.md). Key points:

- Every `.go` file starts with a 79-character box comment header
- Major sections use 73-character dividers in ALL CAPS
- ASCII-only characters (no Unicode dashes)
- Import groups: stdlib, internal, external (separated by blank lines)
- Context propagation through all function chains
- `slog.InfoContext(ctx, ...)` not `slog.Info(...)` for trace correlation
- Full godoc on every exported and unexported function/type/const

## Testing

- Test files live alongside the code: `foo_test.go`
- Table-driven tests with `TestFunctionName_Scenario` naming
- Standard `testing.T` assertions, no external assertion libraries
- Generated mocks via `mockgen` (run `make generate`)
- Untestable code (process entry points, handlers requiring a live backend) is excluded from the coverage metric via `sonar.coverage.exclusions` in `sonar-project.properties`, not inline annotations

## Commit Messages

```
feat: add new feature description
fix: correct bug in component
docs: update README with new section
refactor: simplify backend selection logic
test: add coverage for edge case
perf: optimize metadata-only reads
```

- Imperative mood ("add", not "added")
- Under 72 characters on the first line
- Reference the GitHub issue: `feat: add Drive hybrid (#30)`

## Pull Requests

- One issue per PR
- CI must pass (lint, test, govulncheck, build)
- Squash merge to keep history clean
- Branch naming: `GH_ISSUE_<number>-<short-description>`

## Reporting Issues

Include:

- g3 version (`g3 version`)
- Go version and platform
- Relevant config (redact secrets)
- Steps to reproduce
- Expected vs actual behavior
- Relevant log output (set `log_level: debug`)

## Project Structure

```
cmd/g3/              Entry point and subcommands (serve, auth, sync, validate, version)
internal/
  audit/              Request ID generation, context propagation, audit logging
  auth/               SigV4 signature verification, bucket registry
  backend/            ObjectBackend interface, Gmail/Drive hybrid backend, email
  config/             YAML config loading, validation, defaults
  store/              Metadata index (SQLite + PostgreSQL implementations, sqlc, migrations)
  server/             S3 HTTP routing, handlers, multipart uploads
  telemetry/          Prometheus metrics, OpenTelemetry tracing, log correlation
```
