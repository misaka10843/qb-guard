export class ApiError extends Error {
  constructor(status, message) {
    super(message)
    this.status = status
  }
}

const listeners = new Set()

export function onUnauthorized(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

async function request(method, path, body) {
  const init = { method, headers: {}, credentials: 'same-origin' }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  const resp = await fetch(path, init)
  const text = await resp.text()
  let data = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { error: text }
    }
  }
  if (resp.status === 401) {
    listeners.forEach((fn) => fn())
    throw new ApiError(401, '登录状态已失效')
  }
  if (!resp.ok) {
    throw new ApiError(resp.status, (data && data.error) || `HTTP ${resp.status}`)
  }
  return data
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body ?? {}),
  put: (path, body) => request('PUT', path, body ?? {}),
  del: (path) => request('DELETE', path),
}

export function login(username, password) {
  return request('POST', '/api/auth/login', { username, password })
}

export function logout() {
  return request('POST', '/api/auth/logout', {})
}

export function authStatus() {
  return request('GET', '/api/auth/status')
}

export function subscribe(handlers) {
  const es = new EventSource('/api/events', { withCredentials: true })
  for (const [name, fn] of Object.entries(handlers)) {
    es.addEventListener(name, (ev) => {
      try {
        fn(JSON.parse(ev.data))
      } catch {}
    })
  }
  es.addEventListener('error', () => {
    if (es.readyState === EventSource.CLOSED) {
      listeners.forEach((fn) => fn())
    }
  })
  return () => es.close()
}
