import { useEffect, useRef } from 'react'
import { addrOf } from '../utils/grid'

// FormulaBar is the fx input above the grid. It is the single source of
// editing text together with the in-cell editor: both bind to editing.text.
// While editing here, clicking a grid cell appends its reference (the grid's
// handleCellClick does that whenever text starts with '=').
export default function FormulaBar({ selection, cellData, editing, onStartEdit, onEditText, onCommit, onCancel }) {
  const addr = selection ? addrOf(selection.col, selection.row) : ''
  const inputRef = useRef(null)

  // Keep formula bar focused caret when the in-cell editor drives changes.
  useEffect(() => {}, [editing?.text])

  const active = !!editing
  const value = active ? editing.text : cellData?.raw ?? ''

  const commit = () => onCommit(editing.text)

  return (
    <div className="formula-bar">
      <div className="addr-box">{active ? addrOf(editing.col, editing.row) : addr}</div>
      <div className="fx">fx</div>
      <input
        ref={inputRef}
        className={'fx-input' + (active ? ' active' : '')}
        value={value}
        placeholder="输入数字、文字，或以 = 开头的公式（编辑公式时点表格可引用单元格）"
        onFocus={() => {
          if (selection && !active) onStartEdit(selection.col, selection.row, cellData?.raw ?? '')
        }}
        onChange={(e) => {
          if (!active && selection) onStartEdit(selection.col, selection.row, e.target.value)
          else onEditText(e.target.value)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            if (active) commit()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            onCancel()
          }
        }}
        spellCheck={false}
      />
      {active && (
        <div className="fx-actions">
          <button
            className="btn btn-primary"
            onMouseDown={(e) => {
              e.preventDefault()
              commit()
            }}
          >
            ✓ 提交
          </button>
          <button
            className="btn"
            onMouseDown={(e) => {
              e.preventDefault()
              onCancel()
            }}
          >
            ✕ 取消
          </button>
        </div>
      )}
    </div>
  )
}
