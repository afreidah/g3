# -------------------------------------------------------------------------------
# g3 - Multi-Stage Docker Build
#
# Author: Alex Freidah
#
# Builds a minimal Alpine-based container for the g3 S3 gateway. The builder
# stage compiles a static binary; the runtime stage adds only the binary and
# a nonroot user.
# -------------------------------------------------------------------------------

FROM golang:1.26.1-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 \
    go build -ldflags "-s -w -X github.com/afreidah/g3/internal/telemetry.Version=${VERSION}" \
    -o /g3 ./cmd/g3

# -------------------------------------------------------------------------
# RUNTIME
# -------------------------------------------------------------------------

FROM alpine:3.21

RUN addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup && \
    mkdir -p /etc/g3 /data/g3 && \
    chown appuser:appgroup /etc/g3 /data/g3

COPY --from=builder /g3 /usr/local/bin/g3

USER appuser
EXPOSE 9000

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -qO- http://localhost:9000/health/ready || exit 1

ENTRYPOINT ["g3"]
CMD ["-config", "/etc/g3/config.yaml"]
