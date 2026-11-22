# moipctl

Controller that manages several [moip-toolkit](https://github.com/TechnoTUT/moip-toolkit) nodes
(bundled in [utone-linux](https://github.com/TechnoTUT/utone-linux)) from one place.

Nodes advertise themselves over mDNS (`_moip._tcp`); moipctl discovers them, follows their
state through `/api/events` (SSE) and fans operations out to them, so nobody has to look up
DHCP addresses by hand.

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
| `--mdns` | discover nodes over mDNS (default on). Needs to be attached to the nodes' VLANs, see below |
| `--node host:port` | add a node by address (repeatable). It is never marked offline by discovery |
| `--state file` | keeps node aliases across restarts (empty: memory only) |

## API

Errors are `{"detail": "..."}`.

| Method | Path | Description |
|---|---|---|
| GET | `/api/nodes`, `/api/nodes/{id}` | Nodes with `online`, `connected` and the latest `state` of the node |
| GET | `/api/events` | Server-Sent Events: the node list, sent on every change |
| PUT | `/api/nodes/{id}/alias` | `{"alias": "..."}`: display name, kept in `--state` |
| POST | `/api/nodes/{id}/{role}/{action}` | Forward an operation to a node; the body goes to the node as is and its reply comes back |
| POST | `/api/bulk/{role}/{action}` | `{"nodes": [ids], "body": {...}}`: run it on several nodes in parallel; `results` has one entry per node, so a partial failure is visible |

`role/action` is one of `rx/{start,switch,stop}`, `tx/{start,stop}`, `multiview/{start,stop}`, `signage/{start,stop}`.

```bash
curl -X POST localhost:8080/api/bulk/rx/switch \
  -d '{"nodes": ["utone-a1b2c3", "utone-d4e5f6"], "body": {"sender_name": "CAM1 (OBS)"}}'
```

## Discovery

`moip web` advertises `_moip._tcp` through avahi-daemon with the TXT records `id`, `hostname` and `version`
(TechnoTUT/moip-toolkit#14). The node ID is its hostname, which utone-linux derives from the MAC address
(TechnoTUT/utone-linux#4). mDNS does not cross routers, so on Kubernetes attach the pod to the nodes' VLANs
with vxlan-cni (one `net` per VLAN, e.g. 20 and 42). A node is marked offline after three silent 15-second rounds.

## Build

```bash
git clone --recurse-submodules ...   # or: git submodule update --init
make build
./bin/moipctl serve --port 8080
```
