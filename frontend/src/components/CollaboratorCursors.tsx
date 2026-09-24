import type { UserState } from '../state/types'

// CollaboratorCursors renders every *other* user's selection marker and
// their "currently editing" box. It lives inside the scrolled content layer,
// so coordinates are absolute within the full grid (offset by the sticky
// header sizes).
interface Props {
  users: UserState[]
  selfId: string
  sheetName: string
  colWidth: number
  rowHeight: number
  headerW: number
  headerH: number
}

export function CollaboratorCursors({
  users, selfId, sheetName, colWidth, rowHeight, headerW, headerH,
}: Props) {
  return (
    <>
      {users
        .filter((u) => u.id !== selfId && u.sheet === sheetName)
        .map((u) => {
          const left = headerW + u.col * colWidth
          const top = headerH + u.row * rowHeight
          return (
            <div
              key={'cur-' + u.id}
              className="cursor-tag"
              style={{ left, top, width: colWidth, borderColor: u.color }}
            >
              <span className="label" style={{ background: u.color }}>
                {u.name}
              </span>
            </div>
          )
        })}
      {users
        .filter(
          (u) =>
            u.id !== selfId &&
            u.editing &&
            u.editSheet === sheetName,
        )
        .map((u) => {
          const left = headerW + u.editCol * colWidth
          const top = headerH + u.editRow * rowHeight
          return (
            <div
              key={'edit-' + u.id}
              className="edit-tag"
              style={{
                left, top, width: colWidth, height: rowHeight,
                borderColor: u.color,
              }}
            >
              <span className="label" style={{ background: u.color }}>
                {u.name} 正在编辑
              </span>
            </div>
          )
        })}
    </>
  )
}
