# moipctl

Controller that manages several [moip-toolkit](https://github.com/TechnoTUT/moip-toolkit) nodes
(bundled in [utone-linux](https://github.com/TechnoTUT/utone-linux)) from one place.

Nodes advertise themselves over mDNS (`_moip._tcp`); moipctl discovers them, follows their
state through `/api/events` (SSE) and fans operations out to them, so nobody has to look up
DHCP addresses by hand.

mDNS does not cross routers, so on Kubernetes the controller does not search itself. One small
**scout** (`moipctl scout`) per VLAN that has nodes searches there and reports the addresses it
finds to the controller, which then talks to the nodes through the router. The controller, which the
DJ and AV networks can reach, is not attached to any node VLAN.

## Layout

| Path | Content |
|---|---|
| `cmd/moipctl/` | entry point |
| `internal/` | controller packages |
| `external/moip-toolkit/` | submodule, pinned to the commit utone-linux bundles |
| `external/utone-linux/` | submodule |

Keep `external/moip-toolkit` and utone-linux's `opt/moip-toolkit` on the same commit.

## Run

```bash
./bin/moipctl serve --port 8080 --state /var/lib/moipctl/state.json
./bin/moipctl serve --mdns=false --node 10.10.16.20:8000   # add a node by hand
```

| Flag | Description |
|---|---|
| `--host`, `--port` | listen address (default `0.0.0.0:8080`) |
| `--mdns` | discover nodes over mDNS from this process (default on). Needs to be attached to the nodes' VLANs; use scouts instead where it is not |
| `--scout-listen addr` | accept scout reports on this address, e.g. `:8081` (default: off). Keep it apart from the API's network |
| `--scout-ttl` | a node that no scout has reported for this long is marked offline (default 45s, three scout rounds) |
| `--node host:port` | add a node by address (repeatable). It is never marked offline by discovery |
| `--state file` | keeps node aliases across restarts (empty: memory only) |

## API

Errors are `{"detail": "..."}`. Writes (POST, PUT) must send `Content-Type: application/json`, and a write that names another
origin is refused: the API has no authentication, so this keeps a web page that an operator happens to have open from driving it.

| Method | Path | Description |
|---|---|---|
| GET | `/api/nodes`, `/api/nodes/{id}` | Nodes with `online`, `connected` and the latest `state` of the node |
| GET | `/api/events` | Server-Sent Events: the node list, sent on every change |
| PUT | `/api/nodes/{id}/alias` | `{"alias": "..."}`: display name, kept in `--state` |
| POST | `/api/nodes/{id}/{role}/{action}` | Forward an operation to a node; the body goes to the node as is and its reply comes back |
| POST | `/api/bulk/{role}/{action}` | `{"nodes": [ids], "body": {...}}`: run it on several nodes in parallel; `results` has one entry per node, so a partial failure is visible |

`role/action` is one of `rx/{start,switch,stop}`, `tx/{start,stop}`, `multiview/{start,stop}`, `signage/{start,stop}`.

```bash
curl -X POST localhost:8080/api/bulk/rx/switch -H 'Content-Type: application/json' \
  -d '{"nodes": ["utone-a1b2c3", "utone-d4e5f6"], "body": {"sender_name": "CAM1 (OBS)"}}'
```

## Discovery

`moip web` advertises `_moip._tcp` through avahi-daemon with the TXT records `id`, `hostname` and `version`
(TechnoTUT/moip-toolkit#14). The node ID is its hostname, which utone-linux derives from the MAC address
(TechnoTUT/utone-linux#4).

mDNS stays inside a VLAN, so a scout has to be attached to every VLAN that has nodes:

```bash
moipctl serve --mdns=false --scout-listen :8081          # the controller
moipctl scout --controller http://moipctl:8081 --source av   # one per VLAN, attached to that VLAN
```

A scout searches every 15 seconds and reports after each search, also when it found nothing. The controller
marks a node offline when no report lists it within `--scout-ttl`, and a search that failed sends nothing, so a
broken scout does not take nodes offline by itself. Reports are validated (unicast IPv4 `host:port` only)
because the controller connects to the addresses in them; there is no authentication, so the scout listener
must only be reachable from the scouts (a separate port and a network policy). The controller reaches the nodes
(TCP 8000) through the router, which needs a firewall policy per node VLAN.

Each node shows the scout that found it as `source`.

## Testing against a real node

`test/node` builds a real moip-toolkit node into a container (the Go server and the Rust engine from the
`external/moip-toolkit` submodule, plus avahi-daemon so that it advertises `_moip._tcp` the way utone-linux does).
It has no NDI runtime (its licence does not allow redistributing it), so rx/tx/multiview fail in the engine, while
the API, the state stream, node info and the mDNS advertisement are real. CI publishes it as
`ghcr.io/technotut/moipctl-node-test`; `test/node/pod.yaml` runs one on VLAN 20 and one on VLAN 42.

```bash
git submodule update --init --recursive external/moip-toolkit
podman build -f test/node/Dockerfile -t moipctl-node-test .
podman run -d --name node --hostname utone-test moipctl-node-test
```

## Container

```bash
podman build -t moipctl --build-arg VERSION=dev .
podman run --rm -p 8080:8080 -v moipctl-data:/data moipctl --mdns=false --node 10.10.16.20:8000
```

The image runs as uid 10001 and keeps the aliases in `/data/state.json`. Pushes to `main` and `v*` tags publish
`ghcr.io/technotut/moipctl` (tag `v0.1.0` becomes image tag `0.1.0`). The Kubernetes manifests live in
[TechnoTUT/Infra](https://github.com/TechnoTUT/Infra) (`k8s/manifest/moipctl`).

## Build

```bash
git clone --recurse-submodules ...   # or: git submodule update --init
make build
./bin/moipctl serve --port 8080
```
