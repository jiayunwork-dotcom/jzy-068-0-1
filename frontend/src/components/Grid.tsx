import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSyncExternalStore } from 'react'
import { store } from '../state/store'
import { send, getClientId } from '../state/ws'
import { cellAddress, colLetters } from '../state/types'
import type { CellState } from '../state/types'
import { FormulaEditor, type EditPos } from './FormulaEditor'
import { CollaboratorCursors } from './CollaboratorCursors'

const ROW_H = 26
const COL_W = 96
const HEADER_W = 46
const HEADER_H = 28
const OVERSCAN = 8

interface Sel { col: number; row: number }

export function Grid() {
  useSyncExternalStore(store.subscribe, store.getState)
  const state = store.getState()
  const sheet = state.sheets[state.activeSheet]

  const scrollRef = useRef<HTMLDivElement>(null)
  const [scroll, setScroll] = useState({ top: 0, left: 0 })
  const [sel, setSel] = useState<Sel>({ col: 0, row: 0 })
  const [edit, setEdit] = useState<EditPos | null>(null)
  const [editRefs, setEditRefs] = useState<Sel[]>([])
  const clickRefBox = useRef<((col: number, row: number) => boolean) | null>(null)
  const typingRef = useRef(false)

  // Throttled scroll state for the virtual window.
  const onScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    setScroll({ top: el.scrollTop, left: el.scrollLeft })
  }, [])

  // ---- virtual window ---------------------------------------------------
  const rows = sheet?.rows ?? 0
  const cols = sheet?.cols ?? 26
  const startRow = Math.max(0, Math.floor(scroll.top / ROW_H) - OVERSCAN)
  const endRow = Math.min(
    rows,
    Math.ceil((scroll.top + (scrollRef.current?.clientHeight ?? 600)) / ROW_H) + OVERSCAN,
  )
  const startCol = Math.max(0, Math.floor(scroll.left / COL_W) - OVERSCAN)
  const endCol = Math.min(
    cols,
    Math.ceil((scroll.left + (scrollRef.current?.clientWidth ?? 900)) / COL_W) + OVERSCAN,
  )

  const visibleRows = useMemo(() => {
    const out: number[] = []
    for (let r = startRow; r < endRow; r++) out.push(r)
    return out
  }, [startRow, endRow])
  const visibleCols = useMemo(() => {
    const out: number[] = []
    for (let c = startCol; c < endCol; c++) out.push(c)
    return out
  }, [startCol, endCol])

  const cellAt = useCallback(
    (col: number, row: number): CellState | undefined =>
      sheet?.cells.get(`${col},${row}`),
    [sheet],
  )

  // ---- selection / editing ---------------------------------------------
  const publishCursor = useCallback(
    (s: Sel) => {
      send({ type: 'cursor', sheet: sheet?.name, col: s.col, row: s.row })
    },
    [sheet?.name],
  )

  const selectCell = useCallback(
    (s: Sel, publish = true) => {
      setSel(s)
      if (publish) publishCursor(s)
    },
    [publishCursor],
  )

  const beginEdit = useCallback(
    (s: Sel, initial?: string) => {
      typingRef.current = initial !== undefined
      setEdit({ ...s, initial })
      send({ type: 'editing', sheet: sheet?.name, col: s.col, row: s.row, editing: true })
    },
    [sheet?.name],
  )

  const endEdit = useCallback(
    (value: string | null) => {
      if (edit) {
        if (value !== null) {
          send({
            type: 'setCell',
            sheet: sheet?.name,
            col: edit.col,
            row: edit.row,
            value: value.trim(),
          })
        }
      }
      setEdit(null)
      setEditRefs([])
      clickRefBox.current = null
      typingRef.current = false
      send({ type: 'editing', sheet: sheet?.name, col: 0, row: 0, editing: false })
    },
    [edit, sheet?.name],
  )

  // Keep cursor position valid when switching sheets.
  useEffect(() => {
    publishCursor(sel)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sheet?.name])

  // Keyboard navigation on the grid.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (edit) return // editor handles keys
      let { col, row } = sel
      let moved = true
      let next: Sel | null = null
      switch (e.key) {
        case 'ArrowUp': next = { col, row: Math.max(0, row - 1) }; break
        case 'ArrowDown': next = { col, row: Math.min(rows - 1, row + 1) }; break
        case 'ArrowLeft': next = { col: Math.max(0, col - 1), row }; break
        case 'ArrowRight':
        case 'Tab': next = { col: Math.min(cols - 1, col + 1), row }; break
        case 'Enter': beginEdit(sel); return
        case 'Delete':
        case 'Backspace':
          send({ type: 'setCell', sheet: sheet?.name, col, row, value: '' })
          return
        case 'z':
          if (e.ctrlKey || e.metaKey) { send({ type: 'undo' }); e.preventDefault() }
          return
        case 'y':
          if (e.ctrlKey || e.metaKey) { send({ type: 'redo' }); e.preventDefault() }
          return
        default:
          // Typing a printable char starts editing with that char.
          if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
            beginEdit(sel, e.key)
            e.preventDefault()
          }
          moved = false
      }
      if (next && moved) {
        e.preventDefault()
        selectCell(next)
        ensureVisible(next)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sel, edit, rows, cols, sheet?.name])

  const ensureVisible = (s: Sel) => {
    const el = scrollRef.current
    if (!el) return
    const cellTop = HEADER_H + s.row * ROW_H
    const cellBottom = cellTop + ROW_H
    if (cellTop < el.scrollTop + HEADER_H) el.scrollTop = cellTop - HEADER_H
    if (cellBottom > el.scrollTop + el.clientHeight)
      el.scrollTop = cellBottom - el.clientHeight
    const cellLeft = HEADER_W + s.col * COL_W
    if (cellLeft < el.scrollLeft + HEADER_W) el.scrollLeft = cellLeft - HEADER_W
    if (cellLeft + COL_W > el.scrollLeft + el.clientWidth)
      el.scrollLeft = cellLeft + COL_W - el.clientWidth
  }

  if (!sheet) return <div className="grid-wrap" />

  // Editor and cursors live inside the scrolled document layer (which itself
  // begins below the sticky headers), so coordinates are relative to the
  // first cell: row*ROW_H, HEADER_W + col*COL_W.
  const editorLeft = HEADER_W + (edit?.col ?? 0) * COL_W
  const editorTop = (edit?.row ?? 0) * ROW_H

  return (
    <div className="grid-wrap" ref={scrollRef} onScroll={onScroll} tabIndex={0}>
      <div className="corner" />
      <div className="colhead-row">
        {Array.from({ length: cols }, (_, c) => (
          <div className="colhead" key={c}>
            {colLetters(c)}
          </div>
        ))}
      </div>

      <div className="grid-canvas" style={{ height: rows * ROW_H, width: HEADER_W + cols * COL_W }}>
        {visibleRows.map((r) => (
          <div
            className="body-row"
            key={r}
            style={{ position: 'absolute', top: r * ROW_H, left: 0, height: ROW_H }}
          >
            <div className="rowhead">{r + 1}</div>
            {/* spacer so virtualized columns land at the correct x */}
            <div style={{ width: startCol * COL_W, minWidth: startCol * COL_W, height: ROW_H }} />
            {visibleCols.map((c) => {
              const data = cellAt(c, r)
              const isSel = sel.col === c && sel.row === r
              const isRef = editRefs.some((p) => p.col === c && p.row === r)
              const cls =
                'cell ' +
                (data?.kind === 'error'
                  ? data.circular
                    ? 'circular'
                    : 'error'
                  : data?.kind === 'number'
                    ? 'num'
                    : '') +
                (isSel ? ' selected' : '') +
                (isRef ? ' referencing' : '')
              return (
                <div
                  key={c}
                  className={cls.trim()}
                  onClick={() => {
                    // While editing, a click is a formula point-and-click
                    // reference insert when the editor consumes it; otherwise
                    // (e.g. editing plain text) it moves the selection.
                    if (edit && clickRefBox.current) {
                      if (!clickRefBox.current(c, r)) {
                        selectCell({ col: c, row: r })
                      }
                    } else {
                      selectCell({ col: c, row: r })
                    }
                  }}
                  onDoubleClick={() => beginEdit({ col: c, row: r })}
                  title={data?.error ? data.display : undefined}
                >
                  {data ? data.display : ''}
                </div>
              )
            })}
          </div>
        ))}

        <CollaboratorCursors
          users={state.users}
          selfId={getClientId()}
          sheetName={sheet.name}
          colWidth={COL_W}
          rowHeight={ROW_H}
          headerW={HEADER_W}
          headerH={0}
        />

        {edit && (
          <FormulaEditor
            sheetName={sheet.name}
            pos={edit}
            cell={cellAt(edit.col, edit.row)}
            top={editorTop}
            left={editorLeft}
            width={COL_W + 220}
            rowHeaderWidth={HEADER_W}
            onCommit={(v) => endEdit(v)}
            onCancel={() => endEdit(null)}
            onReferences={setEditRefs}
            registerClickRef={(fn) => {
              clickRefBox.current = fn
            }}
          />
        )}
      </div>
    </div>
  )
}
