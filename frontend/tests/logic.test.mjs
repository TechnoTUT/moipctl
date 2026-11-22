import test from 'node:test'
import assert from 'node:assert/strict'
import { nodeName, nodeHealth, rxPhase, collectSources, displayingOn, planShow, summarize, fmtRes, fmtFps, fmtLatency, pct } from '../src/logic.js'

const live = (id, state = {}, extra = {}) => ({ id, hostname: id, online: true, connected: true, state, ...extra })
const rx = (running, source, connected = true) => ({ rx: { running, current_source: source, is_connected: connected } })

test('nodeName prefers the alias, then the hostname, then the id', () => {
  assert.equal(nodeName({ id: 'a', hostname: 'utone-a', alias: 'Entrance' }), 'Entrance')
  assert.equal(nodeName({ id: 'a', hostname: 'utone-a' }), 'utone-a')
  assert.equal(nodeName({ id: 'static:10.0.0.1:8000' }), 'static:10.0.0.1:8000')
})

test('nodeHealth: live needs both advertised and connected', () => {
  assert.equal(nodeHealth({ online: true, connected: true }), 'live')
  assert.equal(nodeHealth({ online: true, connected: false }), 'stale')
  assert.equal(nodeHealth({ online: false, connected: false }), 'offline')
  assert.equal(nodeHealth({ online: false, connected: true }), 'offline')
})

test('rxPhase', () => {
  assert.equal(rxPhase({}), 'unknown')
  assert.equal(rxPhase({ state: rx(false) }), 'stopped')
  assert.equal(rxPhase({ state: rx(true, 'CAM1', false) }), 'searching')
  assert.equal(rxPhase({ state: rx(true, 'CAM1', true) }), 'connected')
})

test('collectSources merges by name, counts who sees what, ignores stale and offline nodes', () => {
  const nodes = [
    live('a', { sources: [{ name: 'cam1 (OBS)', host_name: 'PC1' }, { name: 'Zeta' }] }),
    live('b', { sources: [{ name: 'cam1 (OBS)', host_name: 'PC1', stream_name: 'OBS' }, { name: 'alpha' }] }),
    { id: 'c', online: true, connected: false, state: { sources: [{ name: 'old' }] } },
    { id: 'd', online: false, connected: false, state: { sources: [{ name: 'gone' }] } },
    live('e', {}), // no sources at all
    live('f', { sources: [null, { name: '' }, { name: 'x' }] }),
  ]
  const got = collectSources(nodes)
  assert.deepEqual(got.map((s) => s.name), ['alpha', 'cam1 (OBS)', 'x', 'Zeta'])
  const cam = got.find((s) => s.name === 'cam1 (OBS)')
  assert.deepEqual(cam.seenBy, ['a', 'b'])
  assert.equal(cam.stream_name, 'OBS', 'a later answer fills in what the first one did not know')
  assert.equal(cam.host_name, 'PC1')
})

test('displayingOn lists live nodes whose receiver runs on that source', () => {
  const nodes = [
    live('a', rx(true, 'CAM1')),
    live('b', rx(true, 'CAM2')),
    live('c', rx(false, 'CAM1')), // stopped: not displaying
    { id: 'd', online: true, connected: false, state: rx(true, 'CAM1') }, // stale
  ]
  assert.deepEqual(displayingOn(nodes, 'CAM1'), ['a'])
  assert.deepEqual(displayingOn(nodes, 'nothing'), [])
})

test('planShow switches running receivers, starts stopped ones and skips what cannot be operated', () => {
  const nodes = [
    live('run', rx(true, 'CAM1')),
    live('idle', rx(false, null)),
    live('nostate'),
    { id: 'stale', hostname: 'stale', online: true, connected: false },
    { id: 'off', alias: 'Lobby', online: false, connected: false },
  ]
  const p = planShow(nodes, ['run', 'idle', 'nostate', 'stale', 'off', 'ghost'])
  assert.deepEqual(p.switch, ['run'])
  assert.deepEqual(p.start, ['idle', 'nostate'])
  assert.deepEqual(p.skipped, [
    { id: 'stale', name: 'stale', reason: 'not reachable' },
    { id: 'off', name: 'Lobby', reason: 'offline' },
    { id: 'ghost', name: 'ghost', reason: 'unknown node' },
  ])
})

test('summarize counts successes and keeps each failure with its reason', () => {
  const names = { a: 'Entrance', b: 'Booth' }
  const s = summarize(
    [{ id: 'a', ok: true, code: 200 }, { id: 'b', ok: false, code: 400, error: 'RX is not running' }, { id: 'c', ok: false, code: 502 }],
    (id) => names[id] || id,
  )
  assert.equal(s.total, 3)
  assert.equal(s.ok, 1)
  assert.deepEqual(s.failed, [
    { id: 'b', name: 'Booth', error: 'RX is not running' },
    { id: 'c', name: 'c', error: 'HTTP 502' },
  ])
})

test('formatters show a dash for missing values', () => {
  assert.equal(fmtRes({ width: 1920, height: 1080 }), '1920 x 1080')
  assert.equal(fmtRes({ width: 0, height: 0 }), '-')
  assert.equal(fmtRes(undefined), '-')
  assert.equal(fmtFps(59.94), '59.9 fps')
  assert.equal(fmtFps(0), '-')
  assert.equal(fmtLatency(12.34), '12.3 ms')
  assert.equal(fmtLatency(null), '-')
  assert.equal(pct(7.25), '7.3%')
  assert.equal(pct(undefined), '-')
})
