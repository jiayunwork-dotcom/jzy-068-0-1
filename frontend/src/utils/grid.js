// Address helpers shared by grid components and state management.

export function colName(col) {
  let n = col + 1
  let s = ''
  while (n > 0) {
    n--
    s = String.fromCharCode(65 + (n % 26)) + s
    n = Math.floor(n / 26)
  }
  return s
}

export function colIndex(name) {
  let n = 0
  for (const ch of name.toUpperCase()) n = n * 26 + (ch.charCodeAt(0) - 64)
  return n - 1
}

export function addrOf(col, row) {
  return colName(col) + (row + 1)
}

export function parseAddr(addr) {
  const m = /^([A-Z]+)(\d+)$/i.exec(addr)
  if (!m) return null
  return { col: colIndex(m[1]), row: parseInt(m[2], 10) - 1 }
}

export const ROW_H = 26
export const ROW_HEADER_W = 46
export const COL_W = 96
export const COL_HEADER_H = 28
