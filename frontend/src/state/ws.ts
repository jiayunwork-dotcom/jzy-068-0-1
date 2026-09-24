import type { Envelope } from './types'
import { store } from './store'

// clientId survives reloads: on reconnect the server restores our name,
// color and undo history.
const CLIENT_ID =
  localStorage.getItem('cs-client-id') ||
  (() => {
    const id =
      Math.random().toString(16).slice(2) + Date.now().toString(16)
    localStorage.setItem('cs-client-id', id)
    return id
  })()

let ws: WebSocket | null = null
let stopped = false
let reconnectDelay = 300

// Commands sent while offline are queued and flushed after the hello
// handshake on reconnect, so locally-made (but unsent) edits get pushed up
// and still go through last-writer-wins arbitration.
const pending: Envelope[] = []

export function getClientId() {
  return CLIENT_ID
}

function wsURL(): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws`
}

function connect() {
  if (stopped) return
  const sock = new WebSocket(wsURL())
  ws = sock

  sock.onopen = () => {
    reconnectDelay = 300
    store.setConnected(true)
    send({
      type: 'hello',
      clientId: CLIENT_ID,
      name: store.getState().nameInput,
    })
    // Flush edits made while disconnected.
    while (pending.length) {
      const m = pending.shift()!
      sock.send(JSON.stringify(m))
    }
  }

  sock.onclose = () => {
    store.setConnected(false)
    ws = null
    if (!stopped) setTimeout(connect, reconnectDelay)
    reconnectDelay = Math.min(reconnectDelay * 2, 5000)
  }

  sock.onerror = () => {
    sock.close()
  }

  sock.onmessage = (ev) => {
    let msg: Envelope
    try {
      msg = JSON.parse(ev.data)
    } catch {
      return
    }
    dispatch(msg)
  }
}

function dispatch(msg: Envelope) {
  switch (msg.type) {
    case 'snapshot':
      store.handleSnapshot(msg)
      break
    case 'changes':
      store.applyChanges(msg.sheet, msg.changes || [])
      break
    case 'structureDone':
      store.applyStructure(msg)
      break
    case 'presence':
      store.setPresence(msg.presence || [])
      break
    case 'undoState':
      store.setUndoState(!!msg.canUndo, !!msg.canRedo)
      break
    case 'overwritten':
      store.pushNotice(
        `你在 ${msg.sheet} 的单元格刚被「${msg.winner}」覆盖为：${msg.newValue || '(空)'}（最后写入胜出）`,
        'conflict',
      )
      break
    case 'error':
      store.pushNotice(msg.message || '操作被服务器拒绝', 'info')
      break
  }
}

// send enqueues a client->server message. When offline it is parked and
// delivered after reconnect.
export function send(msg: Envelope) {
  if (msg.type !== 'hello') msg.clientId = CLIENT_ID
  if (!msg.msgId && msg.type !== 'cursor' && msg.type !== 'editing') {
    msg.msgId = Math.random().toString(16).slice(2)
  }
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(msg))
  } else {
    // Only durable mutations queue; transient presence doesn't.
    if (msg.type === 'setCell' || msg.type === 'structure' ||
        msg.type === 'undo' || msg.type === 'redo') {
      pending.push(msg)
    }
  }
}

export function start() {
  stopped = false
  connect()
}
