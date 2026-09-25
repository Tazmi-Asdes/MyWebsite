# syntax=docker/dockerfile:1.7

# The versions are deliberately explicit so a release can record the exact
# toolchain used to build its immutable images.
ARG GO_VERSION=1.27.1
ARG NODE_VERSION=24.16.0
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ARG NPM_CONFIG_REGISTRY=https://registry.npmjs.org

FROM node:${NODE_VERSION}-alpine AS admin-build
WORKDIR /src/web/admin
ARG NPM_CONFIG_REGISTRY
ENV NPM_CONFIG_REGISTRY=${NPM_CONFIG_REGISTRY}

COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY web/admin/ ./
RUN npm run build

FROM golang:${GO_VERSION}-alpine AS go-build
WORKDIR /src
ARG GOPROXY
ARG GOSUMDB
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}

COPY go.mod go.sum ./
RUN go mod download

# Copy only the Go build inputs.  The runtime stages below receive binaries
# and the compiled admin assets, never the source tree or package caches.
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/public/ ./web/public/

RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
RUN go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl

FROM alpine:3.22 AS app-runtime

# The accepted first-release decision is to run the application as root.  No
# container-hardening claim is made here; this image only contains runtime
# libraries, the two Go binaries, and the upload mount point.
RUN apk add --no-cache ca-certificates tzdata \
    && install -d -m 0755 /data/uploads

WORKDIR /app
COPY --from=go-build /out/server /usr/local/bin/server
COPY --from=go-build /out/adminctl /usr/local/bin/adminctl

EXPOSE 8080
VOLUME ["/data/uploads"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --spider --quiet http://127.0.0.1:8080/-/live || exit 1
ENTRYPOINT ["/usr/local/bin/server"]

FROM nginx:1.29-alpine AS admin-web

COPY deploy/admin-web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=admin-build /src/web/admin/dist /usr/share/nginx/html/admin

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --spider --quiet http://127.0.0.1:8080/admin/ || exit 1
