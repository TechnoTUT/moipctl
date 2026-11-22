#!/usr/bin/env python3
# Five fake moip-toolkit nodes on 127.0.0.1:19001-19005, each in a different state, that answer the API moipctl
# uses (/api/events, /api/node/info and the rx/tx/multiview/signage operations) and change their state when they
# are operated. For trying the web UI without hardware: see test/ui/run.sh.
import json, threading, time, random, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

SOURCES = [
    {"name": "CAM1 (OBS)", "stream_name": "OBS", "host_name": "CAM-PC", "url": None},
    {"name": "VJ (Resolume Arena)", "stream_name": "Resolume Arena", "host_name": "VJ-PC", "url": None},
    {"name": "DJ BOOTH (Camera 2)", "stream_name": "Camera 2", "host_name": "UTONE-TX01", "url": None},
]

def base(rx=None, tx=None, mv=None, sg=None):
    return {
        "sources": SOURCES,
        "rx": {"running": False, "is_connected": False, "current_source": None, "recv_fmt": "uyvy", "recv_bandwidth": "highest", "fullscreen": True, "video_driver": "kmsdrm", "width": 0, "height": 0, "fps": 0, "fps_real": 0, "fps_recv": 0, "latency_ms": None, "dropped_frames": 0, "skipped_frames": 0, "error": None, **(rx or {})},
        "tx": {"running": False, "sender_name": None, "actual_width": 0, "actual_height": 0, "actual_fps": 0, "dropped_frames": 0, "connections": 0, "error": None, **(tx or {})},
        "multiview": {"running": False, "fullscreen": True, "auto_discovery": True, "slots": [], "error": None, **(mv or {})},
        "signage": {"running": False, "current_image": None, "filename": None, "fullscreen": True, "width": 0, "height": 0, "output": "", "error": None, **(sg or {})},
        "system": {"cpu_percent": 12.0, "mem_percent": 31.0, "mem_used_mb": 2400, "mem_total_mb": 7800, "load_avg": [0.4, 0.3, 0.2]},
    }

def rx_on(src): return {"running": True, "is_connected": True, "current_source": src, "width": 1920, "height": 1080, "fps": 59.94, "fps_real": 59.9, "fps_recv": 59.9, "latency_ms": 11.8}

NODES = {
    19001: ("utone-a1b2c3", base(sg={"running": True, "filename": "flyer-2026-11.png", "current_image": "flyer-2026-11.png", "width": 1920, "height": 1080, "output": "kmsdrm"})),
    19002: ("utone-d4e5f6", base(rx=rx_on("CAM1 (OBS)"))),
    19003: ("utone-0a1b2c", base(rx={"running": True, "is_connected": False, "current_source": "VJ (Resolume Arena)"})),
    19004: ("utone-77aa88", base(tx={"running": True, "sender_name": "utone-77aa88 (TX)", "actual_width": 1920, "actual_height": 1080, "actual_fps": 30, "connections": 2})),
    19005: ("utone-c0ffee", base(rx=rx_on("CAM1 (OBS)"), mv={"running": False})),
}
cond = threading.Condition()
version = {p: 0 for p in NODES}

def bump(p):
    with cond:
        version[p] += 1
        cond.notify_all()

def jitter():
    while True:
        time.sleep(2)
        for p, (_, st) in NODES.items():
            st["system"]["cpu_percent"] = round(max(1, min(95, st["system"]["cpu_percent"] + random.uniform(-4, 4))), 1)
            st["system"]["mem_percent"] = round(max(10, min(95, st["system"]["mem_percent"] + random.uniform(-1, 1))), 1)
            bump(p)

def make_handler(port):
    host, st = NODES[port]
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a): pass
        def send_json(self, code, obj):
            b = json.dumps(obj).encode(); self.send_response(code); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
        def do_GET(self):
            if self.path == "/api/node/info": return self.send_json(200, {"id": host, "hostname": host, "version": "fake", "roles": ["rx", "tx", "multiview", "signage"]})
            if self.path != "/api/events": return self.send_json(404, {"detail": "not found"})
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.end_headers()
            seen = -1
            try:
                while True:
                    with cond:
                        if version[port] == seen: cond.wait(0.5)
                        cur = version[port]
                    if cur != seen:
                        seen = cur; self.wfile.write(("event: state\ndata: %s\n\n" % json.dumps(st)).encode())
                    else: self.wfile.write(b": keepalive\n\n")
                    self.wfile.flush()
            except Exception: pass
        def do_POST(self):
            n = int(self.headers.get("Content-Length") or 0); body = json.loads(self.rfile.read(n) or b"{}")
            role, _, action = self.path.removeprefix("/api/").partition("/")
            rx = st["rx"]
            if (role, action) == ("rx", "start"):
                if not body.get("sender_name"): return self.send_json(400, {"detail": "sender_name is required"})
                rx.update(running=True, is_connected=False, current_source=body["sender_name"]); bump(port)
                threading.Timer(1.5, lambda: (rx.update(rx_on(body["sender_name"])), bump(port))).start()
            elif (role, action) == ("rx", "switch"):
                if not rx["running"]: return self.send_json(400, {"detail": "RX is not running"})
                rx.update(is_connected=False, current_source=body.get("sender_name")); bump(port)
                threading.Timer(1.0, lambda: (rx.update(rx_on(body["sender_name"])), bump(port))).start()
            elif (role, action) == ("rx", "stop"):
                st["rx"] = base()["rx"]; bump(port)
            elif (role, action) == ("tx", "stop"):
                st["tx"] = base()["tx"]; bump(port)
            elif (role, action) == ("multiview", "stop"):
                st["multiview"] = base()["multiview"]; bump(port)
            elif (role, action) == ("signage", "start"):
                st["signage"].update(running=True, filename="flyer-2026-11.png", current_image="flyer-2026-11.png"); bump(port)
            elif (role, action) == ("signage", "stop"):
                st["signage"] = base()["signage"]; bump(port)
            else: return self.send_json(404, {"detail": "not found"})
            self.send_json(200, {"ok": True})
    return H

threading.Thread(target=jitter, daemon=True).start()
servers = [ThreadingHTTPServer(("127.0.0.1", p), make_handler(p)) for p in NODES]
for s in servers: threading.Thread(target=s.serve_forever, daemon=True).start()
print("fake nodes on", list(NODES), flush=True)
time.sleep(10**6)
