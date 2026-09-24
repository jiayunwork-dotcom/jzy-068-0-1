import { ROW_H, ROW_HEADER_W, COL_W, COL_HEADER_H, parseAddr } from '../utils/grid'

// PeerCursors draws every other participant's selection as a colored bordered
// box with a name tag. Cursors are positioned in the grid's content
// coordinates (inside the scrolling spacer) so they scroll naturally.
export default function PeerCursors({ me, presence, sheetName, width, height }) {
  return (
    <div
      className="peer-cursors"
      style={{
        position: 'absolute',
        left: ROW_HEADER_W,
        top: COL_HEADER_H,
        width,
        height,
        pointerEvents: 'none',
        zIndex: 40,
      }}
    >
      {presence.peers
        .filter((p) => p.id !== me && p.sheet === sheetName && p.cell)
        .map((p) => {
          const parsed = parseAddr(p.cell)
          if (!parsed) return null
          const x = parsed.col * COL_W
          const y = parsed.row * ROW_H
          return (
            <div
              key={p.id}
              className="peer-cursor"
              style={{
                position: 'absolute',
                left: x - 2,
                top: y - 2,
                width: COL_W + 3,
                height: ROW_H + 3,
                border: `2px solid ${p.color}`,
                boxShadow: `0 0 0 1px ${p.color}55`,
                borderRadius: 2,
              }}
            >
              <span
                className="peer-tag"
                style={{
                  position: 'absolute',
                  top: -19,
                  left: -2,
                  background: p.color,
                  color: '#fff',
                  fontSize: 11,
                  lineHeight: '17px',
                  padding: '0 6px',
                  borderRadius: '3px 3px 3px 0',
                  whiteSpace: 'nowrap',
                }}
              >
                {p.name}
              </span>
            </div>
          )
        })}
    </div>
  )
}
