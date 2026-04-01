# Changelog

All notable changes to this project are documented in this file.


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
