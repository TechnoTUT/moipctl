// Drives the real UI in headless Chrome over the DevTools protocol and checks what a person would see.
//   node check.mjs <moipctl base URL> <output directory for the screenshots>
// moipctl must run with the fake nodes of fakenodes.py attached and named as in run.sh.
import { spawn } from 'node:child_process'
import fs from 'node:fs'
const [base, out] = process.argv.slice(2)
const port = process.env.CHROME_PORT || '9333'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
fs.mkdirSync(out, { recursive: true })
fs.rmSync(`${out}/profile`, { recursive: true, force: true })
const chrome = spawn(process.env.CHROME || 'google-chrome', ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars', `--user-data-dir=${out}/profile`, `--remote-debugging-port=${port}`, '--window-size=1500,1150', 'about:blank'], { stdio: 'ignore' })
process.on('exit', () => chrome.kill())
let ws
for (let i = 0; i < 60 && !ws; i++) {
  try { const t = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()).find((x) => x.type === 'page'); if (t) ws = new WebSocket(t.webSocketDebuggerUrl) } catch {}
  await sleep(250)
}
if (ws.readyState !== 1) await new Promise((r) => (ws.onopen = r)) // it may already be open
let id = 0; const pending = new Map()
ws.onmessage = (m) => { const d = JSON.parse(m.data); if (d.id && pending.has(d.id)) { pending.get(d.id)(d); pending.delete(d.id) } }
const send = (method, params = {}) => new Promise((res) => { const i = ++id; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })) })
const ev = async (expression) => { const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (r.result.exceptionDetails) throw new Error(JSON.stringify(r.result.exceptionDetails.exception)); return r.result.result.value }
const shot = async (name) => { const r = await send('Page.captureScreenshot', { format: 'png' }); fs.writeFileSync(`${out}/${name}.png`, Buffer.from(r.result.data, 'base64')) }
let failures = 0
const check = (ok, what) => { console.log(`${ok ? 'PASS' : 'FAIL'}  ${what}`); if (!ok) failures++ }
const logs = []
ws.addEventListener('message', (m) => { const d = JSON.parse(m.data); if (d.method === 'Log.entryAdded') logs.push(d.params.entry) })
await send('Page.enable'); await send('Runtime.enable'); await send('Log.enable')
await send('Page.navigate', { url: base }); await sleep(3500)

// the checkbox label is stable while a card's title is being edited
const card = (name) => `[...document.querySelectorAll('input[type=checkbox]')].find(c => c.getAttribute('aria-label') === ${JSON.stringify('Select ' + name)}).closest('div.rounded-2xl')`
const text = (name) => ev(`${card(name)}.innerText`)

check((await ev(`document.title`)) === 'MoIP Controller', 'page title')
check((await ev(`document.querySelectorAll('script:not([src])').length`)) === 0, 'the page has no inline script')
check((await ev(`document.body.innerText.includes('Live')`)), 'shows Live (event stream is up)')
check((await ev(`document.querySelectorAll('input[type=checkbox]').length`)) === 6, 'six nodes listed')
check((await ev(`${card('127.0.0.1:19006')}.querySelector('input[type=checkbox]').disabled`)), 'an unreachable node cannot be selected')
check((await ev(`${card('127.0.0.1:19006')}.innerText.toLowerCase().includes('cannot reach')`)), 'an unreachable node says moipctl cannot reach it')
check((await ev(`document.querySelector('button.disabled\\\\:opacity-40, button[disabled]') !== null`)), 'source buttons are disabled with nothing selected')
await shot('1-initial')

// select Booth Monitor (running: will be switched) and Entrance Signage (stopped: will be started), then show VJ on them
await ev(`${card('Booth Monitor')}.querySelector('input[type=checkbox]').click()`)
await ev(`${card('Entrance Signage')}.querySelector('input[type=checkbox]').click()`)
await sleep(300)
check((await ev(`document.body.innerText.includes('2 nodes selected')`)), 'shows 2 nodes selected')
await ev(`[...document.querySelectorAll('h3')].find(h => h.textContent.trim() === 'VJ (Resolume Arena)').closest('div.rounded-2xl').querySelector('button').click()`)
await sleep(3500)
const booth = await text('Booth Monitor'), ent = await text('Entrance Signage')
check(booth.includes('VJ (Resolume Arena)') && booth.includes('CONNECTED'), 'Booth Monitor switched to VJ and is connected')
check(ent.includes('VJ (Resolume Arena)') && ent.includes('CONNECTED'), 'Entrance Signage started on VJ and is connected')
const toastText = await ev(`document.querySelector('[aria-live]').innerText`)
check(toastText.includes('Showing "VJ (Resolume Arena)"') && toastText.includes('2 nodes'), `toast reports the result (${JSON.stringify(toastText)})`)
check((await ev(`document.body.innerText.includes('On 3 nodes')`)), 'the VJ source card now says it is shown on 3 nodes (with Stage Left)')
await shot('2-after-show')

// stop one receiver from its card
await ev(`[...${card('Booth Monitor')}.querySelectorAll('button')].find(b => b.textContent.trim() === 'Stop').click()`)
await sleep(1500)
check((await text('Booth Monitor')).includes('STOPPED'), 'Stop on a card stops that receiver')

// rename
await ev(`${card('Stage Right')}.querySelector('button[title=Rename]').click()`); await sleep(200)
await ev(`(() => { const i = ${card('Stage Right')}.querySelector('input[placeholder]'); i.value = 'Lobby Screen'; i.dispatchEvent(new Event('input')); i.dispatchEvent(new KeyboardEvent('keyup', { key: 'Enter' })) })()`)
await sleep(1200)
check((await ev(`document.body.innerText.includes('Lobby Screen')`)), 'renaming shows the new name')
const alias = await ev(`fetch('/api/nodes/' + encodeURIComponent('static:127.0.0.1:19005')).then(r => r.json()).then(n => n.alias)`)
check(alias === 'Lobby Screen', `the name was saved by the API (${alias})`)

// XSS: a hostile name must be shown as text, never run
await ev(`fetch('/api/nodes/' + encodeURIComponent('static:127.0.0.1:19003') + '/alias', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ alias: '<img src=x onerror=window.__xss=1>' }) })`)
await sleep(1500)
check((await ev(`window.__xss === undefined`)), 'a hostile name did not execute')
check((await ev(`document.body.innerText.includes('<img src=x onerror=window.__xss=1>')`)), 'the hostile name is shown literally, as text')
check((await ev(`document.querySelectorAll('img[src=x]').length`)) === 0, 'no <img> was created from it')

// dark mode, remembered across a reload
await ev(`document.querySelector('button[title="Toggle Light / Dark Mode"]').click()`); await sleep(500)
check((await ev(`document.documentElement.classList.contains('dark')`)), 'the theme toggle switches to dark')
await shot('3-dark')
await send('Page.reload'); await sleep(2500)
check((await ev(`document.documentElement.classList.contains('dark')`)), 'dark is still on after a reload (saved choice)')

// the policy must not have blocked anything the UI needs
const csp = logs.filter((e) => /content security policy|refused to/i.test(e.text || ''))
check(csp.length === 0, `no Content-Security-Policy violation in the console${csp.length ? ': ' + csp.map((e) => e.text).join(' | ') : ''}`)

console.log(failures === 0 ? '\nall UI checks passed' : `\n${failures} UI check(s) FAILED`)
process.exit(failures === 0 ? 0 : 1)
