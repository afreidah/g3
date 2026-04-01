---
title: "Architecture"
weight: 5
---

<p class="landing-subheader">Data flow from S3 clients through the g3 gateway into Gmail storage</p>

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
    LIST["ListObjects"]
    MULTI["Multipart Store"]
    CHUNK["Chunk Manager"]
    GMAIL["Gmail API"]
    LABELS["Labels = Buckets"]
    EMAILS["Emails = Objects"]
    METRICS["Prometheus"]
    TRACING["Tempo"]

    CLIENT -->|"S3 API requests"| AUTH
    AUTH -->|"bucket resolved"| ROUTER
    ROUTER --> PUT
    ROUTER --> GET
    ROUTER --> LIST
    ROUTER --> MULTI
    MULTI -->|"assemble parts"| PUT
    PUT -->|"< 20 MB"| GMAIL
    PUT -->|">= 20 MB"| CHUNK
    CHUNK -->|"chunk emails"| GMAIL
    GET -->|"fetch + reassemble"| GMAIL
    LIST -->|"search emails"| GMAIL
    GMAIL --> LABELS
    GMAIL --> EMAILS
    ROUTER -->|"/metrics"| METRICS
    ROUTER -->|"OTLP gRPC"| TRACING

    classDef client fill:#172554,stroke:#60a5fa,color:#dbeafe
    classDef server fill:#1e293b,stroke:#334155,color:#e2e8f0
    classDef gmail fill:#132a1f,stroke:#22c55e,color:#dcfce7
    classDef obs fill:#2d2513,stroke:#f97316,color:#fef3c7

    class CLIENT client
    class AUTH,ROUTER,PUT,GET,LIST,MULTI,CHUNK server
    class GMAIL,LABELS,EMAILS gmail
    class METRICS,TRACING obs
{{< /mermaid >}}

<script>
document.addEventListener('DOMContentLoaded', function() {
  const nodeInfo = {
    'CLIENT':  { title: 'S3 Clients', detail: 'Any S3-compatible client: AWS CLI, s3cmd, SDKs, or s3-orchestrator. Connects via standard S3 API with SigV4 credentials.' },
    'AUTH':    { title: 'SigV4 Authentication', detail: 'Validates AWS Signature Version 4 request signatures with constant-time comparison. Maps access key IDs to bucket names via the bucket registry. Caches signing keys per credential scope.' },
    'ROUTER':  { title: 'HTTP Router', detail: 'Dispatches S3 API requests by method and path. Generates request IDs, creates OpenTelemetry server spans, and emits audit log entries for every operation.' },
    'PUT':     { title: 'PutObject', detail: 'Writes objects to Gmail. Small objects (< 20 MB) become a single email. Larger objects are routed to the chunk manager. Computes MD5 ETag during write. Last-write-wins semantics.' },
    'GET':     { title: 'GetObject', detail: 'Retrieves objects from Gmail by searching for the email with the matching subject. Chunked objects are reassembled transparently by fetching all chunk emails in order.' },
    'LIST':    { title: 'ListObjects', detail: 'Searches Gmail for emails matching the bucket label and key prefix. Supports delimiter-based common prefixes, pagination via start-after, and returns ETags from email body metadata.' },
    'MULTI':   { title: 'Multipart Store', detail: 'Buffers S3 multipart upload parts in memory. On CompleteMultipartUpload, parts are assembled in order and delegated to PutObject. Max 100 concurrent uploads, parts 1-10000. Abandoned uploads expire after 1 hour.' },
    'CHUNK':   { title: 'Chunk Manager', detail: 'Splits objects exceeding the chunk size (default 20 MB) across multiple emails. Each chunk is a separate email with a numbered subject suffix. A manifest email records chunk count, total size, and ETag.' },
    'GMAIL':   { title: 'Gmail API', detail: 'Google Gmail API v1 with OAuth2 authentication. Objects are inserted as emails with messages.insert, retrieved with messages.get (format=full for metadata, format=raw for data), and found with messages.list.' },
    'LABELS':  { title: 'Labels = Buckets', detail: 'Each S3 bucket maps to a Gmail label under the configured prefix (default s3/). CreateBucket creates a label, ListBuckets lists labels, HeadBucket checks label existence.' },
    'EMAILS':  { title: 'Emails = Objects', detail: 'Each object is an email. Subject encodes the key (s3://bucket/key). Body contains JSON metadata (content type, ETag, size, user metadata). Attachment carries the object data.' },
    'METRICS': { title: 'Prometheus', detail: '11 metric families prefixed with g3_. Covers HTTP request counts and latency, Gmail API calls and latency, inflight requests, storage estimates, object counts, audit events, and build info.' },
    'TRACING': { title: 'Tempo', detail: 'Receives OTLP gRPC traces. Each S3 request produces a server span with child client spans for Gmail API calls. Custom g3.* attributes include bucket, key, operation, and gmail message ID.' }
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

### Single Objects (< 20 MB)

Each object is stored as one Gmail email:
- **Subject**: `s3://bucket-name/path/to/key` (used for search and identification)
- **Body**: JSON metadata (`content_type`, `etag`, `size`, user metadata)
- **Attachment**: Binary object data (`object.bin`)

HeadObject reads only the body (via Gmail `format=full`) without downloading the attachment.

### Chunked Objects (>= 20 MB)

Large objects are split across multiple emails:
- **Chunk emails**: `s3://bucket/key#chunk-001`, `#chunk-002`, ... each carrying a data segment
- **Manifest email**: `s3://bucket/key` with no attachment, body contains `{"chunked":true, "chunk_count":N, "total_size":N, "etag":"..."}`

GetObject fetches the manifest, then retrieves and reassembles all chunks in order. DeleteObject removes the manifest and all chunks.

### Multipart Uploads

S3 multipart uploads are accepted but handled entirely in memory:
1. **CreateMultipartUpload** allocates an upload ID and in-memory part map
2. **UploadPart** buffers each part keyed by part number
3. **CompleteMultipartUpload** sorts parts, concatenates, and delegates to PutObject
4. PutObject handles chunking if the assembled result exceeds the chunk size

Abandoned uploads are cleaned up by a background goroutine (1-hour TTL, 10-minute sweep).

## Observability

- **Prometheus metrics** on the configurable `/metrics` endpoint cover HTTP requests, Gmail API calls, and operational state
- **OpenTelemetry traces** export via OTLP gRPC with server spans for S3 requests and client spans for Gmail API calls
- **Structured JSON logs** include `trace_id` and `span_id` for correlation in Grafana Loki + Tempo
- **Audit logging** records security-relevant operations with request ID correlation

## Request Flow

1. S3 client sends a signed request (SigV4)
2. Auth layer validates the signature and resolves the target bucket
3. Router creates a server span, generates/adopts a request ID
4. Request is dispatched to the appropriate handler (PUT, GET, HEAD, DELETE, LIST, multipart)
5. Handler calls the Gmail backend, which creates child client spans for each API call
6. Response is written with appropriate S3 headers and XML
7. Metrics are recorded and an audit log entry is emitted
