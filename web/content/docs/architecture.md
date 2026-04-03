---
title: "Architecture"
weight: 5
---

<p class="landing-subheader">Data flow from S3 clients through the g3 gateway into Google Drive and Gmail</p>

<style>
.diagram-tooltip {
  display: none;
  position: fixed;
  background: #1e293b;
  border: 1px solid #60a5fa;
  border-radius: 8px;
  padding: 1rem 1.25rem;
  color: #e2e8f0;
  font-size: 0.9rem;
  line-height: 1.6;
  max-width: 360px;
  z-index: 1000;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.6);
  pointer-events: none;
}
.diagram-tooltip strong {
  color: #60a5fa;
  font-size: 1rem;
}
.diagram-tooltip .detail {
  margin-top: 0.5rem;
  color: #94a3b8;
}
</style>

<div id="diagram-tooltip" class="diagram-tooltip"></div>

{{< mermaid >}}
flowchart TD
    CLIENT["S3 Clients"]
    AUTH["SigV4 Auth"]
    ROUTER["HTTP Router"]
    PUT["PutObject"]
    GET["GetObject"]
    HEAD["HeadObject"]
    LIST["ListObjects"]
    MULTI["Multipart Store"]
    SQLITE["Metadata Index"]
    DRIVE["Google Drive API"]
    GMAIL["Gmail API"]
    DRIVESTORE["Drive Files"]
    GMAILSTORE["Gmail Emails"]
    METRICS["Prometheus"]
    TRACING["Tempo"]

    CLIENT -->|"S3 API requests"| AUTH
    AUTH -->|"bucket resolved"| ROUTER
    ROUTER --> PUT
    ROUTER --> GET
    ROUTER --> HEAD
    ROUTER --> LIST
    ROUTER --> MULTI
    MULTI -->|"assemble parts"| PUT
    PUT -->|"upload data"| DRIVE
    PUT -->|"insert metadata email"| GMAIL
    PUT -->|"record"| SQLITE
    GET -->|"lookup file ID"| SQLITE
    GET -->|"download data"| DRIVE
    HEAD -->|"local query"| SQLITE
    LIST -->|"prefix query"| SQLITE
    DRIVE --> DRIVESTORE
    GMAIL --> GMAILSTORE
    ROUTER -->|"/metrics"| METRICS
    ROUTER -->|"OTLP gRPC"| TRACING

    classDef client fill:#172554,stroke:#60a5fa,color:#dbeafe
    classDef server fill:#1e293b,stroke:#334155,color:#e2e8f0
    classDef google fill:#132a1f,stroke:#22c55e,color:#dcfce7
    classDef local fill:#1e293b,stroke:#60a5fa,color:#dbeafe
    classDef obs fill:#2d2513,stroke:#f97316,color:#fef3c7

    class CLIENT client
    class AUTH,ROUTER,PUT,GET,HEAD,LIST,MULTI server
    class DRIVE,GMAIL,DRIVESTORE,GMAILSTORE google
    class SQLITE local
    class METRICS,TRACING obs
{{< /mermaid >}}

<script>
document.addEventListener('DOMContentLoaded', function() {
  const nodeInfo = {
    'CLIENT':     { title: 'S3 Clients', detail: 'Any S3-compatible client: AWS CLI, s3cmd, SDKs, or s3-orchestrator. Connects via standard S3 API with SigV4 credentials.' },
    'AUTH':       { title: 'SigV4 Authentication', detail: 'Validates AWS Signature Version 4 request signatures with constant-time comparison. Maps access key IDs to bucket names via the bucket registry. Caches signing keys per credential scope.' },
    'ROUTER':     { title: 'HTTP Router', detail: 'Dispatches S3 API requests by method and path. Generates request IDs, creates OpenTelemetry server spans, and emits audit log entries for every operation.' },
    'PUT':        { title: 'PutObject', detail: 'Uploads object data to Google Drive, inserts a metadata-only email in Gmail with the Drive file ID, and records the mapping in the local SQLite index. No size limit on objects.' },
    'GET':        { title: 'GetObject', detail: 'Looks up the Drive file ID from the local SQLite index (one local query, no Gmail API call). Downloads object data directly from Google Drive.' },
    'HEAD':       { title: 'HeadObject', detail: 'Resolves entirely from the local SQLite index with zero API calls. Returns size, content type, ETag, last modified, and user metadata.' },
    'LIST':       { title: 'ListObjects', detail: 'Queries the local SQLite index with prefix matching. Supports delimiter-based common prefixes, pagination, and returns ETags. Zero API calls.' },
    'MULTI':      { title: 'Multipart Store', detail: 'Buffers S3 multipart upload parts in memory. On CompleteMultipartUpload, parts are assembled in order and delegated to PutObject. Max 100 concurrent uploads, parts 1-10000. Abandoned uploads expire after 1 hour.' },
    'SQLITE':     { title: 'Metadata Index', detail: 'Database (SQLite or PostgreSQL) mapping bucket/key to Gmail message ID, Drive file ID, and full object metadata. Eliminates Gmail API calls for HeadObject and ListObjects. SQLite for single-node, PostgreSQL for multi-node cluster deployments.' },
    'DRIVE':      { title: 'Google Drive API', detail: 'Stores and retrieves object data as Drive files in a root folder. No file size limit (up to 5TB). Separate API quota pool from Gmail: 12,000 requests/user/minute.' },
    'GMAIL':      { title: 'Gmail API', detail: 'Stores metadata-only emails as object pointers. Each email body contains JSON with Drive file ID, ETag, size, and user metadata. Labels provide bucket isolation.' },
    'DRIVESTORE': { title: 'Drive Files', detail: 'Object data stored as individual files in a g3 root folder. Files are named bucket/key for identification. Backed by Google infrastructure with built-in redundancy.' },
    'GMAILSTORE': { title: 'Gmail Emails', detail: 'Metadata pointer emails with subject s3://bucket/key and JSON body. No attachments. Labels map to S3 buckets. Provides a secondary index independent of the local SQLite database.' },
    'METRICS':    { title: 'Prometheus', detail: '11 metric families prefixed with g3_. Covers HTTP request counts and latency, Gmail/Drive API calls and latency, inflight requests, storage estimates, object counts, audit events, and build info.' },
    'TRACING':    { title: 'Tempo', detail: 'Receives OTLP gRPC traces. Each S3 request produces a server span with child client spans for Gmail and Drive API calls. Custom g3.* attributes include bucket, key, operation, and message/file IDs.' }
  };

  const tooltip = document.getElementById('diagram-tooltip');

  setTimeout(function() {
    document.querySelectorAll('.mermaid .node, .mermaid .nodeLabel').forEach(function(el) {
      var node = el.closest('.node') || el;
      var id = node.id || '';
      var key = id.replace(/^flowchart-/, '').replace(/-\d+$/, '');

      if (!nodeInfo[key]) return;

      node.style.cursor = 'pointer';

      node.addEventListener('mouseenter', function(e) {
        var info = nodeInfo[key];
        tooltip.innerHTML = '<strong>' + info.title + '</strong><div class="detail">' + info.detail + '</div>';
        tooltip.style.display = 'block';
      });

      node.addEventListener('mousemove', function(e) {
        tooltip.style.left = (e.clientX + 16) + 'px';
        tooltip.style.top = (e.clientY + 16) + 'px';
      });

      node.addEventListener('mouseleave', function() {
        tooltip.style.display = 'none';
      });
    });
  }, 1000);
});
</script>

## Storage Model

### Object Data (Google Drive)

Object data is stored as individual files in a root Drive folder (`s3/` by default). There is no file size limit -- Drive supports up to 5TB per file, eliminating the need for chunking. Each file is named `bucket/key` for identification.

### Object Metadata (Gmail + Metadata Index)

Each object has a corresponding Gmail email:
- **Subject**: `s3://bucket-name/path/to/key` (used for search and identification)
- **Body**: JSON metadata (`content_type`, `etag`, `size`, `drive_file_id`, user metadata)
- **No attachment** -- object data lives in Drive

A metadata index (SQLite or PostgreSQL) caches this data along with the Gmail message ID and Drive file ID. This index is the primary lookup path for all read operations. SQLite is the default for single-node deployments; PostgreSQL allows the service to run on any node in a cluster without persistent local storage.

### API Call Budget

| Operation | Drive API Calls | Gmail API Calls | Index Queries |
|---|---|---|---|
| PutObject | 1 (upload) | 1 (insert email) | 1 (insert record) |
| GetObject | 1 (download) | 0 | 1 (lookup file ID) |
| HeadObject | 0 | 0 | 1 (lookup metadata) |
| ListObjects | 0 | 0 | 1 (prefix query) |
| DeleteObject | 1 (delete file) | 1 (delete email) | 1 (delete record) |

### Multipart Uploads

S3 multipart uploads are accepted and buffered in memory:
1. **CreateMultipartUpload** allocates an upload ID and in-memory part map
2. **UploadPart** buffers each part keyed by part number
3. **CompleteMultipartUpload** sorts parts, concatenates, and delegates to PutObject
4. PutObject handles the Drive upload and Gmail metadata insert

Abandoned uploads are cleaned up by a background goroutine (1-hour TTL, 10-minute sweep).

## Observability

- **Prometheus metrics** on the configurable `/metrics` endpoint cover HTTP requests, Gmail/Drive API calls, and operational state
- **OpenTelemetry traces** export via OTLP gRPC with server spans for S3 requests and client spans for Gmail/Drive API calls
- **Structured JSON logs** include `trace_id` and `span_id` for correlation in Grafana Loki + Tempo
- **Audit logging** records security-relevant operations with request ID correlation

## Request Flow

1. S3 client sends a signed request (SigV4)
2. Auth layer validates the signature and resolves the target bucket
3. Router creates a server span, generates/adopts a request ID
4. Request is dispatched to the appropriate handler
5. **Writes**: data uploaded to Drive, metadata email inserted in Gmail, record stored in SQLite
6. **Reads**: metadata resolved from SQLite index, data downloaded from Drive
7. **Metadata-only operations**: resolved entirely from SQLite with zero API calls
8. Response is written with appropriate S3 headers and XML
9. Metrics are recorded and an audit log entry is emitted
