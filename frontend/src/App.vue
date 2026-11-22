<script setup lang="ts">
// moipctl's UI. The look follows moip-toolkit's (same Tailwind classes, brand red, header, status pills).
// Text from the network (aliases, hostnames, NDI source names) is only ever shown with {{ }}, never as HTML.
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { nodeName, nodeHealth, rxPhase, collectSources, displayingOn, planShow, summarize, fmtRes, fmtFps, fmtLatency, pct } from './logic.js'

// Light by default; public/theme.js has already applied a saved dark choice before the first paint.
const dark = ref(document.documentElement.classList.contains('dark'))
function toggleColorMode() {
  dark.value = !dark.value
  document.documentElement.classList.toggle('dark', dark.value)
  try {
    localStorage.setItem('moipctl-theme', dark.value ? 'dark' : 'light')
  } catch {
    /* storage can be blocked; the choice then lasts until the page is closed */
  }
}

// ---------------------------------------------------------------- state
const nodes = ref<any[]>([])
const streamUp = ref(false) // the event stream to moipctl is open
const loaded = ref(false)
const selected = reactive(new Set<string>())
const busy = reactive(new Set<string>()) // node IDs with a request in flight
const editing = ref<string | null>(null)
const aliasDraft = ref('')
const toasts = ref<{ id: number; kind: 'ok' | 'err' | 'info'; title: string; lines: string[] }[]>([])
let es: EventSource | null = null
let toastSeq = 0

function connect() {
  es = new EventSource('/api/events')
  es.onopen = () => { streamUp.value = true }
  es.onerror = () => { streamUp.value = false } // EventSource reconnects by itself
  es.addEventListener('nodes', (e) => {
    try {
      nodes.value = JSON.parse((e as MessageEvent).data) || []
    } catch {
      return
    }
    loaded.value = true
    streamUp.value = true
    const ids = new Set(nodes.value.map((n) => n.id))
    for (const id of [...selected]) if (!ids.has(id)) selected.delete(id)
  })
}
onMounted(connect)
onUnmounted(() => es && es.close())

// ---------------------------------------------------------------- derived
const rank: Record<string, number> = { live: 0, stale: 1, offline: 2 }
const sortedNodes = computed(() =>
  [...nodes.value].sort((a, b) => rank[nodeHealth(a)] - rank[nodeHealth(b)] || nodeName(a).localeCompare(nodeName(b))),
)
const sources = computed(() => collectSources(nodes.value))
const liveNodes = computed(() => nodes.value.filter((n) => nodeHealth(n) === 'live'))
const counts = computed(() => ({
  total: nodes.value.length,
  live: liveNodes.value.length,
  receiving: liveNodes.value.filter((n) => rxPhase(n) === 'connected').length,
}))
const selectedCount = computed(() => selected.size)
const nameOf = (id: string) => {
  const n = nodes.value.find((x) => x.id === id)
  return n ? nodeName(n) : id
}

// ---------------------------------------------------------------- selection
function toggle(id: string) {
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
}
function selectAllLive() {
  selected.clear()
  for (const n of liveNodes.value) selected.add(n.id)
}
function clearSelection() {
  selected.clear()
}

// ---------------------------------------------------------------- API
async function api(method: string, path: string, body?: unknown) {
  const r = await fetch(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await r.text()
  let data: any = null
  try { data = text ? JSON.parse(text) : null } catch { /* not JSON */ }
  if (!r.ok) throw new Error((data && data.detail) || `HTTP ${r.status}`)
  return data
}

// One operation on several nodes. Always resolves: a request that fails as a whole becomes a failure for each node.
async function bulk(role: string, action: string, ids: string[], body?: unknown) {
  if (ids.length === 0) return []
  try {
    const data = await api('POST', `/api/bulk/${role}/${action}`, { nodes: ids, body: body ?? {} })
    return data.results as { id: string; ok: boolean; code: number; error?: string }[]
  } catch (e: any) {
    return ids.map((id) => ({ id, ok: false, code: 0, error: e.message }))
  }
}

function toast(kind: 'ok' | 'err' | 'info', title: string, lines: string[] = []) {
  const id = ++toastSeq
  toasts.value.push({ id, kind, title, lines })
  setTimeout(() => dismiss(id), kind === 'err' ? 14000 : 5000)
}
function dismiss(id: number) {
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

function report(done: string, results: { id: string; ok: boolean; code: number; error?: string }[], skipped: { name: string; reason: string }[] = []) {
  const s = summarize(results, nameOf)
  const lines = [
    ...s.failed.map((f) => `${f.name}: ${f.error}`),
    ...skipped.map((f) => `${f.name}: skipped, ${f.reason}`),
  ]
  const total = s.total + skipped.length
  if (lines.length === 0) toast('ok', `${done} (${s.ok} node${s.ok === 1 ? '' : 's'})`)
  else toast('err', `${done}: ${s.ok} of ${total} succeeded`, lines)
}

async function run(ids: string[], fn: () => Promise<void>) {
  ids.forEach((id) => busy.add(id))
  try { await fn() } finally { ids.forEach((id) => busy.delete(id)) }
}

// Show a source on the selected nodes: a running receiver is switched, a stopped one is started.
async function showOn(sourceName: string) {
  const ids = [...selected]
  if (ids.length === 0) {
    toast('info', 'Select one or more nodes first')
    return
  }
  const plan = planShow(nodes.value, ids)
  await run(ids, async () => {
    const [sw, st] = await Promise.all([
      bulk('rx', 'switch', plan.switch, { sender_name: sourceName }),
      bulk('rx', 'start', plan.start, { sender_name: sourceName, fullscreen: true }),
    ])
    report(`Showing "${sourceName}"`, [...sw, ...st], plan.skipped)
  })
}

async function act(role: string, action: string, ids: string[], done: string) {
  if (ids.length === 0) return
  await run(ids, async () => report(done, await bulk(role, action, ids)))
}
const stopRx = (ids: string[]) => act('rx', 'stop', ids, 'Receiver stopped')
const stopTx = (ids: string[]) => act('tx', 'stop', ids, 'Sender stopped')
const stopMultiview = (ids: string[]) => act('multiview', 'stop', ids, 'Multi-view stopped')
const startSignage = (ids: string[]) => act('signage', 'start', ids, 'Signage started')
const stopSignage = (ids: string[]) => act('signage', 'stop', ids, 'Signage stopped')
const stopSelectedRx = () => stopRx([...selected].filter((id) => nodes.value.some((n) => n.id === id && nodeHealth(n) === 'live')))

// ---------------------------------------------------------------- alias
function startEdit(n: any) {
  editing.value = n.id
  aliasDraft.value = n.alias || ''
}
async function saveAlias(n: any) {
  const id = n.id
  try {
    await api('PUT', `/api/nodes/${encodeURIComponent(id)}/alias`, { alias: aliasDraft.value })
    editing.value = null
  } catch (e: any) {
    toast('err', 'Could not save the name', [e.message])
  }
}

// ---------------------------------------------------------------- look
const pillBase = 'px-2.5 py-1 rounded-full text-xs font-semibold uppercase tracking-wider border'
const pillSm = 'px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wider border'
const pillClass: Record<string, string> = {
  ok: 'bg-emerald-50 dark:bg-emerald-950/40 text-emerald-700 dark:text-emerald-400 border-emerald-200 dark:border-emerald-800',
  warn: 'bg-amber-50 dark:bg-amber-950/40 text-amber-700 dark:text-amber-400 border-amber-200 dark:border-amber-800',
  bad: 'bg-rose-50 dark:bg-rose-950/40 text-rose-700 dark:text-rose-400 border-rose-200 dark:border-rose-800',
  off: 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-transparent',
}
const healthPill = (n: any) => ({ live: 'ok', stale: 'warn', offline: 'off' } as Record<string, string>)[nodeHealth(n)]
const healthText = (n: any) => ({ live: 'Online', stale: 'Unreachable', offline: 'Offline' } as Record<string, string>)[nodeHealth(n)]
const rxPill = (n: any) => ({ connected: 'ok', searching: 'warn', stopped: 'off', unknown: 'off' } as Record<string, string>)[rxPhase(n)]
const rxText = (n: any) => ({ connected: 'Connected', searching: 'Searching', stopped: 'Stopped', unknown: '-' } as Record<string, string>)[rxPhase(n)]
const usageText = (v: number) => (v > 85 ? 'text-rose-500' : v > 60 ? 'text-amber-500' : 'text-emerald-500')
const canOperate = (n: any) => nodeHealth(n) === 'live' && !busy.has(n.id)
const btnStop =
  'py-1.5 px-3 bg-rose-50 dark:bg-rose-950/30 hover:bg-rose-100 dark:hover:bg-rose-900/40 text-rose-700 dark:text-rose-400 border border-rose-200 dark:border-rose-800/60 rounded-xl font-medium text-xs transition disabled:opacity-50 disabled:cursor-not-allowed'
const btnPlain =
  'py-1.5 px-3 bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 dark:hover:bg-slate-700 text-slate-800 dark:text-slate-100 rounded-xl text-xs font-semibold transition disabled:opacity-50 disabled:cursor-not-allowed'
const card = 'bg-white dark:bg-slate-900 border rounded-2xl p-4 space-y-3 shadow-sm transition'
const h2 = 'text-xs font-bold text-slate-500 dark:text-slate-400 uppercase tracking-wider'
</script>

<template>
  <div class="min-h-screen bg-slate-50 dark:bg-slate-950 text-slate-800 dark:text-slate-100 flex flex-col font-sans transition-colors duration-200">
    <!-- Top Navigation Bar -->
    <header class="border-b border-slate-200 dark:border-slate-800 bg-white/90 dark:bg-slate-900/80 backdrop-blur px-6 py-3 flex items-center justify-between sticky top-0 z-30 shadow-sm">
      <a href="/" class="flex items-center gap-4 py-0.5">
        <img src="/logo.svg" alt="TechnoTUT" class="h-12 w-auto max-w-[200px] object-contain dark:hidden" />
        <img src="/logo_dark.svg" alt="TechnoTUT" class="h-12 w-auto max-w-[200px] object-contain hidden dark:block" />
        <div class="h-8 w-px bg-slate-200 dark:bg-slate-800"></div>
        <div>
          <h1 class="text-base font-bold tracking-tight text-slate-900 dark:text-slate-100">MoIP Controller</h1>
          <p class="text-[11px] text-slate-500 dark:text-slate-400 hidden sm:block">The Utopia Tone Streaming Network</p>
        </div>
      </a>

      <div class="flex items-center gap-3">
        <!-- Counts -->
        <div class="hidden md:flex items-center gap-3 bg-slate-100 dark:bg-slate-900 px-3 py-1.5 rounded-xl border border-slate-200 dark:border-slate-800 text-xs font-mono select-none">
          <span class="flex items-center gap-1.5"><span class="text-slate-400 font-sans font-semibold text-[10px] uppercase">Nodes</span><span class="font-bold tabular-nums">{{ counts.live }}/{{ counts.total }}</span></span>
          <span class="w-px h-3 bg-slate-300 dark:bg-slate-700"></span>
          <span class="flex items-center gap-1.5"><span class="text-slate-400 font-sans font-semibold text-[10px] uppercase">Receiving</span><span class="font-bold tabular-nums">{{ counts.receiving }}</span></span>
        </div>

        <!-- Connection to moipctl -->
        <span
          class="flex items-center gap-2 px-3 py-1.5 rounded-xl border text-xs font-semibold"
          :class="streamUp ? 'border-emerald-200 dark:border-emerald-800 text-emerald-700 dark:text-emerald-400 bg-emerald-50 dark:bg-emerald-950/40' : 'border-rose-200 dark:border-rose-800 text-rose-700 dark:text-rose-400 bg-rose-50 dark:bg-rose-950/40'"
          :title="streamUp ? 'Receiving live updates from moipctl' : 'Lost the connection to moipctl; reconnecting'"
        >
          <span class="h-2 w-2 rounded-full" :class="streamUp ? 'bg-emerald-400 animate-pulse' : 'bg-rose-400'" />
          <span>{{ streamUp ? 'Live' : 'Reconnecting' }}</span>
        </span>

        <!-- Theme Toggle Button -->
        <button
          @click="toggleColorMode"
          class="p-2 rounded-xl border border-slate-200 dark:border-slate-800 text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100 hover:bg-slate-100 dark:hover:bg-slate-800 transition"
          title="Toggle Light / Dark Mode"
        >
          <svg v-if="dark" class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
          </svg>
          <svg v-else class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
          </svg>
        </button>
      </div>
    </header>

    <!-- Main Content -->
    <main class="flex-1 p-6 max-w-7xl mx-auto w-full">
      <div class="grid grid-cols-1 xl:grid-cols-5 gap-6">
        <!-- ==================== Nodes (left 3 of 5 cols on wide screens) ==================== -->
        <div class="space-y-4 xl:col-span-3">
          <div class="flex items-center justify-between">
            <h2 class="text-base font-bold text-slate-900 dark:text-slate-100 tracking-tight flex items-center gap-2">
              <span>Nodes</span>
              <span class="text-xs bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-semibold px-2 py-0.5 rounded-full">{{ nodes.length }}</span>
            </h2>
            <div class="flex items-center gap-3 text-xs font-medium">
              <button @click="selectAllLive" class="text-slate-500 dark:text-slate-400 hover:text-[#C7000A] transition">Select all</button>
              <button @click="clearSelection" :disabled="selectedCount === 0" class="text-slate-500 dark:text-slate-400 hover:text-[#C7000A] transition disabled:opacity-40">Clear</button>
            </div>
          </div>

          <div v-if="loaded && nodes.length === 0" class="bg-white dark:bg-slate-900 border border-dashed border-slate-300 dark:border-slate-800 rounded-2xl p-10 text-center space-y-2 shadow-sm">
            <p class="text-sm font-medium text-slate-600 dark:text-slate-400">No nodes found yet.</p>
            <p class="text-xs text-slate-400 dark:text-slate-500">A node appears when moip-toolkit runs on a network that has a scout.</p>
          </div>
          <div v-else-if="!loaded" class="text-sm text-slate-400 dark:text-slate-500 px-1">Connecting to moipctl...</div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
          <div
            v-for="n in sortedNodes"
            :key="n.id"
            :class="[card, selected.has(n.id) ? 'border-[#C7000A] ring-2 ring-[#C7000A]/30' : 'border-slate-200 dark:border-slate-800', nodeHealth(n) === 'offline' ? 'opacity-60' : '']"
          >
            <!-- Title row -->
            <div class="flex items-start justify-between gap-3">
              <div class="flex items-start gap-3 min-w-0">
                <input
                  type="checkbox"
                  :checked="selected.has(n.id)"
                  :disabled="nodeHealth(n) !== 'live'"
                  @change="toggle(n.id)"
                  class="mt-1 rounded bg-white dark:bg-slate-950 border-slate-300 dark:border-slate-700 text-[#C7000A] focus:ring-[#C7000A] h-4 w-4 disabled:opacity-40"
                  :aria-label="`Select ${nodeName(n)}`"
                />
                <div class="min-w-0">
                  <div v-if="editing === n.id" class="flex items-center gap-2">
                    <input
                      v-model="aliasDraft"
                      @keyup.enter="saveAlias(n)"
                      @keyup.esc="editing = null"
                      maxlength="64"
                      :placeholder="n.hostname"
                      class="w-40 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-800 rounded-lg px-2 py-1 text-sm focus:outline-none focus:border-[#C7000A]"
                    />
                    <button @click="saveAlias(n)" class="text-xs font-semibold text-[#C7000A]">Save</button>
                    <button @click="editing = null" class="text-xs text-slate-400">Cancel</button>
                  </div>
                  <h3 v-else class="text-base font-bold text-slate-900 dark:text-slate-100 break-all flex items-center gap-2">
                    <span>{{ nodeName(n) }}</span>
                    <button @click="startEdit(n)" class="text-slate-300 hover:text-[#C7000A] dark:text-slate-600 transition" title="Rename">
                      <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536M9 13l6.768-6.768a2 2 0 112.828 2.828L11.828 15.83a2 2 0 01-.89.514L7 17l.656-3.938a2 2 0 01.514-.89z" /></svg>
                    </button>
                  </h3>
                  <p class="text-xs text-slate-500 dark:text-slate-400 font-mono mt-0.5 break-all">
                    {{ n.addr }}<span v-if="n.source" class="ml-2 px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-[10px] uppercase">{{ n.source }}</span>
                  </p>
                </div>
              </div>
              <span :class="[pillBase, pillClass[healthPill(n)]]">{{ healthText(n) }}</span>
            </div>

            <p v-if="nodeHealth(n) === 'stale'" class="text-xs text-amber-700 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-800 rounded-lg px-3 py-2">
              moipctl cannot reach this node. What is shown may be out of date.
            </p>

            <div v-if="n.state && nodeHealth(n) !== 'offline'" class="border-t border-slate-100 dark:border-slate-800 pt-3 space-y-3 text-sm">
              <!-- Receiver -->
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span :class="h2">RX</span>
                    <span :class="[pillSm, pillClass[rxPill(n)]]">{{ rxText(n) }}</span>
                  </div>
                  <p v-if="n.state.rx && n.state.rx.running" class="font-mono font-semibold text-slate-900 dark:text-slate-100 truncate mt-1" :title="n.state.rx.current_source">{{ n.state.rx.current_source || 'None' }}</p>
                  <p v-if="rxPhase(n) === 'connected'" class="text-xs text-slate-500 dark:text-slate-400 font-mono mt-0.5">
                    {{ fmtRes(n.state.rx) }} · {{ fmtFps(n.state.rx.fps_real) }} · {{ fmtLatency(n.state.rx.latency_ms) }}<span v-if="n.state.rx.dropped_frames" class="text-amber-600 dark:text-amber-400"> · {{ n.state.rx.dropped_frames }} dropped</span>
                  </p>
                  <p v-if="n.state.rx && n.state.rx.error" class="text-xs text-rose-600 dark:text-rose-400 break-words mt-0.5">{{ n.state.rx.error }}</p>
                </div>
                <button v-if="n.state.rx && n.state.rx.running" @click="stopRx([n.id])" :disabled="!canOperate(n)" :class="[btnStop, 'shrink-0']">Stop</button>
              </div>

              <!-- Sender: only while it is on air or failing -->
              <div v-if="n.state.tx && (n.state.tx.running || n.state.tx.error)" class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span :class="h2">TX</span>
                    <span :class="[pillSm, n.state.tx.running ? pillClass.bad : pillClass.off]">{{ n.state.tx.running ? 'On air' : 'Stopped' }}</span>
                  </div>
                  <p v-if="n.state.tx.running" class="font-mono font-semibold text-slate-900 dark:text-slate-100 truncate mt-1" :title="n.state.tx.sender_name">{{ n.state.tx.sender_name || '-' }}</p>
                  <p v-if="n.state.tx.running" class="text-xs text-slate-500 dark:text-slate-400 font-mono mt-0.5">
                    {{ n.state.tx.actual_width ? `${n.state.tx.actual_width} x ${n.state.tx.actual_height} @ ${(n.state.tx.actual_fps || 0).toFixed(0)}` : '-' }} · {{ n.state.tx.connections ?? 0 }} receiver{{ (n.state.tx.connections ?? 0) === 1 ? '' : 's' }}
                  </p>
                  <p v-if="n.state.tx.error" class="text-xs text-rose-600 dark:text-rose-400 break-words mt-0.5">{{ n.state.tx.error }}</p>
                </div>
                <button v-if="n.state.tx.running" @click="stopTx([n.id])" :disabled="!canOperate(n)" :class="[btnStop, 'shrink-0']">Stop</button>
              </div>

              <!-- Multi-view: only while running -->
              <div v-if="n.state.multiview && n.state.multiview.running" class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span :class="h2">Multi-view</span>
                    <span :class="[pillSm, pillClass.ok]">Running</span>
                  </div>
                  <p class="text-xs text-slate-500 dark:text-slate-400 font-mono mt-1">{{ (n.state.multiview.slots || []).filter((x: any) => x.is_connected).length }} / {{ (n.state.multiview.slots || []).length }} sources connected</p>
                </div>
                <button @click="stopMultiview([n.id])" :disabled="!canOperate(n)" :class="[btnStop, 'shrink-0']">Stop</button>
              </div>

              <!-- Signage -->
              <div class="flex items-center justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span :class="h2">Signage</span>
                    <span :class="[pillSm, n.state.signage && n.state.signage.running ? pillClass.ok : pillClass.off]">{{ n.state.signage && n.state.signage.running ? 'Showing' : 'Stopped' }}</span>
                  </div>
                  <p v-if="n.state.signage && n.state.signage.running" class="font-mono text-xs text-slate-500 dark:text-slate-400 truncate mt-1">{{ n.state.signage.filename || n.state.signage.current_image || '-' }}</p>
                  <p v-if="n.state.signage && n.state.signage.error" class="text-xs text-rose-600 dark:text-rose-400 break-words mt-0.5">{{ n.state.signage.error }}</p>
                </div>
                <button v-if="n.state.signage && n.state.signage.running" @click="stopSignage([n.id])" :disabled="!canOperate(n)" :class="[btnStop, 'shrink-0']">Stop</button>
                <button v-else @click="startSignage([n.id])" :disabled="!canOperate(n)" :class="[btnPlain, 'shrink-0']">Show</button>
              </div>

              <!-- System -->
              <div v-if="n.state.system" class="flex items-center gap-3 border-t border-slate-100 dark:border-slate-800 pt-2.5 text-xs font-mono">
                <span class="flex items-center gap-1.5"><span class="text-slate-400 font-sans font-semibold text-[10px] uppercase">CPU</span><span class="font-bold tabular-nums" :class="usageText(n.state.system.cpu_percent)">{{ pct(n.state.system.cpu_percent) }}</span></span>
                <span class="w-px h-3 bg-slate-300 dark:bg-slate-700"></span>
                <span class="flex items-center gap-1.5"><span class="text-slate-400 font-sans font-semibold text-[10px] uppercase">RAM</span><span class="font-bold tabular-nums" :class="usageText(n.state.system.mem_percent)">{{ pct(n.state.system.mem_percent) }}</span></span>
              </div>
            </div>

            <p v-else-if="nodeHealth(n) === 'offline'" class="text-xs text-slate-400 dark:text-slate-500">Not advertised on the network any more.</p>
          </div>
          </div>
        </div>

        <!-- ==================== Sources (right 2 cols) ==================== -->
        <div class="xl:col-span-2 space-y-4">
          <div class="flex items-center justify-between">
            <h2 class="text-base font-bold text-slate-900 dark:text-slate-100 tracking-tight flex items-center gap-2">
              <span>Discovered NDI Sources</span>
              <span class="text-xs bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-semibold px-2 py-0.5 rounded-full">{{ sources.length }}</span>
            </h2>
            <button
              @click="stopSelectedRx"
              :disabled="selectedCount === 0"
              class="text-xs font-medium text-slate-500 dark:text-slate-400 hover:text-[#C7000A] transition disabled:opacity-40"
            >
              Stop receivers on selected
            </button>
          </div>

          <p class="text-xs text-slate-500 dark:text-slate-400">
            {{ selectedCount === 0 ? 'Tick the nodes on the left, then choose a source to show on them.' : `${selectedCount} node${selectedCount === 1 ? '' : 's'} selected.` }}
          </p>

          <div v-if="sources.length === 0" class="bg-white dark:bg-slate-900 border border-dashed border-slate-300 dark:border-slate-800 rounded-2xl p-12 text-center space-y-2 shadow-sm">
            <p class="text-sm font-medium text-slate-600 dark:text-slate-400">No NDI sources detected.</p>
            <p class="text-xs text-slate-400 dark:text-slate-500">Ensure NDI senders (OBS, vMix, cameras, or a node's TX) are active on the same network as a node.</p>
          </div>

          <div v-else class="grid grid-cols-1 gap-4">
            <div
              v-for="s in sources"
              :key="s.name"
              class="bg-white dark:bg-slate-900 border rounded-2xl p-5 flex flex-col justify-between space-y-4 transition shadow-sm"
              :class="displayingOn(nodes, s.name).length ? 'border-[#C7000A] bg-red-50/40 dark:bg-[#C7000A]/10 ring-2 ring-[#C7000A]/30' : 'border-slate-200 dark:border-slate-800 hover:border-slate-300 dark:hover:border-slate-700'"
            >
              <div>
                <div class="flex items-center justify-between mb-1.5">
                  <span class="text-xs font-mono text-[#C7000A] font-bold tracking-wide uppercase truncate">{{ s.host_name || 'Host' }}</span>
                  <span v-if="displayingOn(nodes, s.name).length" class="px-2 py-0.5 bg-[#C7000A] text-white text-[11px] rounded-full font-bold whitespace-nowrap">
                    On {{ displayingOn(nodes, s.name).length }} node{{ displayingOn(nodes, s.name).length === 1 ? '' : 's' }}
                  </span>
                </div>
                <h3 class="text-base font-bold text-slate-900 dark:text-slate-100 break-all">{{ s.name }}</h3>
                <p v-if="s.stream_name" class="text-xs text-slate-500 dark:text-slate-400 mt-0.5">Stream: {{ s.stream_name }}</p>
                <p class="text-xs text-slate-400 dark:text-slate-500 mt-2">
                  Seen by {{ s.seenBy.length }} node{{ s.seenBy.length === 1 ? '' : 's' }}
                  <span v-if="displayingOn(nodes, s.name).length"> · on {{ displayingOn(nodes, s.name).map(nameOf).join(', ') }}</span>
                </p>
              </div>
              <button
                @click="showOn(s.name)"
                :disabled="selectedCount === 0"
                class="py-2 px-3 bg-[#C7000A] hover:bg-[#b00009] text-white rounded-xl text-xs font-semibold transition shadow-md shadow-[#C7000A]/20 disabled:opacity-40 disabled:cursor-not-allowed disabled:shadow-none"
              >
                Show on {{ selectedCount === 0 ? 'selected nodes' : `${selectedCount} selected node${selectedCount === 1 ? '' : 's'}` }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Toasts -->
    <div class="fixed bottom-4 right-4 z-40 space-y-2 w-[min(24rem,calc(100vw-2rem))]" aria-live="polite">
      <div
        v-for="t in toasts"
        :key="t.id"
        @click="dismiss(t.id)"
        class="rounded-xl border px-4 py-3 shadow-lg cursor-pointer text-sm bg-white dark:bg-slate-900"
        :class="t.kind === 'err' ? 'border-rose-300 dark:border-rose-800' : t.kind === 'ok' ? 'border-emerald-300 dark:border-emerald-800' : 'border-slate-200 dark:border-slate-700'"
      >
        <p class="font-semibold" :class="t.kind === 'err' ? 'text-rose-700 dark:text-rose-400' : t.kind === 'ok' ? 'text-emerald-700 dark:text-emerald-400' : 'text-slate-700 dark:text-slate-200'">{{ t.title }}</p>
        <ul v-if="t.lines.length" class="mt-1 text-xs text-slate-600 dark:text-slate-300 space-y-0.5 break-words">
          <li v-for="(l, i) in t.lines" :key="i">{{ l }}</li>
        </ul>
      </div>
    </div>
  </div>
</template>
