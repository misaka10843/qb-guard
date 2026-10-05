<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard } from '@/lib/toast'
import { Badge, Button, Card, Tip } from '@/components/ui'
import { bytes, number, speed, countdown, ago } from '@/lib/format'
import {
  Activity,
  ShieldAlert,
  ShieldCheck,
  Timer,
  Gauge,
  Users,
  RefreshCw,
  CircleSlash,
} from 'lucide-vue-next'

const bans = ref([])
const subs = ref(null)
const trackers = ref(null)
const busy = ref(false)

const s = computed(() => store.status)

const tiles = computed(() => {
  const st = s.value
  return [
    { label: '运行时长', value: st?.uptime || '—', icon: Timer, hint: '本服务启动至今的时间' },
    {
      label: '检测轮次',
      value: number(st?.cycles ?? 0),
      icon: RefreshCw,
      hint: '每 check-interval 跑一轮：拉一次任务列表，检查其中所有 Peer。这里是从启动到现在累计跑了多少轮',
    },
    {
      label: '本轮耗时',
      value: st ? `${st.cycleTakenMs} ms` : '—',
      icon: Gauge,
      hint: '最近一轮检测从开始到结束的耗时。明显变长通常意味着下载器响应慢或 Peer 太多',
    },
    {
      label: '活跃任务',
      value: number(st?.activeTorrents ?? 0),
      icon: Activity,
      hint: '下载器里处于下载 / 做种等活跃状态的任务数，已暂停与已完成的不算',
    },
    {
      label: '已检查 Peer',
      value: number(st?.peersChecked ?? 0),
      icon: Users,
      hint: '累计检查过的 Peer 次数。同一个 Peer 每轮都会重新检查一次，所以这个数会持续增长',
    },
    {
      label: '累计封禁',
      value: number(st?.bansIssued ?? 0),
      icon: ShieldAlert,
      hint: '进程启动以来发出的封禁总数，包含已经到期的',
    },
    {
      label: '当前封禁',
      value: number(st?.currentBans ?? 0),
      icon: CircleSlash,
      hint: '此刻仍在生效的封禁条数。到期后会自动解除，这个数会降下来',
    },
    {
      label: '错误次数',
      value: number(st?.errors ?? 0),
      icon: ShieldCheck,
      hint: '与下载器通信失败等错误的累计次数。正常应长期保持 0；不为 0 时看日志里的具体报错',
    },
  ]
})

const modules = computed(() => s.value?.modules || [])

async function load() {
  busy.value = true
  await guard(async () => {
    const [b, sub, tr] = await Promise.all([
      api.get('/api/bans'),
      api.get('/api/subscriptions'),
      api.get('/api/trackers'),
    ])
    bans.value = b.bans || []
    subs.value = sub
    trackers.value = tr
  })
  busy.value = false
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-5">
    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <Card v-for="t in tiles" :key="t.label" class="flex items-center gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
          <component :is="t.icon" class="size-4 text-muted-foreground" />
        </div>
        <div class="min-w-0">
          <div class="flex items-center gap-1 text-xs text-muted-foreground">
            <span class="truncate">{{ t.label }}</span>
            <Tip :text="t.hint" />
          </div>
          <div class="mono truncate text-lg font-semibold">{{ t.value }}</div>
        </div>
      </Card>
    </div>

    <div class="grid gap-4 lg:grid-cols-3">
      <Card class="lg:col-span-2">
        <div class="mb-3 flex items-center gap-2">
          <h2 class="text-sm font-semibold">已启用的检测模块</h2>
          <Tip text="每个模块负责一类反吸血判定。任一模块判定为封禁即封禁该 Peer；同时命中多个时，取封禁时长更长的那个作为归属。" />
          <Badge variant="muted">{{ modules.length }}</Badge>
          <div class="flex-1" />
          <Button variant="ghost" size="icon-sm" :loading="busy" title="刷新" @click="load">
            <RefreshCw />
          </Button>
        </div>
        <div v-if="!modules.length" class="py-6 text-center text-sm text-muted-foreground">
          没有启用任何模块，当前处于只读模式（不会向下载器写入任何封禁）
        </div>
        <div v-else class="flex flex-wrap gap-2">
          <Badge v-for="m in modules" :key="m" variant="secondary" class="mono">{{ m }}</Badge>
        </div>
      </Card>

      <Card>
        <div class="mb-3 flex items-center gap-2">
          <h2 class="text-sm font-semibold">今日流量</h2>
          <Tip text="上传/下载按自然日累计，由后台按 sample-interval 定时采样。实时速率取自下载器当前上报值。" />
        </div>
        <div v-if="s?.trafficToday" class="flex flex-col gap-2 text-sm">
          <div class="flex justify-between">
            <span class="text-muted-foreground">上传</span>
            <span class="mono">{{ bytes(s.trafficToday.up) }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-muted-foreground">下载</span>
            <span class="mono">{{ bytes(s.trafficToday.dl) }}</span>
          </div>
        </div>
        <div v-else class="py-4 text-center text-sm text-muted-foreground">暂无采样</div>
        <div v-if="store.stats?.global" class="mt-3 flex flex-col gap-2 border-t pt-3 text-sm">
          <div class="flex justify-between">
            <span class="text-muted-foreground">实时上传</span>
            <span class="mono">{{ speed(store.stats.global.upspeed) }}</span>
          </div>
          <div class="flex justify-between">
            <span class="text-muted-foreground">实时下载</span>
            <span class="mono">{{ speed(store.stats.global.dlspeed) }}</span>
          </div>
        </div>
      </Card>
    </div>

    <div class="grid gap-4 lg:grid-cols-3">
      <Card class="lg:col-span-2">
        <div class="mb-3 flex items-center gap-2">
          <h2 class="text-sm font-semibold">最近封禁</h2>
          <Tip text="按封禁时间倒序，只列最近 8 条。完整列表与搜索在「封禁」页。" />
          <Badge variant="muted">{{ bans.length }}</Badge>
        </div>
        <div v-if="!bans.length" class="py-6 text-center text-sm text-muted-foreground">
          当前没有封禁记录
        </div>
        <div v-else class="flex flex-col divide-y">
          <div
            v-for="b in bans.slice(0, 8)"
            :key="b.ip"
            class="flex items-center gap-3 py-2 text-sm"
          >
            <span class="mono w-32 shrink-0">{{ b.ip }}</span>
            <Badge variant="destructive">{{ b.module }}</Badge>
            <span class="min-w-0 flex-1 truncate text-muted-foreground" :title="b.reason">
              {{ b.reason }}
            </span>
            <span class="shrink-0 text-xs text-muted-foreground">
              <template v-if="b.disconnect">
                <Tip text="临时断开：只是把连接断掉、不加入下载器封禁列表，几秒后对端可以重连。用于「再观察一下」的场景。">
                  <span class="cursor-help underline decoration-dotted underline-offset-2">临时断开</span>
                </Tip>
              </template>
              <template v-else>{{ countdown(b.untilMs) }}</template>
            </span>
          </div>
        </div>
      </Card>

      <Card>
        <div class="mb-3 flex items-center gap-2">
          <h2 class="text-sm font-semibold">订阅与聚合</h2>
          <Tip text="IP 集订阅把远程网段列表拉下来当黑名单用；Tracker 聚合把多个 tracker 列表合并去重后写进下载器。两者都在左侧对应页面里配置。" />
        </div>
        <div class="flex flex-col gap-3 text-sm">
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground">IP 集订阅</span>
            <span class="mono">
              {{ subs?.enabled ? `${(subs.subscriptions || []).length} 个源` : '未启用' }}
            </span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-muted-foreground">Tracker 聚合</span>
            <span class="mono">
              {{ trackers?.enabled ? `${trackers.snapshot?.total ?? 0} 条` : '未启用' }}
            </span>
          </div>
          <div v-if="trackers?.enabled" class="flex items-center justify-between">
            <span class="text-muted-foreground">上次刷新</span>
            <span class="mono">{{ ago(trackers.snapshot?.lastRun) }}</span>
          </div>
          <div v-if="trackers?.snapshot?.lastError" class="rounded-md bg-destructive/10 p-2 text-xs text-destructive">
            {{ trackers.snapshot.lastError }}
          </div>
        </div>
      </Card>
    </div>
  </div>
</template>
