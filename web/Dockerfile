FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS godoc
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# gomarkdoc is a go.mod tool dependency, so `go tool` resolves a lock-enforced
# version from go.sum (no unpinned `go install ...@latest`).
RUN mkdir -p /godoc && \
    for pkg in audit auth backend config server store telemetry; do \
        printf -- '---\ntitle: "%s"\n---\n\n' "$pkg" > "/godoc/$pkg.md" && \
        go tool gomarkdoc "./internal/$pkg" >> "/godoc/$pkg.md" && \
        sed -i '/^# '"$pkg"'$/d' "/godoc/$pkg.md"; \
    done

FROM --platform=$BUILDPLATFORM hugomods/hugo:0.163.3 AS build
WORKDIR /src/web
COPY web/ /src/web/
COPY README.md /src/README.md
COPY CHANGELOG.md /src/CHANGELOG.md
COPY --from=godoc /godoc/ /src/web/content/godoc/
COPY web/content/godoc/_index.md /src/web/content/godoc/_index.md
RUN hugo --minify

# nginx-unprivileged runs as a non-root user (uid 101) and listens on 8080.
FROM nginxinc/nginx-unprivileged:1.29-alpine
USER root
RUN apk upgrade --no-cache
COPY --from=build /src/web/public /usr/share/nginx/html
USER nginx
EXPOSE 8080
