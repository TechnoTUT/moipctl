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

## Build

```bash
git clone --recurse-submodules ...   # or: git submodule update --init
make build
./bin/moipctl serve --port 8080
```
