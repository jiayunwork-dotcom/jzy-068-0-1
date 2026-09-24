import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { SheetSocket } from '../utils/socket'
import { addrOf, parseAddr } from '../utils/grid'

const CLIENT_ID_KEY = 'collabsheet.clientId'

function clientId() {
  let id = localStorage.getItem(CLIENT_ID_KEY)
  if (!id) {
    id = Math.random().toString(16).slice(2) + Date.now().toString(16)
    localStorage.setItem(CLIENT_ID_KEY, id)
  }
  return id
}

function emptyWorkbook() {
  return { sheets: [] }
}

export function useCollabSheet() {
  const idRef = useRef(clientId())
  const [displayName, setDisplayName] = useState(
    () => localStorage.getItem('collabsheet.name') || '',
  )
  const [workbook, setWorkbook] = useState(emptyWorkbook)
  const [activeSheet, setActiveSheet] = useState('')
  const [presence, setPresence] = useState({ me: idRef.current, peers: [] })
  const [status, setStatus] = useState('connecting')
  const [notice, setNotice] = useState(null) // {kind, text}
  const [canUndo, setCanUndo] = useState(false)
  const [canRedo, setCanRedo] = useState(false)
  const socketRef = useRef(null)

  // Active cell selection (this user's cursor).
  const [selection, setSelection] = useState(null) // {sheet, col, row}

  // The cell currently edited locally but not yet committed (kept here so a
  // reconnect can still flush it); keyed by sheet!addr.
  const pendingRef = useRef(null)

  const flashNotice = useCallback((n) => {
    setNotice(n)
    setTimeout(() => setNotice((cur) => (cur === n ? null : cur)), 4000)
  }, [])

  useEffect(() => {
    const name = displayName || ('用户' + idRef.current.slice(0, 4))
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/ws?id=${encodeURIComponent(idRef.current)}`
    const sock = new SheetSocket({
      url,
      hello: { type: 'hello', id: idRef.current, name },
      onStatus: setStatus,
      onMessage: (msg) => handleMessage(msg, sock),
    })
    socketRef.current = sock
    return () => sock.close()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [displayName])

  const handleMessage = useCallback((msg, sock) => {
    switch (msg.type) {
      case 'snapshot':
        applySnapshot(msg.workbook)
        setPresence(msg.presence)
        break
      case 'patch':
        applyPatch(msg)
        break
      case 'presence':
        setPresence(msg.presence)
        break
      case 'conflict': {
        // Our value at that cell was overwritten by another writer.
        flashNotice({
          kind: 'conflict',
          text: `你在 ${msg.cell} 的内容「${msg.your || '空'}」已被 ${msg.byName} 覆盖为「${msg.theirs || '空'}」（最后写入胜出）`,
        })
        break
      }
      case 'undo_result':
      case 'redo_result':
        setCanUndo(msg.canUndo)
        setCanRedo(msg.canRedo)
        break
      case 'stack_state':
        setCanUndo(msg.canUndo)
        setCanRedo(msg.canRedo)
        break
      case 'error':
        flashNotice({ kind: 'error', text: msg.msg })
        break
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ---- state mutations ----

  const applySnapshot = useCallback((wb) => {
    setWorkbook(wb)
    setActiveSheet((cur) => {
      if (cur && wb.sheets.some((s) => s.name === cur)) return cur
      return wb.sheets[0]?.name || ''
    })
  }, [])

  const applyPatch = useCallback((patch) => {
    setWorkbook((wb) => {
      const sheets = wb.sheets.map((s) => {
        const changes = patch.cells.filter((c) => c.sheet === s.name)
        if (changes.length === 0) return s
        const cells = { ...s.cells }
        for (const c of changes) {
          if (!c.raw) delete cells[c.cell]
          else cells[c.cell] = c
        }
        return { ...s, cells }
      })
      return { ...wb, sheets }
    })
  }, [])

  // ---- outgoing actions ----

  const send = useCallback((obj) => socketRef.current?.send(obj), [])

  const commitEdit = useCallback(
    (sheet, addr, raw) => {
      pendingRef.current = null
      send({ type: 'edit', sheet, cell: addr, raw })
      // Optimistic local render; the authoritative patch follows immediately.
      applyPatch({
        type: 'patch',
        cells: [{ sheet, cell: addr, raw, value: raw.startsWith('=') ? '…' : raw }],
      })
    },
    [applyPatch, send],
  )

  const moveCursor = useCallback(
    (sheet, col, row) => {
      setSelection({ sheet, col, row })
      send({ type: 'cursor', sheet, cell: addrOf(col, row) })
    },
    [send],
  )

  const undo = useCallback(() => send({ type: 'undo' }), [send])
  const redo = useCallback(() => send({ type: 'redo' }), [send])

  const structOp = useCallback(
    (kind, sheet, index) => send({ type: 'struct', op: { kind, sheet, index, count: 1 } }),
    [send],
  )

  const saveName = useCallback((name) => {
    localStorage.setItem('collabsheet.name', name)
    setDisplayName(name)
  }, [])

  const activeSheetData = useMemo(
    () => workbook.sheets.find((s) => s.name === activeSheet) || null,
    [workbook, activeSheet],
  )

  // Cell lookup helper.
  const cellAt = useCallback(
    (sheet, col, row) => {
      const sh = workbook.sheets.find((s) => s.name === sheet)
      if (!sh) return null
      return sh.cells[addrOf(col, row)] || null
    },
    [workbook],
  )

  return {
    me: idRef.current,
    displayName,
    saveName,
    workbook,
    activeSheet,
    setActiveSheet,
    activeSheetData,
    presence,
    status,
    notice,
    selection,
    moveCursor,
    commitEdit,
    undo,
    redo,
    canUndo,
    canRedo,
    structOp,
    cellAt,
  }
}
