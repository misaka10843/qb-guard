<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, logout } from '@/lib/api'
import { connect, disconnect, loadStatus, store } from '@/lib/store'
import { toast, guard } from '@/lib/toast'
import { Button, Badge } from '@/components/ui'
import {
  LayoutDashboard,
  Boxes,
  Ban,
  Rss,
  Radar,
  BarChart3,
  Settings,
  LogOut,
  Sun,
  Moon,
  Wifi,
  WifiOff,
  Menu,
  X,
} from 'lucide-vue-next'

import OverviewView from '@/views/OverviewView.vue'
import ModulesView from '@/views/ModulesView.vue'
import BansView from '@/views/BansView.vue'
import SubscriptionsView from '@/views/SubscriptionsView.vue'
import TrackersView from '@/views/TrackersView.vue'
import StatsView from '@/views/StatsView.vue'
import SettingsView from '@/views/SettingsView.vue'

const props = defineProps({ username: { type: String, default: '' } })
const emit = defineEmits(['logout'])

const nav = [
  { key: 'overview', label: '总览', icon: LayoutDashboard, comp: OverviewView },
  { key: 'modules', label: '检测模块', icon: Boxes, comp: ModulesView },
  { key: 'bans', label: '封禁', icon: Ban, comp: BansView },
  { key: 'subs', label: 'IP 集订阅', icon: Rss, comp: SubscriptionsView },
  { key: 'trackers', label: 'Tracker 聚合', icon: Radar, comp: TrackersView },
  { key: 'stats', label: '流量统计', icon: BarChart3, comp: StatsView },
  { key: 'settings', label: '设置', icon: Settings, comp: SettingsView },
]

const view = ref('overview')
const menuOpen = ref(false)
const dark = ref(true)
const slide = ref('view-next')

const current = computed(() => nav.find((n) => n.key === view.value) || nav[0])
const banCount = computed(() => store.status?.currentBans ?? 0)

function go(key) {
  const from = nav.findIndex((n) => n.key === view.value)
  const to = nav.findIndex((n) => n.key === key)
  if (to !== from) slide.value = to > from ? 'view-next' : 'view-prev'
  view.value = key
  menuOpen.value = false
}

function applyTheme() {
  document.documentElement.classList.toggle('dark', dark.value)
  localStorage.setItem('qbguard-theme', dark.value ? 'dark' : 'light')
}

function toggleTheme() {
  dark.value = !dark.value
  applyTheme()
}

async function signOut() {
  await guard(() => logout())
  disconnect()
  emit('logout')
}

onMounted(async () => {
  const saved = localStorage.getItem('qbguard-theme')
  dark.value = saved ? saved === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches
  applyTheme()

  await guard(loadStatus)
  connect()
})

onUnmounted(disconnect)

watch(
  () => store.reloaded,
  (n) => {
    if (n > 0) toast.ok('配置已热重载')
  },
)
</script>

<template>
  <div class="flex h-screen overflow-hidden">
    <aside
      class="fixed inset-y-0 left-0 z-40 flex w-60 shrink-0 flex-col border-r bg-card transition-transform lg:static lg:translate-x-0"
      :class="menuOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <div class="flex h-14 shrink-0 items-center gap-2 border-b px-4">
        <div class="flex size-7 items-center justify-center rounded-lg border bg-background">
          <Radar class="size-4" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-semibold">qB 下载守卫</div>
          <div class="truncate text-[11px] text-muted-foreground">
            v{{ store.version || store.status?.version || '—' }}
          </div>
        </div>
        <button class="lg:hidden" aria-label="关闭菜单" @click="menuOpen = false">
          <X class="size-4" />
        </button>
      </div>

      <nav class="flex flex-1 flex-col gap-0.5 overflow-y-auto p-2">
        <button
          v-for="item in nav"
          :key="item.key"
          :data-nav="item.key"
          :data-active="view === item.key ? 'true' : 'false'"
          class="flex items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium transition-colors"
          :class="
            view === item.key
              ? 'bg-accent text-accent-foreground'
              : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground'
          "
          @click="go(item.key)"
        >
          <component :is="item.icon" class="size-4 shrink-0" />
          <span class="flex-1 text-left">{{ item.label }}</span>
          <Badge v-if="item.key === 'bans' && banCount" variant="destructive">{{ banCount }}</Badge>
        </button>
      </nav>

      <div class="flex shrink-0 flex-col gap-2 border-t p-3">
        <div class="flex items-center gap-2 text-xs text-muted-foreground">
          <component :is="store.connected ? Wifi : WifiOff" class="size-3.5"
            :class="store.connected ? 'text-success' : 'text-destructive'" />
          <span>{{ store.connected ? '实时连接正常' : '实时连接断开' }}</span>
        </div>
        <div class="flex items-center gap-1">
          <span class="flex-1 truncate text-xs text-muted-foreground">{{ props.username }}</span>
          <Button variant="ghost" size="icon-sm" :title="dark ? '切换浅色' : '切换深色'" @click="toggleTheme">
            <component :is="dark ? Sun : Moon" />
          </Button>
          <Button variant="ghost" size="icon-sm" title="退出登录" @click="signOut">
            <LogOut />
          </Button>
        </div>
      </div>
    </aside>

    <Transition name="fade">
      <div
        v-if="menuOpen"
        class="fixed inset-0 z-30 bg-black/50 lg:hidden"
        @click="menuOpen = false"
      />
    </Transition>

    <main class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <header class="z-20 flex h-14 shrink-0 items-center gap-3 border-b bg-background/85 px-4 backdrop-blur">
        <button class="lg:hidden" aria-label="打开菜单" @click="menuOpen = true">
          <Menu class="size-5" />
        </button>
        <h1 class="text-base font-semibold">{{ current.label }}</h1>
        <div class="flex-1" />
        <Badge v-if="store.status?.qbUrl" variant="muted" class="mono hidden sm:inline-flex">
          {{ store.status.qbUrl }}
        </Badge>
      </header>

      <div class="flex-1 overflow-y-auto p-4 lg:p-6" :data-slide="slide">
        <Transition :name="slide" mode="out-in">
          <component :is="current.comp" :key="current.key" @goto="go" />
        </Transition>
      </div>
    </main>
  </div>
</template>
