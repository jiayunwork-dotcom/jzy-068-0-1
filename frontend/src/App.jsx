import { useEffect, useRef, useState } from 'react'
import { useCollabSheet } from './hooks/useCollabSheet'
import VirtualGrid from './components/VirtualGrid'
import Toolbar from './components/Toolbar'
import FormulaBar from './components/FormulaBar'
import PresencePanel from './components/PresencePanel'
import { addrOf } from './utils/grid'
import './styles.css'

export default function App() {
  const sheet = useCollabSheet()
  // editing = { col, row, text, picked, fromBar } while this user composes.
  const [editing, setEditing] = useState(null)
  const editingRef = useRef(null)
  editingRef.current = editing

  const selectedCellData =
    sheet.selection && sheet.activeSheetData
      ? sheet.cellAt(sheet.activeSheet, sheet.selection.col, sheet.selection.row)
      : null

  const beginEdit = (col, row, text, extra = {}) => {
    const initial =
      text !== undefined ? text : sheet.cellAt(sheet.activeSheet, col, row)?.raw ?? ''
    setEditing({ col, row, text: initial, ...extra })
  }

  const updateEditText = (text) => setEditing((e) => (e ? { ...e, text } : e))
  const clearPicked = () => setEditing((e) => (e && e.picked ? { ...e, picked: undefined } : e))
  const cancelEdit = () => setEditing(null)

  // commitCurrent sends exactly one edit for the active cell and ends editing.
  // Called from every commit path (Enter/Tab/blur/formula bar/click away),
  // guarded so Enter-then-move cannot record the same edit twice.
  const commitCurrent = (rawOverride) => {
    const e = editingRef.current
    if (!e || !sheet.activeSheet) return
    const raw = rawOverride !== undefined ? rawOverride : e.text
    sheet.commitEdit(sheet.activeSheet, addrOf(e.col, e.row), raw)
    editingRef.current = null
    setEditing(null)
  }

  const select = (sheetName, col, row) => {
    // Commit any in-progress edit before moving the selection.
    if (editingRef.current) commitCurrent()
    sheet.moveCursor(sheetName, col, row)
  }

  // commitAndMove is the Enter/Tab path: one server edit, then move.
  const commitAndMove = (sheetName, col, row, raw, dc, dr) => {
    const nc = Math.max(0, col + dc)
    const nr = Math.max(0, row + dr)
    sheet.commitEdit(sheetName, addrOf(col, row), raw)
    editingRef.current = null
    setEditing(null)
    sheet.moveCursor(sheetName, nc, nr)
  }

  // Global Ctrl/Cmd+Z / Ctrl+Shift+Z (per-user independence enforced server side).
  useEffect(() => {
    const onKey = (e) => {
      const k = e.key.toLowerCase()
      if ((e.ctrlKey || e.metaKey) && k === 'z') {
        e.preventDefault()
        editingRef.current = null
        setEditing(null)
        if (e.shiftKey) sheet.redo()
        else sheet.undo()
      } else if ((e.ctrlKey || e.metaKey) && k === 'y') {
        e.preventDefault()
        editingRef.current = null
        setEditing(null)
        sheet.redo()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [sheet])

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">协作电子表格 · CollabSheet</div>
      </header>
      <Toolbar
        workbook={sheet.workbook}
        activeSheet={sheet.activeSheet}
        onSelectSheet={(name) => {
          if (editingRef.current) commitCurrent()
          sheet.setActiveSheet(name)
        }}
        onStruct={(kind, s, index) => {
          editingRef.current = null
          setEditing(null)
          sheet.structOp(kind, s, index)
        }}
        onUndo={() => {
          editingRef.current = null
          setEditing(null)
          sheet.undo()
        }}
        onRedo={() => {
          editingRef.current = null
          setEditing(null)
          sheet.redo()
        }}
        canUndo={sheet.canUndo}
        canRedo={sheet.canRedo}
        status={sheet.status}
        selection={sheet.selection}
      />
      <FormulaBar
        selection={sheet.selection}
        cellData={selectedCellData}
        editing={editing}
        onStartEdit={(c, r, t) => beginEdit(c, r, t, { fromBar: true })}
        onEditText={updateEditText}
        onCommit={() => commitCurrent()}
        onCancel={cancelEdit}
      />
      <div className="main">
        <VirtualGrid
          sheet={sheet.activeSheetData}
          me={sheet.me}
          presence={sheet.presence}
          selection={sheet.selection}
          onSelect={select}
          cellAt={sheet.cellAt}
          onCommitCell={(s, a, raw) => sheet.commitEdit(s, a, raw)}
          endEdit={() => {
            editingRef.current = null
            setEditing(null)
          }}
          commitAndMove={commitAndMove}
          editing={editing}
          beginEdit={beginEdit}
          updateEditText={updateEditText}
          clearPicked={clearPicked}
          cancelEdit={cancelEdit}
        />
        <PresencePanel
          presence={sheet.presence}
          me={sheet.me}
          onSaveName={sheet.saveName}
          displayName={sheet.displayName}
        />
      </div>
      {sheet.notice && (
        <div className={'notice notice-' + sheet.notice.kind}>{sheet.notice.text}</div>
      )}
    </div>
  )
}
