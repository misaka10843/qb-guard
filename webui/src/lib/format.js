const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']

export function bytes(value) {
  if (!Number.isFinite(value)) return '—'
  const sign = value < 0 ? '-' : ''
  let v = Math.abs(value)
  let i = 0
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024
    i += 1
  }
  return `${sign}${v.toFixed(i === 0 ? 0 : 2)} ${UNITS[i]}`
}

export function speed(value) {
  return `${bytes(value)}/s`
}

export function number(value) {
  if (!Number.isFinite(value)) return '—'
  return value.toLocaleString('zh-CN')
}

function pad(n) {
  return String(n).padStart(2, '0')
}

export function hms(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function dayLabel(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function stamp(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function dateLabel(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function datetime(ms) {
  if (!ms) return '—'
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function countdown(untilMs) {
  if (!untilMs) return '永久'
  const left = Math.floor((untilMs - Date.now()) / 1000)
  if (left <= 0) return '已到期'
  const d = Math.floor(left / 86400)
  const h = Math.floor((left % 86400) / 3600)
  const m = Math.floor((left % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  return `${m} 分`
}

export function ago(input) {
  if (!input) return '从未'
  const t = typeof input === 'number' ? input : Date.parse(input)
  if (!Number.isFinite(t)) return '—'
  const secs = Math.floor((Date.now() - t) / 1000)
  if (secs < 60) return '刚刚'
  if (secs < 3600) return `${Math.floor(secs / 60)} 分钟前`
  if (secs < 86400) return `${Math.floor(secs / 3600)} 小时前`
  return `${Math.floor(secs / 86400)} 天前`
}
