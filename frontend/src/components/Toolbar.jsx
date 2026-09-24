// Toolbar hosts sheet tabs, structural operations, undo/redo and connection
// status. Structural ops are intentionally coarse-grained controls rather
// than per-row handles, keeping the focus on the engine.
export default function Toolbar({
  workbook,
  activeSheet,
  onSelectSheet,
  onStruct,
  onUndo,
  onRedo,
  canUndo,
  canRedo,
  status,
  selection,
}) {
  const rowIdx = selection ? selection.row : 0
  const colIdx = selection ? selection.col : 0

  return (
    <div className="toolbar">
      <div className="toolbar-group">
        <button className="btn" onClick={onUndo} disabled={!canUndo} title="撤销你的上一步 (Ctrl+Z)">
          ↶ 撤销
        </button>
        <button className="btn" onClick={onRedo} disabled={!canRedo} title="重做 (Ctrl+Shift+Z)">
          ↷ 重做
        </button>
      </div>
      <div className="divider" />
      <div className="toolbar-group">
        <button className="btn" onClick={() => onStruct('insert_row', activeSheet, rowIdx)}>
          上方插入行
        </button>
        <button className="btn" onClick={() => onStruct('delete_row', activeSheet, rowIdx)}>
          删除本行
        </button>
        <button className="btn" onClick={() => onStruct('insert_col', activeSheet, colIdx)}>
          左侧插入列
        </button>
        <button className="btn" onClick={() => onStruct('delete_col', activeSheet, colIdx)}>
          删除本列
        </button>
      </div>
      <div className="divider" />
      <div className="sheet-tabs">
        {workbook.sheets.map((s) => (
          <button
            key={s.name}
            className={'sheet-tab' + (s.name === activeSheet ? ' active' : '')}
            onClick={() => onSelectSheet(s.name)}
          >
            {s.name}
          </button>
        ))}
      </div>
      <div className="spacer" />
      <div className={'status status-' + status}>
        <span className="status-dot" />
        {status === 'connected' ? '已连接' : status === 'connecting' ? '连接中…' : '断线，正在重连'}
      </div>
    </div>
  )
}
