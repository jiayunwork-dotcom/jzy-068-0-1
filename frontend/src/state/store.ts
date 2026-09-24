import type { CellState, Envelope, SheetState, UserState } from './types'

// Store is a tiny external store (subscribe/getSnapshot) holding the live
// workbook, presence and connection state. React subscribes via
// useSyncExternalStore.

export interface Notice {
  id: number
  text: string
  tone: 'conflict' | 'info'
}

interface SheetData {
  name: string
  rows: number
  cols: number
  // cells keyed by `${col},${row}`
  cells: Map<string, CellState>
}

interface State {
  connected: boolean
  version: number
  sheets: SheetData[]
  activeSheet: number
  users: UserState[]
  notices: Notice[]
  canUndo: boolean
  canRedo: boolean
  nameInput: string
}

const key = (col: number, row: number) => `${col},${row}`

class Store {
  private state: State = {
    connected: false,
    version: 0,
    sheets: [],
    activeSheet: 0,
    users: [],
    notices: [],
    canUndo: false,
    canRedo: false,
    nameInput: localStorage.getItem('cs-name') || randomName(),
  }
  private listeners = new Set<() => void>()
  private noticeId = 1

  subscribe = (fn: () => void) => {
    this.listeners.add(fn)
    return () => this.listeners.delete(fn)
  }
  getState = () => this.state

  private set(patch: Partial<State>) {
    this.state = { ...this.state, ...patch }
    this.listeners.forEach((l) => l())
  }

  setName(name: string) {
    localStorage.setItem('cs-name', name)
    this.set({ nameInput: name })
  }

  setConnected(v: boolean) {
    if (this.state.connected !== v) this.set({ connected: v })
  }

  activeSheetData(): SheetData | undefined {
    return this.state.sheets[this.state.activeSheet]
  }

  setActiveSheet(i: number) {
    if (i !== this.state.activeSheet) this.set({ activeSheet: i })
  }

  // ---- inbound protocol -------------------------------------------------

  handleSnapshot(msg: Envelope) {
    const sheets = (msg.sheets || []).map((s: SheetState) => this.toSheetData(s))
    this.set({
      sheets,
      version: msg.version || 0,
      users: msg.users || [],
      activeSheet: Math.min(this.state.activeSheet, Math.max(0, sheets.length - 1)),
    })
  }

  applyChanges(sheetName: string | undefined, changes: CellState[]) {
    if (!changes.length) return
    const sheets = this.state.sheets.map((s) => {
      if (s.name !== sheetName) return s
      const cells = new Map(s.cells)
      for (const ch of changes) {
        if (ch.kind === 'blank' && !ch.input) {
          cells.delete(key(ch.col, ch.row))
        } else {
          cells.set(key(ch.col, ch.row), ch)
        }
      }
      return { ...s, cells }
    })
    this.set({ sheets, version: this.state.version + 1 })
  }

  applyStructure(msg: Envelope) {
    // Row/column surgery moves or deletes many cells at once. The server's
    // structureDone enumerates every non-empty cell of the target sheet after
    // the edit, so rebuild that sheet's cell map from the payload: otherwise a
    // deleted or moved-away cell could linger client-side.
    const sheets = this.state.sheets.map((s) => {
      if (s.name !== msg.sheet) return s
      const cells = new Map<string, CellState>()
      for (const ch of msg.changes || []) {
        if (ch.kind !== 'blank' || ch.input) {
          cells.set(key(ch.col, ch.row), ch)
        }
      }
      return {
        ...s,
        rows: msg.rows && msg.rows > 0 ? msg.rows : s.rows,
        cols: msg.cols && msg.cols > 0 ? msg.cols : s.cols,
        cells,
      }
    })
    this.set({ sheets, version: msg.version || this.state.version + 1 })
  }

  setPresence(users: UserState[]) {
    this.set({ users })
  }

  setUndoState(canUndo: boolean, canRedo: boolean) {
    this.set({ canUndo, canRedo })
  }

  pushNotice(text: string, tone: Notice['tone'] = 'info') {
    const id = this.noticeId++
    const notices = [...this.state.notices, { id, text, tone }]
    this.set({ notices })
    setTimeout(() => this.dismissNotice(id), 6000)
  }

  dismissNotice(id: number) {
    this.set({ notices: this.state.notices.filter((n) => n.id !== id) })
  }

  private toSheetData(s: SheetState): SheetData {
    const cells = new Map<string, CellState>()
    for (const c of s.cells) cells.set(key(c.col, c.row), c)
    return { name: s.name, rows: s.rows, cols: s.cols, cells }
  }
}

function randomName(): string {
  const adjs = ['敏捷', '冷静', '机智', '勇敢', '细致']
  const anims = ['熊猫', '狐狸', '海豚', '猎鹰', '水獭']
  return (
    adjs[Math.floor(Math.random() * adjs.length)] +
    anims[Math.floor(Math.random() * anims.length)] +
    Math.floor(Math.random() * 90 + 10)
  )
}

export const store = new Store()
