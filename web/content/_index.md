---
title: " "
archetype: "home"
description: "S3-compatible HTTP gateway backed by Gmail and Google Drive"
---

<div style="text-align: center; margin-top: -2rem; margin-bottom: -3rem;">
  <img src="/images/logo.png" alt="g3" style="max-width: 650px; height: auto;">
</div>

<div class="badge-grid">

{{% badge style="primary" icon="fas fa-hdd" %}}Google Drive Storage{{% /badge %}}
{{% badge style="info" title=" " icon="fas fa-envelope" %}}Gmail Metadata{{% /badge %}}
{{% badge style="green" icon="fas fa-database" %}}SQLite Index{{% /badge %}}
{{% badge style="danger" icon="fas fa-fire" %}}Prometheus Metrics{{% /badge %}}
{{% badge style="warning" title=" " icon="fas fa-project-diagram" %}}OpenTelemetry Tracing{{% /badge %}}

</div>

<div style="text-align: center; margin-top: 1rem;">

{{% button href="docs/readme/" style="primary" icon="fas fa-book" %}}README{{% /button %}}
{{% button href="docs/architecture/" style="primary" icon="fas fa-project-diagram" %}}Architecture{{% /button %}}
{{% button href="godoc/" style="primary" icon="fas fa-code" %}}Go API{{% /button %}}
{{% button href="https://github.com/afreidah/g3" style="primary" icon="fab fa-github" %}}GitHub{{% /button %}}

</div>

<hr style="margin-top: 3rem;">

<h2 style="text-align: center; color: #60a5fa;">S3 gateway backed by Google Drive and Gmail</h2>

A Go service that presents an S3-compatible HTTP API and stores objects using Google's free storage. Object data lives in Google Drive files (no size limit). Gmail emails serve as metadata pointers. A local SQLite index eliminates API calls for metadata-only operations like HeadObject and ListObjects. Designed for write-once/read-rarely workloads like offsite backups.

<div class="hero-bullets">

- **Any S3 client works** -- AWS CLI, s3cmd, SDKs, or s3-orchestrator as a backend
- **Drive hybrid storage** eliminates Gmail's 25 MB attachment limit -- no chunking needed
- **SQLite metadata index** makes HeadObject and ListObjects instant with zero API calls
- **Dual API quota pools** -- Drive and Gmail operate on separate rate limits
- **Full observability** with Prometheus metrics, OpenTelemetry traces, and structured JSON logging

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
      <strong>Drive + Gmail Hybrid Storage</strong>
      <p>Object data in Google Drive, metadata pointers in Gmail emails.</p>
    </div>
    <div class="feature-detail">Drive files store the actual object data with no size limit (up to 5TB). Gmail emails contain JSON metadata with the Drive file ID, ETag, and user metadata. Separate API quota pools nearly double total throughput.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>SQLite Metadata Index</strong>
      <p>Local database eliminates API calls for metadata-only operations.</p>
    </div>
    <div class="feature-detail">HeadObject and ListObjects resolve entirely from the local SQLite index with zero API calls. GetObject and DeleteObject use cached IDs to skip Gmail search. Index is populated automatically on writes.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Multipart Upload</strong>
      <p>Standard S3 multipart protocol for large file uploads from any client.</p>
    </div>
    <div class="feature-detail">Parts are buffered in memory and assembled on CompleteMultipartUpload. The assembled object is uploaded to Drive and recorded in Gmail and SQLite. Abandoned uploads are cleaned up after 1 hour.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>Prometheus Metrics</strong>
      <p>Request counts, latency histograms, Gmail/Drive API metrics, and operational gauges.</p>
    </div>
    <div class="feature-detail">11 metric families prefixed with g3_ covering HTTP requests, API calls to both Gmail and Drive, inflight tracking, storage estimates, and build info. Exportable to any Prometheus-compatible monitoring stack.</div>
  </div>
  <div class="feature-item">
    <div>
      <strong>OpenTelemetry Tracing</strong>
      <p>Server spans for S3 requests with client spans for every Gmail and Drive API call.</p>
    </div>
    <div class="feature-detail">Exports traces via OTLP gRPC to Tempo or any OpenTelemetry-compatible backend. Custom g3.* attributes on every span. trace_id and span_id injected into structured logs for correlation in Grafana.</div>
  </div>
</div>
