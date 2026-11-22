# syntax=docker/dockerfile:1
# moipctl: a single static binary with the web UI embedded. The submodules under
# external/ are not needed to build it, so they are not part of the build context.

# ---- Web UI (Vue, built with Vite) ----
FROM docker.io/library/node:22-bookworm-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---- Build ----
FROM docker.io/library/golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# the web UI is embedded in the binary
COPY --from=web /src/frontend/dist/ internal/ui/dist/
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /moipctl ./cmd/moipctl \
 && mkdir /data

# ---- Runtime ----
# Alpine rather than scratch: `kubectl exec` has a shell for debugging.
FROM docker.io/library/alpine:3.21
RUN adduser -S -u 10001 -h /data moipctl
COPY --from=build --chown=10001:10001 /data /data
COPY --from=build /moipctl /usr/local/bin/moipctl
USER 10001
VOLUME /data
EXPOSE 8080
# Node aliases are kept in /data/state.json. mDNS discovery needs the pod to be
# attached to the nodes' VLANs.
ENTRYPOINT ["moipctl", "serve", "--host", "0.0.0.0", "--port", "8080", "--state", "/data/state.json"]
