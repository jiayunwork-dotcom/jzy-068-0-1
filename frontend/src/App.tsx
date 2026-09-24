import { useSyncExternalStore } from 'react'
import { store } from './state/store'
import { send } from './state/ws'
import { Grid } from './components/Grid'

export function App() {
  useSyncExternalStore(store.subscribe, store.getState)
  const s = store.getState()
  const sheet = s.sheets[s.activeSheet]

  const structure = (kind: string) => {
    const idx = parseInt(prompt(`输入要${kind.includes('Row') ? '行' : '列'}的位置（1 开始的序号）：`, '1') || '', 10)
    if (!idx || idx < 1) return
    // Convert 1-based insertion index: inserting "before row N" = 0-based N-1.
    send({ type: 'structure', kind, sheet: sheet?.name, index: idx - 1 })
  }

  return (
    <div className="app">
      <div className="topbar">
        <h1>📊 协同电子表格</h1>
        <button className="btn" onClick={() => structure('insertRow')}>插入行</button>
        <button className="btn" onClick={() => structure('deleteRow')}>删除行</button>
        <button className="btn" onClick={() => structure('insertCol')}>插入列</button>
        <button className="btn" onClick={() => structure('deleteCol')}>删除列</button>
        <div className="spacer" />
        <span className="conn">
          <span className={'dot ' + (s.connected ? 'on' : 'off')} />
          {s.connected ? '已连接' : '断线重连中…'}
        </span>
        <input
          className="name"
          value={s.nameInput}
          onChange={(e) => store.setName(e.target.value)}
          placeholder="你的名字"
        />
        <button className="btn" disabled={!s.canUndo} onClick={() => send({ type: 'undo' })}>
          撤销
        </button>
        <button className="btn" disabled={!s.canRedo} onClick={() => send({ type: 'redo' })}>
          重做
        </button>
      </div>

      <div className="tabs">
        {s.sheets.map((sh, i) => (
          <div
            key={sh.name}
            className={'tab' + (i === s.activeSheet ? ' active' : '')}
            onClick={() => store.setActiveSheet(i)}
          >
            {sh.name}
          </div>
        ))}
        <div className="spacer" style={{ flex: 1 }} />
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingRight: 10 }}>
          {s.users
            .filter((u) => u.id)
            .map((u) => (
              <span
                key={u.id}
                title={u.name}
                style={{
                  display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 12,
                }}
              >
                <span
                  style={{
                    width: 9, height: 9, borderRadius: '50%', background: u.color,
                    display: 'inline-block',
                  }}
                />
                {u.name}
              </span>
            ))}
        </div>
      </div>

      <Grid />

      <div className="notices">
        {s.notices.map((n) => (
          <div className="notice" key={n.id}>
            <button onClick={() => store.dismissNotice(n.id)}>×</button>
            {n.text}
          </div>
        ))}
      </div>
    </div>
  )
}
