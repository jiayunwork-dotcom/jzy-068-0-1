import { useEffect, useRef } from 'react'
import { ROW_H, COL_W } from '../utils/grid'

// Cell renders one grid square. When isEditing it becomes a controlled text
// input whose text lives in the parent (shared with the formula bar).
export default function Cell({
  col,
  row,
  data,
  selected,
  isEditing,
  editText,
  editActive,
  fromBar,
  onEditText,
  picked,
  clearPicked,
  onClick,
  onEdit,
  onCommit,
  endEdit,
  commitAndMove,
  cancelEdit,
}) {
  const inputRef = useRef(null)
  const doneRef = useRef(false)

  useEffect(() => {
    if (isEditing && !fromBar) {
      doneRef.current = false
      requestAnimationFrame(() => {
        const el = inputRef.current
        if (!el) return
        el.focus()
        const pos = el.value.length
        el.setSelectionRange(pos, pos)
      })
    }
  }, [isEditing, fromBar])

  // Another cell clicked while editing a formula: insert its address at caret.
  useEffect(() => {
    if (isEditing && picked) {
      const el = inputRef.current
      const pos = el ? el.selectionStart ?? editText.length : editText.length
      const next = editText.slice(0, pos) + picked + editText.slice(pos)
      onEditText(next)
      requestAnimationFrame(() => {
        if (el) {
          el.focus()
          const np = pos + picked.length
          el.setSelectionRange(np, np)
        }
      })
      clearPicked?.()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [picked])

  // finish paths send exactly one edit:
  //  - Enter/Tab: commitAndMove sends once and moves the selection;
  //  - blur (click outside the grid): onCommit sends once.
  const commitHere = () => {
    if (doneRef.current) return
    doneRef.current = true
    onCommit(editText)
    endEdit()
  }

  const commitMove = (dc, dr) => {
    if (doneRef.current) return
    doneRef.current = true
    commitAndMove?.(editText, dc, dr) // parent sends once, closes edit, moves
  }

  const onKeyDown = (e) => {
    e.stopPropagation()
    if (e.key === 'Enter') {
      e.preventDefault()
      commitMove(0, e.shiftKey ? -1 : 1)
    } else if (e.key === 'Tab') {
      e.preventDefault()
      commitMove(e.shiftKey ? -1 : 1, 0)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      cancelEdit()
    }
  }

  const isFormula = typeof editText === 'string' && editText.startsWith('=')
  const shown = data?.value ?? ''
  const kind = data?.kind
  const error = data?.error || data?.cycle

  let align = 'left'
  if (kind === 'number' || kind === 'bool') align = 'right'
  if (error) align = 'center'

  return (
    <div
      className={
        'cell' +
        (selected ? ' selected' : '') +
        (error ? ' error' : '') +
        (data?.cycle ? ' cycle' : '')
      }
      style={{
        position: 'absolute',
        left: col * COL_W,
        top: row * ROW_H,
        width: COL_W,
        height: ROW_H,
      }}
      onMouseDown={(e) => {
        // While editing anywhere, prevent blur-submit: formula clicks insert a
        // reference; plain clicks commit-then-select through handleCellClick.
        if (editActive) e.preventDefault()
        onClick()
      }}
      onDoubleClick={() => onEdit()}
      title={error ? shown : undefined}
    >
      {isEditing ? (
        <input
          ref={inputRef}
          className={'cell-input' + (isFormula ? ' formula' : '')}
          value={editText}
          onChange={(e) => onEditText(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={commitHere}
          spellCheck={false}
        />
      ) : (
        <span className="cell-text" style={{ textAlign: align }}>
          {shown}
        </span>
      )}
    </div>
  )
}
