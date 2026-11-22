// Pure helpers for the controller UI. They know nothing about Vue, so `node --test` can run them.
//
// A node, as /api/events delivers it:
//   { id, hostname, alias, addr, source, online, connected, state: { sources, rx, tx, multiview, signage, system } }
// "online" is: the node is advertised on the network. "connected" is: moipctl has its state stream, so state is fresh.

/** What to call a node: its alias, else its hostname. */
export function nodeName(n) {
  return n.alias || n.hostname || n.id
}

/**
 * live:    advertised and its state stream is up, so what we show is current and it can be operated
 * stale:   advertised but moipctl cannot reach it (state may be old)
 * offline: no longer advertised
 */
export function nodeHealth(n) {
  if (!n.online) return 'offline'
  return n.connected ? 'live' : 'stale'
}

/** connected / searching / stopped for the receiver of a live node; unknown if there is no state. */
export function rxPhase(n) {
  const rx = n.state && n.state.rx
  if (!rx) return 'unknown'
  if (!rx.running) return 'stopped'
  return rx.is_connected ? 'connected' : 'searching'
}

/**
 * The NDI sources the nodes can see, merged by name. Only live nodes count: a stale node's list may be old.
 * Sorted by name, ignoring case. seenBy lists the IDs of the nodes that see the source.
 */
export function collectSources(nodes) {
  const byName = new Map()
  for (const n of nodes) {
    if (nodeHealth(n) !== 'live') continue
    for (const s of (n.state && n.state.sources) || []) {
      if (!s || !s.name) continue
      let e = byName.get(s.name)
      if (!e) {
        e = { name: s.name, stream_name: s.stream_name || null, host_name: s.host_name || null, seenBy: [] }
        byName.set(s.name, e)
      }
      // a node that answers later may know what an earlier one did not
      if (!e.stream_name && s.stream_name) e.stream_name = s.stream_name
      if (!e.host_name && s.host_name) e.host_name = s.host_name
      if (!e.seenBy.includes(n.id)) e.seenBy.push(n.id)
    }
  }
  return [...byName.values()].sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.name.localeCompare(b.name))
}

/** IDs of the live nodes whose receiver is running on this source. */
export function displayingOn(nodes, sourceName) {
  return nodes
    .filter((n) => nodeHealth(n) === 'live' && n.state && n.state.rx && n.state.rx.running && n.state.rx.current_source === sourceName)
    .map((n) => n.id)
}

/**
 * What to send to show a source on the chosen nodes: a receiver that is running is switched, one that is
 * stopped is started. Nodes that cannot be operated are skipped, with the reason.
 */
export function planShow(nodes, ids) {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const plan = { start: [], switch: [], skipped: [] }
  for (const id of ids) {
    const n = byId.get(id)
    if (!n) {
      plan.skipped.push({ id, name: id, reason: 'unknown node' })
      continue
    }
    const health = nodeHealth(n)
    if (health !== 'live') {
      plan.skipped.push({ id, name: nodeName(n), reason: health === 'offline' ? 'offline' : 'not reachable' })
    } else if (n.state && n.state.rx && n.state.rx.running) {
      plan.switch.push(id)
    } else {
      plan.start.push(id)
    }
  }
  return plan
}

/** Fold the per-node results of bulk calls into counts and the list of failures. */
export function summarize(results, nameOf = (id) => id) {
  const failed = []
  let ok = 0
  for (const r of results) {
    if (r.ok) ok++
    else failed.push({ id: r.id, name: nameOf(r.id), error: r.error || `HTTP ${r.code}` })
  }
  return { total: results.length, ok, failed }
}

export function fmtRes(rx) {
  return rx && rx.width ? `${rx.width} x ${rx.height}` : '-'
}

export function fmtFps(v) {
  return typeof v === 'number' && v > 0 ? `${v.toFixed(1)} fps` : '-'
}

export function fmtLatency(ms) {
  return typeof ms === 'number' ? `${ms.toFixed(1)} ms` : '-'
}

export function pct(v) {
  return typeof v === 'number' ? `${v.toFixed(1)}%` : '-'
}
