import { reactive } from 'vue'

export const toasts = reactive([])
let seq = 0

export function dismiss(id) {
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

export function toast(message, kind = 'info') {
  const id = ++seq
  toasts.push({ id, message: String(message), kind })
  setTimeout(() => dismiss(id), kind === 'error' ? 8000 : 3200)
  return id
}

toast.ok = (m) => toast(m, 'ok')
toast.error = (m) => toast(m, 'error')

export async function guard(fn, successMessage) {
  try {
    const result = await fn()
    if (successMessage) toast.ok(successMessage)
    return result
  } catch (err) {
    toast.error(err?.message || '操作失败')
    return undefined
  }
}
