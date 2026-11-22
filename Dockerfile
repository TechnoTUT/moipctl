# syntax=docker/dockerfile:1
# moipctl: a single static binary. The submodules under external/ are not
# needed to build it, so they are not part of the build context.

# ---- Build ----
FROM docker.io/library/golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
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
