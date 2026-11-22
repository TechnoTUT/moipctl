#!/bin/sh
# dbus and avahi-daemon, then the node. The pod's hostname becomes the node ID.
set -e
mkdir -p /run/dbus
rm -f /run/dbus/pid /run/avahi-daemon/pid
dbus-daemon --system --fork
avahi-daemon -D --no-drop-root --no-chroot
exec /opt/moip/bin/moip web --host 0.0.0.0 --port 8000 "$@"
