import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ROW_H,
  ROW_HEADER_W,
  COL_W,
  COL_HEADER_H,
  colName,
  addrOf,
} from '../utils/grid'
import Cell from './Cell'
import PeerCursors from './PeerCursors'

// VirtualGrid renders only the visible slice of the sheet (windowing).
export default function VirtualGrid({
  sheet,
  me,
  presence,
  selection,
  onSelect,
  cellAt,
  onCommitCell,
  endEdit,
  commitAndMove,
  editing,
  beginEdit,
  updateEditText,
  clearPicked,
  cancelEdit,
}) {
  const scrollRef = useRef(null)
  const [scroll, setScroll] = useState({ top: 0, left: 0 })
  const [viewport, setViewport] = useState({ w: 800, h: 600 })

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const update = () => setViewport({ w: el.clientWidth, h: el.clientHeight })
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const onScroll = (e) => setScroll({ top: e.target.scrollTop, left: e.target.scrollLeft })

  const rows = sheet?.rows || 0
  const cols = sheet?.cols || 26

  const overscan = 6
  const startRow = Math.max(0, Math.floor(scroll.top / ROW_H) - overscan)
  const endRow = Math.min(rows, Math.ceil((scroll.top + viewport.h) / ROW_H) + overscan)
  const startCol = Math.max(0, Math.floor(scroll.left / COL_W) - overscan)
  const endCol = Math.min(cols, Math.ceil((scroll.left + viewport.w) / COL_W) + overscan)

  const visibleRows = useMemo(() => range(startRow, endRow), [startRow, endRow])
  const visibleCols = useMemo(() => range(startCol, endCol), [startCol, endCol])

  if (!sheet) return null

  const totalH = rows * ROW_H
  const totalW = cols * COL_W

  const editingRef = useRef(editing)
  editingRef.current = editing

  const handleCellClick = (c, r) => {
    const ed = editingRef.current
    if (ed) {
      // Formula composition: insert the clicked cell's reference, no commit.
      if (typeof ed.text === 'string' && ed.text.startsWith('=')) {
        beginEdit(ed.col, ed.row, ed.text, {
          picked: addrOf(c, r),
          fromBar: ed.fromBar,
        })
        return
      }
      // Plain edit: commit once (this is the single send) and move selection.
      onCommitCell(sheet.name, addrOf(ed.col, ed.row), ed.text)
      endEdit()
    }
    onSelect(sheet.name, c, r)
  }

  const moveSelection = (dc, dr) => {
    if (!selection) return
    const c = Math.max(0, Math.min(cols - 1, selection.col + dc))
    const r = Math.max(0, Math.min(rows - 1, selection.row + dr))
    onSelect(sheet.name, c, r)
  }

  const handleKeyDown = (e) => {
    if (!selection) return
    if (editing) return // editor handles its own keys

    let { col, row } = selection
    let handled = true
    if (e.key === 'ArrowDown') row++
    else if (e.key === 'ArrowUp') row--
    else if (e.key === 'ArrowLeft') col--
    else if (e.key === 'ArrowRight') col++
    else if (e.key === 'Tab') {
      e.preventDefault()
      col += e.shiftKey ? -1 : 1
    } else if (e.key === 'Enter' || e.key === 'F2') {
      beginEdit(col, row)
      return
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      onCommitCell(sheet.name, addrOf(col, row), '')
      return
    } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
      beginEdit(col, row, e.key)
      return
    } else handled = false

    if (handled) {
      e.preventDefault()
      col = Math.max(0, Math.min(cols - 1, col))
      row = Math.max(0, Math.min(rows - 1, row))
      onSelect(sheet.name, col, row)
    }
  }

  return (
    <div className="grid" ref={scrollRef} onScroll={onScroll} onKeyDown={handleKeyDown} tabIndex={0}>
      <div
        style={{
          width: ROW_HEADER_W + totalW,
          height: COL_HEADER_H + totalH,
          position: 'relative',
        }}
      >
        {/* Column headers: pinned to top; corner inside is pinned to left */}
        <div
          style={{
            position: 'sticky',
            top: 0,
            zIndex: 30,
            height: COL_HEADER_H,
            width: '100%',
            display: 'flex',
            background: '#f1f3f4',
          }}
        >
          <div style={{ position: 'sticky', left: 0, zIndex: 31, flex: '0 0 auto' }}>
            <div className="corner" style={{ width: ROW_HEADER_W, height: COL_HEADER_H }} />
          </div>
          <div style={{ position: 'relative', width: totalW, height: COL_HEADER_H, flex: '0 0 auto' }}>
            {visibleCols.map((c) => (
              <div
                key={c}
                className={'col-header' + (selection?.col === c ? ' selected' : '')}
                style={{ position: 'absolute', left: c * COL_W, top: 0, width: COL_W, height: COL_HEADER_H }}
              >
                {colName(c)}
              </div>
            ))}
          </div>
        </div>

        {/* Body: pinned row-header column + scrolling cells */}
        <div style={{ display: 'flex', position: 'relative' }}>
          <div
            style={{
              position: 'sticky',
              left: 0,
              zIndex: 20,
              width: ROW_HEADER_W,
              height: totalH,
              flex: '0 0 auto',
              background: '#f1f3f4',
            }}
          >
            {visibleRows.map((r) => (
              <div
                key={r}
                className={'row-header' + (selection?.row === r ? ' selected' : '')}
                style={{ position: 'absolute', top: r * ROW_H, height: ROW_H, width: ROW_HEADER_W }}
              >
                {r + 1}
              </div>
            ))}
          </div>

          {/* Cells */}
          <div
            className="cells-layer"
            style={{ position: 'relative', width: totalW, height: totalH, flex: '0 0 auto' }}
          >
            {visibleRows.map((r) =>
              visibleCols.map((c) => (
                <Cell
                  key={c + ':' + r}
                  col={c}
                  row={r}
                  data={cellAt(sheet.name, c, r)}
                  selected={selection?.sheet === sheet.name && selection.col === c && selection.row === r}
                  isEditing={!!editing && editing.col === c && editing.row === r}
                  editText={editing && editing.col === c && editing.row === r ? editing.text : ''}
                  editActive={!!editing}
                  fromBar={!!editing?.fromBar && editing.col === c && editing.row === r}
                  onEditText={updateEditText}
                  picked={editing && editing.col === c && editing.row === r ? editing.picked : undefined}
                  clearPicked={clearPicked}
                  onClick={() => handleCellClick(c, r)}
                  onEdit={() => beginEdit(c, r)}
                  onCommit={(raw) => onCommitCell(sheet.name, addrOf(c, r), raw)}
                  endEdit={endEdit}
                  commitAndMove={(raw, dc, dr) => commitAndMove(sheet.name, c, r, raw, dc, dr)}
                  cancelEdit={cancelEdit}
                  onMoveSelection={moveSelection}
                />
              )),
            )}
          </div>
        </div>

        <PeerCursors
          me={me}
          presence={presence}
          sheetName={sheet.name}
          width={totalW}
          height={totalH}
        />
      </div>
    </div>
  )
}

function range(a, b) {
  const out = []
  for (let i = a; i < b; i++) out.push(i)
  return out
}
