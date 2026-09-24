// PresencePanel lists everyone currently on the workbook.
export default function PresencePanel({ presence, me, onSaveName, displayName }) {
  return (
    <div className="presence">
      <div className="presence-title">
        在线协作者（{presence.peers.length}）
        <input
          className="name-input"
          defaultValue={displayName}
          placeholder="你的名字"
          onBlur={(e) => onSaveName(e.target.value.trim())}
          onKeyDown={(e) => e.key === 'Enter' && e.target.blur()}
        />
      </div>
      <div className="peer-list">
        {presence.peers.map((p) => (
          <div key={p.id} className={'peer' + (p.id === me ? ' me' : '')}>
            <span className="peer-dot" style={{ background: p.color }} />
            <span className="peer-name">{p.name}{p.id === me ? '（你）' : ''}</span>
            <span className="peer-loc">
              {p.cell ? `${p.sheet}!${p.cell}` : p.sheet}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
