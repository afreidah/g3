---
title: " "
archetype: "home"
description: "S3-compatible HTTP gateway backed by Gmail"
---

<div style="text-align: center; margin-top: -2rem; margin-bottom: -3rem;">
  <img src="/images/logo.png" alt="g3" style="max-width: 650px; height: auto;">
</div>

<div class="badge-grid">

{{% badge style="primary" icon="fas fa-envelope" %}}Gmail Storage{{% /badge %}}
{{% badge style="info" title=" " icon="fas fa-cloud-upload-alt" %}}S3-Compatible API{{% /badge %}}
{{% badge style="danger" icon="fas fa-fire" %}}Prometheus Metrics{{% /badge %}}
{{% badge style="green" icon="fas fa-puzzle-piece" %}}Chunked Uploads{{% /badge %}}
{{% badge style="warning" title=" " icon="fas fa-project-diagram" %}}OpenTelemetry Tracing{{% /badge %}}

</div>

<div style="text-align: center; margin-top: 1rem;">

{{% button href="docs/readme/" style="primary" icon="fas fa-book" %}}README{{% /button %}}
{{% button href="docs/architecture/" style="primary" icon="fas fa-project-diagram" %}}Architecture{{% /button %}}
{{% button href="godoc/" style="primary" icon="fas fa-code" %}}Go API{{% /button %}}
{{% button href="https://github.com/afreidah/g3" style="primary" icon="fab fa-github" %}}GitHub{{% /button %}}

</div>

<hr style="margin-top: 3rem;">

<h2 style="text-align: center; color: #60a5fa;">Gmail as an S3 storage backend</h2>

A Go service that presents an S3-compatible HTTP API and stores objects as Gmail emails. Buckets map to labels, object data lives in attachments, and metadata is stored as JSON in the email body. Designed for write-once/read-rarely workloads like offsite backups, where Gmail's 15 GB of free storage becomes a durable, API-accessible backup target.

<div class="hero-bullets">

- **Any S3 client works** -- AWS CLI, s3cmd, SDKs, or s3-orchestrator as a backend
- **Chunked storage** splits objects exceeding 25 MB across multiple emails transparently
- **Multipart uploads** are buffered and assembled, then routed through chunking automatically
- **Full observability** with Prometheus metrics, OpenTelemetry traces, and structured JSON logging with trace correlation

</div>

<hr style="margin-top: 3rem;">

<h2 style="text-align: center; color: #60a5fa;">Key Features</h2>

<div class="feature-grid">
  <div class="feature-item">
    <div>
      <strong>S3-Compatible API</strong>
      <p>PUT, GET, HEAD, DELETE, ListObjectsV2, multipart uploads, and bucket operations.</p>
    </div>
    <div class="feature-detail">Implements the S3 API surface needed for backup workloads. Works with the AWS CLI, any S3 SDK, and s3-orchestrator as a backend target. SigV4 request authentication with per-bucket credentials.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Gmail Storage Backend</strong>
      <p>Objects stored as emails with metadata in the body and data as attachments.</p>
    </div>
    <div class="feature-detail">Each object becomes an email under a Gmail label (bucket). JSON metadata in the body enables fast HeadObject without downloading attachment data. Gmail's 15 GB free tier provides durable, Google-backed storage.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Chunked Large Objects</strong>
      <p>Transparent splitting for objects exceeding Gmail's 25 MB attachment limit.</p>
    </div>
    <div class="feature-detail">Objects larger than the configured chunk size are split across numbered emails with a manifest. GetObject reassembles chunks transparently. DeleteObject cleans up all chunks. ListObjects shows the correct total size.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Multipart Upload</strong>
      <p>Standard S3 multipart protocol for large file uploads from any client.</p>
    </div>
    <div class="feature-detail">Parts are buffered in memory and assembled on CompleteMultipartUpload. The assembled object is routed through the normal write path, including chunking for large objects. Abandoned uploads are cleaned up after 1 hour.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Prometheus Metrics</strong>
      <p>Request counts, latency histograms, Gmail API metrics, and operational gauges.</p>
    </div>
    <div class="feature-detail">11 metric families prefixed with g3_ covering HTTP requests, Gmail API calls, inflight tracking, storage estimates, and build info. Exportable to any Prometheus-compatible monitoring stack.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>OpenTelemetry Tracing</strong>
      <p>Server spans for S3 requests with client spans for every Gmail API call.</p>
    </div>
    <div class="feature-detail">Exports traces via OTLP gRPC to Tempo or any OpenTelemetry-compatible backend. Custom g3.* attributes on every span. trace_id and span_id injected into structured logs for correlation in Grafana.</div>
  </div>
</div>
