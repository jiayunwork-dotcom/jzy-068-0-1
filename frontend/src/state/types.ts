// Wire + view model types shared by the frontend.

export interface CellState {
  col: number
  row: number
  input: string
  isFormula: boolean
  display: string
  kind: 'blank' | 'number' | 'string' | 'boolean' | 'error'
  num?: number
  str?: string
  bool?: boolean
  error?: string
  circular?: boolean
}

export interface SheetState {
  name: string
  rows: number
  cols: number
  cells: CellState[]
}

export interface UserState {
  id: string
  name: string
  color: string
  sheet?: string
  col: number
  row: number
  hasCursor: boolean
  editSheet?: string
  editCol: number
  editRow: number
  editing: boolean
}

export interface Envelope {
  type: string
  clientId?: string
  name?: string
  lastVersion?: number
  msgId?: string
  sheet?: string
  col?: number
  row?: number
  value?: string
  kind?: string
  index?: number
  rows?: number
  cols?: number
  version?: number
  sheets?: SheetState[]
  changes?: CellState[]
  users?: UserState[]
  presence?: UserState[]
  canUndo?: boolean
  canRedo?: boolean
  message?: string
  editing?: boolean
  winner?: string
  oldValue?: string
  newValue?: string
  cell?: string
}

// colLetters maps a 0-based column to A..Z, AA...
export function colLetters(col: number): string {
  let s = ''
  let c = col
  while (true) {
    s = String.fromCharCode(65 + (c % 26)) + s
    c = Math.floor(c / 26) - 1
    if (c < 0) break
  }
  return s
}

export function parseCol(letters: string): number {
  let c = 0
  for (const ch of letters.toUpperCase()) {
    c = c * 26 + (ch.charCodeAt(0) - 64)
  }
  return c - 1
}

// cellAddress renders A1 style 1-based coordinates.
export function cellAddress(col: number, row: number): string {
  return `${colLetters(col)}${row + 1}`
}
