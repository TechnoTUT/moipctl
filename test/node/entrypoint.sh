#!/bin/sh
# dbus and avahi-daemon, then the node. The pod's hostname becomes the node ID.
set -e

# A pod's default route is eth0, but this node is meant to look like a host on a
# VLAN. A controller that reaches it through the router (the source address is the
# controller's node IP) would get its replies out of eth0 and never see them. So,
# for every other interface, answer from that interface's address through that
# VLAN's gateway. On these VLANs the gateway is the network address + 1.
gateway_for() { # 10.10.20.8/20 -> 10.10.16.1
	addr=${1%/*}; plen=${1#*/}
	IFS=. read -r a b c d <<EOF2
$addr
EOF2
	ipint=$(( (a << 24) | (b << 16) | (c << 8) | d ))
	mask=$(( (0xFFFFFFFF << (32 - plen)) & 0xFFFFFFFF ))
	gw=$(( (ipint & mask) + 1 ))
	echo "$(( gw >> 24 & 255 )).$(( gw >> 16 & 255 )).$(( gw >> 8 & 255 )).$(( gw & 255 ))"
}

if [ "${1:-}" = "--print-gateway" ]; then gateway_for "$2"; exit 0; fi

table=100
for dev in $(ls /sys/class/net); do
	case "$dev" in lo | eth0) continue ;; esac
	cidr=$(ip -4 -o addr show dev "$dev" | awk '{print $4}' | head -n 1)
	[ -n "$cidr" ] || continue
	gw=$(gateway_for "$cidr")
	ip route add default via "$gw" dev "$dev" table $table
	ip rule add from "${cidr%/*}" lookup $table
	echo "entrypoint: replies from ${cidr%/*} leave through $dev via $gw"
	table=$((table + 1))
done

mkdir -p /run/dbus
rm -f /run/dbus/pid /run/avahi-daemon/pid
dbus-daemon --system --fork
avahi-daemon -D --no-drop-root --no-chroot
exec /opt/moip/bin/moip web --host 0.0.0.0 --port 8000 "$@"
