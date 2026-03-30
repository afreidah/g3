<p align="center">
  <img src="docs/logo.png" alt="g3 logo" width="400">
</p>

# g3

An S3-compatible HTTP gateway that uses Gmail as the storage backend.

Objects are stored as emails -- metadata in the body, data as attachments. Buckets map to Gmail labels. Designed for write-once/read-rarely workloads like offsite backups, where Gmail's 15 GB of free storage becomes a durable, API-accessible backup target.

## How It Works

| S3 Concept | Gmail Mapping |
|---|---|
| Bucket | Gmail label (`s3/bucket-name`) |
| Object | Email with attachment |
| Object key | Email subject (`s3://bucket/path/to/key`) |
| Object metadata | JSON in email body (visible in Gmail UI) |
| ETag | MD5 hex digest of content |
| Large objects (>20 MB) | Chunked across multiple emails with a manifest |

## S3 API Coverage

| Operation | Supported | Notes |
|---|---|---|
| PutObject | Yes | Single email or chunked for large objects |
| GetObject | Yes | Reassembles chunked objects transparently |
| HeadObject | Yes | Fast metadata-only read via Gmail format=full |
| DeleteObject | Yes | Deletes manifest and all chunks |
| ListObjectsV2 | Yes | Prefix, delimiter, pagination, ETags |
| ListBuckets | Yes | Lists all labels under the configured prefix |
| CreateBucket | Yes | Creates a Gmail label |
| HeadBucket | Yes | Checks bucket existence |
| GetBucketLocation | Yes | Returns empty constraint (us-east-1) |
| CreateMultipartUpload | Yes | In-memory part buffering |
| UploadPart | Yes | Parts 1-10000, max 100 concurrent uploads |
| CompleteMultipartUpload | Yes | Assembles and delegates to PutObject |
| AbortMultipartUpload | Yes | Discards buffered parts |

## Features

- **S3-compatible API** -- works with the AWS CLI, s3cmd, any S3 SDK
- **Multipart upload** -- large files via standard S3 multipart protocol
- **Chunked storage** -- objects exceeding Gmail's 25 MB attachment limit are split across emails
- **SigV4 authentication** -- standard AWS Signature Version 4 request signing
- **Prometheus metrics** -- request counts, latency, Gmail API metrics (`g3_` prefix)
- **OpenTelemetry tracing** -- distributed traces with OTLP gRPC export
- **Log/span correlation** -- trace_id and span_id injected into structured JSON logs
- **Audit logging** -- security-relevant operations logged with request ID correlation
- **YAML configuration** -- environment variable expansion (`${VAR}` syntax)
- **Graceful shutdown** -- clean drain on SIGINT/SIGTERM
- **Health checks** -- `/health` (liveness) and `/health/ready` (readiness)

## Prerequisites

Each user needs a Google Cloud project with the Gmail API enabled (free, no billing required):

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a project (or use an existing one)
3. Navigate to **APIs & Services > Library**, search for **Gmail API**, and enable it
4. Navigate to **APIs & Services > Credentials**
5. Click **Create Credentials > OAuth client ID**
6. Select application type **Desktop app**, name it (e.g., "g3")
7. Copy the **client ID** and **client secret**
8. Navigate to **OAuth consent screen**, set to **External**, and add your email as a **test user**

> **Note:** In Testing mode, refresh tokens expire after 7 days. You will need to re-run `g3 auth` weekly. Publishing the app removes this limit but requires Google's verification review for Gmail scopes.

## Getting Started

```bash
# Clone and build
git clone https://github.com/afreidah/g3.git
cd g3
make build

# Obtain a Gmail refresh token (one-time setup)
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
  max_attachment_bytes: 25000000           # Gmail attachment limit (default: 25 MB)
  chunk_size_bytes: 20000000               # Chunk boundary for large objects (default: 20 MB)
  label_prefix: "s3"                       # Gmail label prefix for buckets (default: s3)

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
    quota_bytes: 15000000000    # 15 GB Gmail storage limit
```

## CLI Subcommands

| Command | Description |
|---|---|
| `g3` or `g3 serve` | Start the S3 gateway server |
| `g3 auth` | Obtain a Gmail refresh token via OAuth2 browser flow |
| `g3 validate` | Validate a config file without starting the server |
| `g3 version` | Print version and Go runtime information |
| `g3 help` | Show available commands |

### g3 auth

```bash
g3 auth --client-id <id> --client-secret <secret> [--port <port>]
```

Opens a browser for Google OAuth2 authorization. After approval, prints the refresh token to stdout. The `--port` flag sets the localhost callback port (default: auto-assigned).

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
                    \         |        /
                   Gmail Backend (ObjectBackend)
                    /         |        \
             Send Email   Get Email   Search Emails
                              |
                        Gmail API
                              |
                     Gmail (15 GB free)
```

### Storage model

- **Single objects** (< chunk_size_bytes): one email per object. Subject is the key, body is JSON metadata, attachment is the data.
- **Chunked objects** (>= chunk_size_bytes): data split across chunk emails (`key#chunk-001`, `key#chunk-002`, ...) plus a manifest email at the original key containing chunk count and total size.
- **Metadata-only reads**: HeadObject and ListObjects use Gmail's `format=full` API parameter to read the email body without downloading attachments, avoiding the transfer cost of large objects.

### Multipart uploads

S3 multipart uploads are buffered in memory and assembled on `CompleteMultipartUpload`. The assembled object is then written through the normal PutObject path, which handles chunking automatically. Abandoned uploads are cleaned up after 1 hour.

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

When `telemetry.tracing.enabled` is true, g3 exports traces via OTLP gRPC. Each S3 request produces a server span, and each Gmail API call produces a child client span. Custom attributes are prefixed with `g3.` (e.g., `g3.bucket`, `g3.key`, `g3.gmail.message_id`).

Trace IDs and span IDs are automatically injected into JSON log output for correlation in tools like Grafana Loki + Tempo.

### Health Checks

| Endpoint | Behavior |
|---|---|
| `GET /health` | Always returns 200 `{"status":"ok"}` |
| `GET /health/ready` | Returns 200 after startup, 503 during initialization or shutdown |

## Limitations

- **Gmail storage quota**: 15 GB shared across all Gmail data (emails, Drive, Photos). Objects count against this limit.
- **Attachment size**: 25 MB per email. Objects larger than `chunk_size_bytes` (default 20 MB) are automatically chunked.
- **API rate limits**: 250 quota units/second. Each operation costs 5-100 units. Sufficient for backup workloads but not high-throughput applications.
- **Token expiry**: In Google Cloud Testing mode, refresh tokens expire after 7 days. Re-run `g3 auth` to obtain a new token.
- **Eventual consistency**: Gmail search indexing has a small delay. A newly written object may not appear in ListObjects for a few seconds.
- **No range requests**: Partial reads are not supported. GetObject always returns the full object.
- **Memory usage**: Multipart uploads and PutObject buffer the full object in memory during email construction.

## Project Structure

```
cmd/g3/              Entry point and subcommands (serve, auth, validate, version)
internal/
  audit/              Request ID generation, context propagation, audit logging
  auth/               SigV4 signature verification, bucket registry
  backend/
    types.go          ObjectBackend interface, result types, S3Error
    gmail.go          Gmail API client, PutObject, GetObject, HeadObject, DeleteObject
    gmail_list.go     ListObjects, ListBuckets, CreateBucket
    gmail_chunked.go  Large object chunking (write, read, delete)
    email.go          MIME email construction and parsing
    search.go         Gmail search query builder
  config/             YAML config loading, validation, defaults
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
