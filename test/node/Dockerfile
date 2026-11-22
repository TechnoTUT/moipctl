# syntax=docker/dockerfile:1
# A real moip-toolkit node in a container, for testing moipctl without hardware:
# the Go server and the Rust engine are built from the submodule
# external/moip-toolkit (the commit moipctl pins), and avahi-daemon runs next to
# them so that `moip web` advertises _moip._tcp exactly as it does on utone-linux.
#
# Not included: the NDI runtime (libndi.so.6). Its licence does not allow
# redistributing it, so rx/tx/multiview fail in the engine, while the API, the
# state stream, node info and the mDNS advertisement are the real thing.
#
# Build from the repository root, with the submodules fetched:
#   git submodule update --init --recursive external/moip-toolkit
#   podman build -f test/node/Dockerfile -t moipctl-node-test .

# ---- Engine (Rust) ----
FROM docker.io/library/rust:1-trixie AS engine
RUN apt-get update \
 && apt-get install -y --no-install-recommends libsdl2-dev libasound2-dev pkg-config \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY external/moip-toolkit/engine/ ./
RUN --mount=type=cache,target=/usr/local/cargo/registry --mount=type=cache,target=/src/target \
    cargo build --release --locked \
 && cp target/release/moip-engine /moip-engine

# ---- Server (Go) ----
FROM docker.io/library/golang:1.24-trixie AS server
WORKDIR /src
COPY external/moip-toolkit/server/ ./
ARG VERSION=test
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /moip ./cmd/moip

# ---- Runtime ----
FROM docker.io/library/debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends libsdl2-2.0-0 libasound2t64 avahi-daemon dbus \
 && rm -rf /var/lib/apt/lists/*
COPY --from=engine /moip-engine /opt/moip/bin/moip-engine
COPY --from=server /moip /opt/moip/bin/moip
COPY test/node/entrypoint.sh /entrypoint.sh
EXPOSE 8000
ENTRYPOINT ["/entrypoint.sh"]
