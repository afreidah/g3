<p align="center">
  <img src="docs/logo.png" alt="g3 logo" width="400">
</p>

# g3

[![CI](https://github.com/afreidah/g3/actions/workflows/ci.yml/badge.svg)](https://github.com/afreidah/g3/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/afreidah/g3/graph/badge.svg)](https://codecov.io/gh/afreidah/g3)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An S3-compatible HTTP gateway that uses Gmail and Google Drive as the storage backend.

Object data is stored in Google Drive files. Gmail emails serve as metadata pointers with JSON in the body containing the Drive file ID, ETag, size, and user metadata. A local SQLite index eliminates API calls for metadata-only operations. Buckets map to Gmail labels. Designed for write-once/read-rarely workloads like offsite backups, where Google's 15 GB of free storage becomes a durable, API-accessible backup target.

## How It Works

| S3 Concept | Google Mapping |
|---|---|
| Bucket | Gmail label (`s3/bucket-name`) |
| Object data | Google Drive file |
| Object metadata | Gmail email body (JSON with Drive file ID) |
| Object key | Email subject (`s3://bucket/path/to/key`) |
| ETag | MD5 hex digest of content |
| Metadata index | Local SQLite database |

## S3 API Coverage

| Operation | Supported | Notes |
|---|---|---|
| PutObject | Yes | Uploads to Drive, inserts metadata email in Gmail |
| GetObject | Yes | Downloads from Drive using cached file ID |
| HeadObject | Yes | Local SQLite lookup, zero API calls |
| DeleteObject | Yes | Removes Drive file, Gmail email, and index record |
| ListObjectsV2 | Yes | Local SQLite query, zero API calls |
| ListBuckets | Yes | Lists all labels under the configured prefix |
| CreateBucket | Yes | Creates a Gmail label |
| HeadBucket | Yes | Checks bucket existence |
| GetBucketLocation | Yes | Returns empty constraint (us-east-1) |
| CreateMultipartUpload | Yes | In-memory part buffering |
| UploadPart | Yes | Parts 1-10000, max 100 concurrent uploads |
| CompleteMultipartUpload | Yes | Assembles and delegates to PutObject |
| AbortMultipartUpload | Yes | Discards buffered parts |

## Features

- **Drive + Gmail hybrid storage** -- object data in Drive (no size limit), metadata in Gmail emails
- **Local SQLite index** -- HeadObject and ListObjects resolve locally with zero API calls
- **S3-compatible API** -- works with the AWS CLI, s3cmd, any S3 SDK
- **Multipart upload** -- large files via standard S3 multipart protocol
- **Dual API quota pools** -- Drive (12,000 req/min) and Gmail (250 units/sec) operate independently
- **SigV4 authentication** -- standard AWS Signature Version 4 request signing
- **Prometheus metrics** -- request counts, latency, Gmail/Drive API metrics (`g3_` prefix)
- **OpenTelemetry tracing** -- distributed traces with OTLP gRPC export
- **Log/span correlation** -- trace_id and span_id injected into structured JSON logs
- **Audit logging** -- security-relevant operations logged with request ID correlation
- **YAML configuration** -- environment variable expansion (`${VAR}` syntax)
- **Graceful shutdown** -- clean drain on SIGINT/SIGTERM
- **Health checks** -- `/health` (liveness) and `/health/ready` (readiness)

## Prerequisites

Each user needs a Google Cloud project with the Gmail and Drive APIs enabled (free, no billing required):

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a project (or use an existing one)
3. Navigate to **APIs & Services > Library** and enable both **Gmail API** and **Google Drive API**
4. Navigate to **APIs & Services > Credentials**
5. Click **Create Credentials > OAuth client ID**
6. Select application type **Desktop app**, name it (e.g., "g3")
7. Copy the **client ID** and **client secret**
8. Navigate to **OAuth consent screen**, set to **External**, and add your email as a **test user**

## Getting Started

```bash
# Clone and build
git clone https://github.com/afreidah/g3.git
cd g3
make build

# Obtain a refresh token (one-time setup)
./g3 auth --client-id YOUR_CLIENT_ID --client-secret YOUR_CLIENT_SECRET
# A browser window opens for Google authorization
# After approval, the refresh token is printed to stdout

# Create config.yaml (see Configuration section below)
# Then start the server
./g3 -config config.yaml
```

## Configuration

```yaml
server:
  listen_addr: "0.0.0.0:9000"       # Listen address (default: 0.0.0.0:9000)
  log_level: "info"                  # debug, info, warn, error (default: info)
  read_timeout: "5m"                 # HTTP read timeout (default: 5m)
  write_timeout: "5m"                # HTTP write timeout (default: 5m)
  shutdown_timeout: "30s"            # Graceful shutdown deadline (default: 30s)

gmail:
  client_id: "${GMAIL_CLIENT_ID}"          # Google OAuth2 client ID (required)
  client_secret: "${GMAIL_CLIENT_SECRET}"  # Google OAuth2 client secret (required)
  refresh_token: "${GMAIL_REFRESH_TOKEN}"  # OAuth2 refresh token from g3 auth (required)
  user: "me"                               # Gmail user (default: me)
  label_prefix: "s3"                       # Gmail label prefix for buckets (default: s3)

database:
  path: "/data/g3/metadata.db"             # SQLite metadata index path (default: g3-metadata.db)

buckets:
  - name: "backups"                        # Bucket name (maps to Gmail label s3/backups)
    credentials:
      - access_key_id: "mykey"             # S3 access key for this bucket
        secret_access_key: "mysecret"      # S3 secret key for this bucket

telemetry:
  metrics:
    enabled: true                          # Enable Prometheus endpoint (default: false)
    path: "/metrics"                       # Metrics path (default: /metrics)
  tracing:
    enabled: false                         # Enable OpenTelemetry tracing (default: false)
    endpoint: "tempo:4317"                 # OTLP gRPC endpoint
    insecure: true                         # Use insecure gRPC connection
    sample_rate: 1.0                       # Trace sampling rate 0.0-1.0
```

All string values support `${ENV_VAR}` expansion, making it easy to inject secrets from Vault, Nomad templates, or environment variables.

## Usage

### Basic operations with the AWS CLI

```bash
# Set credentials (or use ~/.aws/credentials)
export AWS_ACCESS_KEY_ID=mykey
export AWS_SECRET_ACCESS_KEY=mysecret
export AWS_ENDPOINT_URL=http://localhost:9000

# Create a bucket (creates Gmail label s3/backups)
aws s3 mb s3://backups

# Upload a file
aws s3 cp backup.tar.gz s3://backups/daily/backup.tar.gz

# List objects
aws s3 ls s3://backups/daily/

# Download a file
aws s3 cp s3://backups/daily/backup.tar.gz ./restored.tar.gz

# Delete a file
aws s3 rm s3://backups/daily/backup.tar.gz
```

### As an s3-orchestrator backend

g3 can be added as a backend in [s3-orchestrator](https://github.com/afreidah/s3-orchestrator) alongside other S3-compatible providers:

```yaml
backends:
  - name: "gmail"
    endpoint: "http://g3.service.consul:9000"
    region: "us-east-1"
    bucket: "backups"
    access_key_id: "mykey"
    secret_access_key: "mysecret"
    force_path_style: true
    quota_bytes: 15000000000    # 15 GB Google storage limit
```

## CLI Subcommands

| Command | Description |
|---|---|
| `g3` or `g3 serve` | Start the S3 gateway server |
| `g3 auth` | Obtain a refresh token via OAuth2 browser flow |
| `g3 validate` | Validate a config file without starting the server |
| `g3 version` | Print version and Go runtime information |
| `g3 help` | Show available commands |

### g3 auth

```bash
g3 auth --client-id <id> --client-secret <secret> [--port <port>]
```

Opens a browser for Google OAuth2 authorization requesting `gmail.modify` and `drive.file` scopes. After approval, prints the refresh token to stdout. The `--port` flag sets the localhost callback port (default: auto-assigned).

### g3 validate

```bash
g3 validate -config /path/to/config.yaml
```

Parses and validates the configuration file, checking all required fields and defaults. Exits 0 on success, 1 on failure with error details.

## Architecture

```
              S3 Clients (aws cli, s3-orchestrator, SDKs)
                             |
                        [SigV4 Auth]
                             |
                   g3 S3 HTTP Server
                   /         |        \
            PutObject   GetObject   ListObjects ...
                   |         |           |
              [SQLite Metadata Index]    |
              /         |         \      |
     Drive Upload   Drive Download   Local Query
         |              |
    Gmail Insert    (data from Drive)
    (metadata email)
         |
   Google Drive (object data)  +  Gmail (metadata emails)
```

### Storage model

- **Object data** is stored as Google Drive files in a root folder (`s3/` by default). No size limit -- Drive supports up to 5TB per file.
- **Object metadata** is stored as Gmail emails with JSON in the body containing the Drive file ID, content type, ETag, size, and user metadata. No attachment.
- **Local SQLite index** maps bucket/key to Gmail message ID, Drive file ID, and metadata. HeadObject and ListObjects resolve entirely from the index with zero API calls. GetObject and DeleteObject use the cached IDs to skip Gmail search.
- **Buckets** map to Gmail labels under the configured prefix (e.g., `s3/backups`).

### Data flow

**Write path (PutObject):**
1. Upload object data to Google Drive
2. Insert metadata-only email in Gmail with Drive file ID
3. Record metadata in local SQLite index

**Read path (GetObject):**
1. Look up Drive file ID from SQLite index (or Gmail email on cache miss)
2. Download object data from Google Drive

**Metadata path (HeadObject, ListObjects):**
1. Query local SQLite index -- zero API calls

### Multipart uploads

S3 multipart uploads are buffered in memory and assembled on `CompleteMultipartUpload`. The assembled object is then written through the normal PutObject path (Drive upload + Gmail metadata). Abandoned uploads are cleaned up after 1 hour.

Limits: 100 concurrent uploads, part numbers 1-10000.

## Observability

### Prometheus Metrics

Available at `/metrics` when `telemetry.metrics.enabled` is true.

| Metric | Type | Labels |
|---|---|---|
| `g3_requests_total` | Counter | method, status_code |
| `g3_request_duration_seconds` | Histogram | method |
| `g3_request_size_bytes` | Histogram | method |
| `g3_response_size_bytes` | Histogram | method |
| `g3_inflight_requests` | Gauge | method |
| `g3_gmail_api_requests_total` | Counter | operation, status |
| `g3_gmail_api_duration_seconds` | Histogram | operation |
| `g3_gmail_storage_bytes` | Gauge | -- |
| `g3_objects_total` | Gauge | bucket |
| `g3_audit_events_total` | Counter | event |
| `g3_build_info` | Gauge | version, go_version |

### Tracing

When `telemetry.tracing.enabled` is true, g3 exports traces via OTLP gRPC. Each S3 request produces a server span, and each Gmail/Drive API call produces a child client span. Custom attributes are prefixed with `g3.` (e.g., `g3.bucket`, `g3.key`, `g3.gmail.message_id`).

Trace IDs and span IDs are automatically injected into JSON log output for correlation in tools like Grafana Loki + Tempo.

### Health Checks

| Endpoint | Behavior |
|---|---|
| `GET /health` | Always returns 200 `{"status":"ok"}` |
| `GET /health/ready` | Returns 200 after startup, 503 during initialization or shutdown |

## Limitations

- **Google storage quota**: 15 GB shared across Gmail, Drive, and Photos. Objects count against this limit.
- **API rate limits**: Drive allows 12,000 requests/user/minute. Gmail allows 250 quota units/second. Sufficient for backup workloads.
- **Eventual consistency**: Gmail search indexing has a small delay. Objects not yet in the SQLite index may take a few seconds to appear via Gmail search fallback.
- **Memory usage**: Multipart uploads and PutObject buffer the full object in memory during Drive upload.
- **SQLite persistence**: The metadata index must be on a persistent volume. If lost, a `g3 sync` command can rebuild it from Gmail (not yet implemented).

## Project Structure

```
cmd/g3/              Entry point and subcommands (serve, auth, validate, version)
internal/
  audit/              Request ID generation, context propagation, audit logging
  auth/               SigV4 signature verification, bucket registry
  backend/
    types.go          ObjectBackend interface, MetadataStore interface, result types
    gmail.go          Gmail + Drive hybrid backend (PutObject, GetObject, HeadObject, DeleteObject)
    gmail_list.go     ListObjects, ListBuckets, CreateBucket
    gmail_chunked.go  Legacy chunked object support (read-only for old data)
    email.go          MIME email construction and parsing
    search.go         Gmail search query builder
  config/             YAML config loading, validation, defaults
  store/              SQLite metadata index implementation
  server/
    server.go         HTTP routing, auth, spans, audit logging
    objects.go        PUT, GET, HEAD, DELETE handlers
    list.go           ListObjectsV2 handler
    buckets.go        ListBuckets, CreateBucket, HeadBucket, GetBucketLocation
    multipart.go      Multipart upload store and handlers
    helpers.go        S3 XML responses, path parsing, metadata extraction
  telemetry/
    metrics.go        Prometheus metric definitions
    tracing.go        OpenTelemetry initialization, span helpers
    tracehandler.go   slog handler for trace/span ID injection
    logbuffer.go      Circular log buffer for operational visibility
```

## License

MIT
