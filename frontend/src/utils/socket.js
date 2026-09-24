// socket.js wraps a WebSocket with automatic reconnect, message delivery and
// offline send queueing. The same client id is reused across reconnects so the
// server recognizes the participant; after reconnecting, the server pushes a
// fresh snapshot that catches up everything missed while offline, after which
// queued local edits are flushed (they still go through last-writer-wins).
export class SheetSocket {
  constructor({ url, hello, onMessage, onStatus }) {
    this.url = url
    this.hello = hello
    this.onMessage = onMessage
    this.onStatus = onStatus
    this.ws = null
    this.connected = false
    this.synced = false // true only after a snapshot has been received
    this.queue = []
    this.retry = 500
    this.closed = false
    this.connect()
  }

  connect() {
    if (this.closed) return
    this.onStatus?.('connecting')
    const ws = new WebSocket(this.url)
    this.ws = ws

    ws.onopen = () => {
      this.connected = true
      this.synced = false
      this.retry = 500
      this.onStatus?.('connected')
      this._rawSend(this.hello) // re-greet; snapshot will follow
    }
    ws.onmessage = (ev) => {
      let msg
      try {
        msg = JSON.parse(ev.data)
      } catch {
        return
      }
      if (msg.type === 'snapshot') {
        this.synced = true
        this.flush()
      }
      this.onMessage?.(msg)
    }
    ws.onclose = () => {
      this.connected = false
      this.synced = false
      if (this.closed) return
      this.onStatus?.('offline')
      setTimeout(() => this.connect(), this.retry)
      this.retry = Math.min(this.retry * 2, 5000)
    }
    ws.onerror = () => ws.close()
  }

  _rawSend(obj) {
    this.ws.send(JSON.stringify(obj))
  }

  // Normal application sends are queued until the post-reconnect snapshot
  // has been applied, preventing stale local edits from racing the catch-up.
  send(obj) {
    if (this.connected && this.synced) {
      this._rawSend(obj)
    } else {
      this.queue.push(obj)
    }
  }

  flush() {
    const q = this.queue
    this.queue = []
    for (const m of q) this._rawSend(m)
  }

  close() {
    this.closed = true
    this.ws?.close()
  }
}
