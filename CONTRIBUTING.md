# Contributing to g3

Thanks for your interest in contributing. This document covers everything you need to get started.

## Prerequisites

- **Go 1.26+** (version pinned in `go.mod`)
- **Docker** (for container builds)
- **Make** (all common tasks have Makefile targets)

## Getting Started

```bash
# Clone the repository
git clone https://github.com/afreidah/g3.git
cd g3

# Install build dependencies
make tools

# Run unit tests
make test

# Run linter
make lint

# Build the binary
make build
```

## Testing

### Unit tests

```bash
make test
```

### Running locally

Requires a `config.yaml` in the project root with valid Gmail OAuth2 credentials. See `README.md` for configuration format.

```bash
make run
```

## Code Style

Follow the conventions in [`style-guide.md`](style-guide.md). Key points:

- All source files start with a 79-character box comment header
- Section dividers use 73-character dashes
- ASCII-only characters (no Unicode dashes or special characters)
- Context propagation through all function chains
- Import groups: stdlib, internal, external (separated by blank lines)

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat: add new feature description
fix: correct bug in component
docs: update admin guide with new section
refactor: simplify backend selection logic
test: add coverage for edge case
```

- Use imperative mood ("add", not "added")
- Keep the first line under 72 characters
- Reference the GitHub issue number: `feat: add chunking support (#5)`

## Pull Requests

- **One issue per PR** - keep changes focused
- **CI must pass** - linting, unit tests, and build
- **Squash merge** - PRs are squash-merged to keep history clean
- Create a branch named `GH_ISSUE_<number>-<short-description>`

## Reporting Issues

When filing a bug report, include:

- g3 version (`g3 version`)
- Go version and platform
- Relevant config (redact secrets)
- Steps to reproduce
- Expected vs actual behavior
- Relevant log output

## Project Structure

```
cmd/g3/                Entry point and subcommands
internal/
  audit/               Request ID and audit logging
  auth/                SigV4 and bucket registry
  backend/             ObjectBackend interface, Gmail implementation
  config/              YAML config loading and validation
  server/              S3 HTTP handlers
  telemetry/           Prometheus metrics, OpenTelemetry tracing
```
