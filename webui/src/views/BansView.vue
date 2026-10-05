<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Input, Label, Select, Tip } from '@/components/ui'
import { Ban, RefreshCw, Trash2, Plus, Search } from 'lucide-vue-next'
import { datetime, countdown } from '@/lib/format'

const bans = ref([])
const foreign = ref([])
const loading = ref(false)
const keyword = ref('')
const moduleFilter = ref('all')

const addOpen = ref(false)
const form = ref({ ip: '', duration: '1h', reason: '' })
const submitting = ref(false)

const DURATIONS = ['5m', '1h', '12h', '1d', '7d', '30d', '永久']

const moduleOptions = computed(() => {
  const names = [...new Set(bans.value.map((b) => b.module))].sort()
  return [{ value: 'all', label: '全部模块' }, ...names.map((n) => ({ value: n, label: n }))]
})

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  return bans.value
    .filter((b) => moduleFilter.value === 'all' || b.module === moduleFilter.value)
    .filter(
      (b) =>
        !kw ||
        b.ip.toLowerCase().includes(kw) ||
        (b.reason || '').toLowerCase().includes(kw) ||
        (b.torrent || '').toLowerCase().includes(kw),
    )
})

async function load() {
  loading.value = true
  await guard(async () => {
    const res = await api.get('/api/bans')
    bans.value = res.bans || []
    foreign.value = res.foreign || []
  })
  loading.value = false
}

async function add() {
  submitting.value = true
  await guard(async () => {
    const duration = form.value.duration === '永久' ? '' : form.value.duration
    await api.post('/api/bans', { ...form.value, duration })
    toast.ok(`已封禁 ${form.value.ip}`)
    addOpen.value = false
    form.value = { ip: '', duration: '1h', reason: '' }
    await load()
  })
  submitting.value = false
}

async function unban(ip) {
  await guard(async () => {
    await api.del(`/api/bans/${encodeURIComponent(ip)}`)
    toast.ok(`已解封 ${ip}`)
    await load()
  })
}

onMounted(load)
watch(() => store.lastBan, load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <div class="relative">
        <Search class="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input v-model="keyword" placeholder="搜索 IP / 原因 / 种子" class="w-56 pl-8" />
      </div>
      <Select v-model="moduleFilter" :options="moduleOptions" class="w-44" />
      <Badge variant="muted">{{ filtered.length }} / {{ bans.length }} 条</Badge>
      <Tip text="封禁列表整体写入下载器，这里只读显示。到期会自动解除，也可以点右侧的垃圾桶立刻解封。" />
      <div class="flex-1" />
      <Button variant="ghost" size="icon-sm" :loading="loading" title="刷新" @click="load">
        <RefreshCw />
      </Button>
      <Tip text="手工封一个 IP 或网段，立即写入下载器。用于临时处理已知的吸血来源。">
        <Button size="sm" @click="addOpen = true"><Plus />手动封禁</Button>
      </Tip>
    </div>

    <Card padded="false" class="overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="border-b bg-muted/40 text-xs text-muted-foreground">
            <tr>
              <th class="px-4 py-2.5 text-left font-medium">IP</th>
              <th class="px-4 py-2.5 text-left font-medium">
                <span class="inline-flex items-center gap-1">
                  来源模块
                  <Tip text="判定这条封禁的模块。同一 Peer 同时命中多个模块时，取封禁时长更长的那个。" />
                </span>
              </th>
              <th class="px-4 py-2.5 text-left font-medium">原因</th>
              <th class="px-4 py-2.5 text-left font-medium">任务</th>
              <th class="px-4 py-2.5 text-left font-medium">
                <span class="inline-flex items-center gap-1">
                  到期
                  <Tip text="到点后自动从下载器封禁列表移除。显示「临时断开」的条目只是断了连接、没有进封禁列表，对端可以重连。" />
                </span>
              </th>
              <th class="px-4 py-2.5 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y">
            <tr v-if="!filtered.length">
              <td colspan="6" class="px-4 py-10 text-center text-muted-foreground">
                没有匹配的封禁记录
              </td>
            </tr>
            <tr v-for="b in filtered" :key="b.ip" class="hover:bg-muted/30">
              <td class="mono px-4 py-2.5 whitespace-nowrap">{{ b.ip }}</td>
              <td class="px-4 py-2.5">
                <Badge
                  :variant="b.disconnect ? 'warning' : 'destructive'"
                  :title="b.disconnect ? '只是断开了连接，没有写进封禁列表' : '已写进下载器的封禁列表'"
                >
                  {{ b.module }}
                </Badge>
              </td>
              <td class="max-w-xs truncate px-4 py-2.5" :title="b.reason">{{ b.reason }}</td>
              <td class="max-w-[10rem] truncate px-4 py-2.5 text-muted-foreground" :title="b.torrent">
                {{ b.torrent || '—' }}
              </td>
              <td class="px-4 py-2.5 whitespace-nowrap text-muted-foreground">
                <template v-if="b.disconnect">
                  <Tip text="临时断开：只把连接断掉、不加入下载器封禁列表，对端几秒后可以重连。">
                    <span class="cursor-help underline decoration-dotted underline-offset-2">临时断开</span>
                  </Tip>
                </template>
                <span v-else :title="datetime(b.untilMs)">{{ countdown(b.untilMs) }}</span>
              </td>
              <td class="px-4 py-2.5 text-right">
                <Button variant="ghost" size="icon-sm" title="解封" @click="unban(b.ip)">
                  <Trash2 />
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Card>

    <Card v-if="foreign.length">
      <div class="flex items-center gap-2">
        <Ban class="size-4 text-muted-foreground" />
        <h2 class="text-sm font-semibold">下载器中的外部封禁</h2>
        <Badge variant="muted">{{ foreign.length }}</Badge>
      </div>
      <p class="mt-2 text-xs text-muted-foreground">
        这些条目不是本服务写入的（例如你在下载器界面手工添加的），全量回写时会原样保留。
      </p>
      <div class="mt-3 flex flex-wrap gap-1.5">
        <Badge v-for="ip in foreign" :key="ip" variant="outline" class="mono">{{ ip }}</Badge>
      </div>
    </Card>

    <Dialog
      v-model:open="addOpen"
      title="手动封禁"
      description="会立即写入下载器的封禁列表，到期后自动移除"
    >
      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-2">
          <Label>IP 或网段</Label>
          <Input v-model="form.ip" class="mono" placeholder="203.0.113.5" />
          <p class="text-xs text-muted-foreground">
            单个 IP（203.0.113.5）或 CIDR 网段（203.0.113.0/24）都可以
          </p>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="flex flex-col gap-2">
            <Label>时长</Label>
            <Select
              v-model="form.duration"
              :options="DURATIONS.map((d) => ({ value: d, label: d }))"
            />
            <p class="text-xs text-muted-foreground">选「永久」表示不会自动解除</p>
          </div>
          <div class="flex flex-col gap-2">
            <Label>原因</Label>
            <Input v-model="form.reason" placeholder="手动封禁" />
            <p class="text-xs text-muted-foreground">只用于自己辨认，会显示在封禁列表里</p>
          </div>
        </div>
        <div class="flex justify-end gap-2 border-t pt-3">
          <Button variant="outline" @click="addOpen = false">取消</Button>
          <Button :loading="submitting" @click="add">封禁</Button>
        </div>
      </div>
    </Dialog>
  </div>
</template>
