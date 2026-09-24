import { useEffect, useRef } from 'react'
import type { CellState } from '../state/types'
import { cellAddress } from '../state/types'

export interface EditPos {
  col: number
  row: number
  // when defined the editor opens with that prefix (user started typing)
  initial?: string
}

interface Props {
  sheetName: string
  pos: EditPos
  cell?: CellState
  top: number
  left: number
  width: number
  rowHeaderWidth: number
  onCommit: (value: string) => void
  onCancel: () => void
  onReferences?: (refs: { col: number; row: number }[]) => void
  // While editing a *formula*, grid clicks are routed here and return true
  // (the click inserts an address). For plain text edits the handler returns
  // false and the grid moves the selection normally.
  registerClickRef?: (fn: ((col: number, row: number) => boolean) | null) => void
}

const REF_AT_CURSOR = /(\$?[A-Z]{1,3}\$?\d+)?$/

export function FormulaEditor({
  pos, cell, top, left, width, onCommit, onCancel, onReferences, registerClickRef,
}: Props) {
  const ref = useRef<HTMLInputElement>(null)
  const finished = useRef(false)

  const emitRefs = (text: string) => {
    if (!onReferences) return
    if (!text.startsWith('=')) {
      onReferences([])
      return
    }
    const refs: { col: number; row: number }[] = []
    const re = /\$?([A-Z]{1,3})\$?(\d+)/g
    let m: RegExpExecArray | null
    while ((m = re.exec(text))) {
      let c = 0
      for (const ch of m[1]) c = c * 26 + (ch.charCodeAt(0) - 64)
      refs.push({ col: c - 1, row: parseInt(m[2], 10) - 1 })
    }
    onReferences(refs)
  }

  useEffect(() => {
    const input = ref.current!
    const startText = pos.initial ?? cell?.input ?? ''
    input.value = startText
    input.focus()
    input.setSelectionRange(input.value.length, input.value.length)
    emitRefs(startText)

    // Returns true only when the editor is in formula mode and the click was
    // consumed as an address insertion.
    const clickCell = (col: number, row: number): boolean => {
      if (!input.value.startsWith('=')) return false
      const start = input.selectionStart ?? input.value.length
      const end = input.selectionEnd ?? start
      const stripped = input.value.slice(0, start).replace(REF_AT_CURSOR, '')
      const addr = cellAddress(col, row)
      const next = stripped + addr + input.value.slice(end)
      input.value = next
      const caret = stripped.length + addr.length
      input.setSelectionRange(caret, caret)
      input.focus()
      emitRefs(next)
      return true
    }
    registerClickRef?.(clickCell)
    return () => {
      registerClickRef?.(null)
      onReferences?.([])
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const finish = (fn: () => void) => {
    if (finished.current) return
    finished.current = true
    fn()
  }

  const commit = () => finish(() => onCommit(ref.current!.value))
  const cancel = () => finish(onCancel)

  return (
    <input
      ref={ref}
      className="cell-editor"
      style={{ top, left, width }}
      onChange={(e) => {
        e.currentTarget.classList.toggle('formula', e.currentTarget.value.startsWith('='))
        emitRefs(e.currentTarget.value)
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          commit()
        } else if (e.key === 'Escape') {
          e.preventDefault()
          cancel()
        }
        e.stopPropagation()
      }}
      onBlur={commit}
    />
  )
}
