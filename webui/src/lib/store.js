import { reactive } from 'vue'
import { api, subscribe } from './api'

export const store = reactive({
  connected: false,
  status: null,
  stats: null,
  trackers: null,
  geo: null,
  reloaded: 0,
  lastBan: null,
})

let close = null

export function connect() {
  disconnect()
  close = subscribe({
    hello: (data) => {
      store.connected = true
      if (data.version) store.version = data.version
    },
    status: (data) => {
      store.status = data
    },
    stats: (data) => {
      store.stats = data
    },
    trackers: (data) => {
      store.trackers = data
    },
    geo: (data) => {
      store.geo = data
    },
    ban: (data) => {
      store.lastBan = data
    },
    config: () => {
      store.reloaded += 1
    },
  })
}

export function disconnect() {
  if (close) {
    close()
    close = null
  }
  store.connected = false
}

export async function loadStatus() {
  store.status = await api.get('/api/status')
}
