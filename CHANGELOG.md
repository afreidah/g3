# Changelog

All notable changes to this project are documented in this file.


## [0.5.3] - 2026-06-30

### Added
- add extractBodyText tests and update docs for resumable uploads
- add ParseMetadataForSync tests
- add g3 sync command to rebuild SQLite index from Gmail (#33)
- add SQLite metadata index for zero-API-call reads (#30)

### Fixed
- fix(web): non-root nginx and lock-enforced docs tool install
- fix(backend): serve ListObjects from metadata index (#50)

### Refactored
- refactor(server): own the backend contract as a consumer interface
- refactor(backend): cut listFromGmail cognitive complexity

### Improved
- update all documentation for PostgreSQL store support
- update all documentation for sync command and current architecture
- update all documentation for Drive hybrid and SQLite index
- update CHANGELOG.md for v0.2.13 (#26)

### Other
- address SonarCloud findings on the refactor PR
- consumer-defined auth interface and thin cmd wrappers
- ci(release): pin third-party actions to commit SHAs
- cover new extracted helpers for the new-code quality gate
- replace Codecov with SonarCloud, fix govulncheck failure
- build: bump to Go 1.26.3 and golang.org/x/net v0.55.0 for CVE fixes
- build: overhaul publish-deb to upload, snapshot, and publish via Aptly
- correct Prometheus metrics inventory (11 -> 27)
- build(deps): bump github.com/jackc/pgx/v5
- use resumable Drive uploads to prevent EOF on large files (#46)
- stream PutObject and multipart assembly to eliminate OOM kills (#44)
- build(deps): bump go.opentelemetry.io/otel/sdk
- use GMT instead of UTC in Last-Modified header (#41)
- exclude postgres/sqlc from codecov, add database config tests
- support PostgreSQL as metadata store backend (#38)
- overhaul observability with proper spans, metrics, and correlation (#35)
- exclude SQLite WAL files from git
- use Messages.Trash instead of Messages.Delete for gmail.modify scope
- put demo sqlite db file in ignore list
- exclude Gmail/Drive API files and CLI entrypoints from codecov
- remove dead code and add codecov:ignore to all Gmail/Drive API methods
- Google Drive hybrid storage backend (#30)
- handle errcheck warnings in store and main
- resolve lint errors and exclude mock from codecov
- comprehensive test coverage with gomock (#28)
- replace copied theme with git submodule

## [0.2.11] - 2026-04-01

### Other
- don't double-prefix v in release tag
- commit changelog.gz and use lint action in release preflight

## [0.2.11] - 2026-04-01

### Other
- golangci-lint update

## [0.2.10] - 2026-04-01

### Added
- add release workflow, changelog, and Debian packaging (#25)
- add project website, LICENSE, and README badges (#23)
- add codecov:ignore to handler functions requiring live backend
- add auth and SigV4 unit tests for coverage
- add multipart store unit tests
- add codecov:ignore annotations for untestable paths
- add unit tests, CI pipeline, and contributing guide (#6)
- add chunked storage for large objects (#5)
- add SigV4 auth, server routing, and helpers (#2)
- add style guide adapted for g3

### Other
- exclude web build artifacts from git
- ensure ListObjects correctly handles chunked objects (#17)
- comprehensive README and CONTRIBUTING rewrite
- use bytes.Equal instead of string comparison per gocritic
- S3 compatibility issues from code review (#20)
- implement multipart upload support (#16)
- upgrade golangci-lint-action to v7 for lint v2 support
- handle errcheck warnings in auth callback handler
- store metadata as email body text, use format=full for HeadObject
- rewrite auth to use authorization code flow (#14)
- config-driven OAuth2 with device code auth flow (#12)
- resolve golangci-lint errors
- wire S3 handlers to Gmail backend (#4)
- complete core scaffold with build tooling (#1)
- initial scaffold for g3 - S3-compatible gateway backed by Gmail
