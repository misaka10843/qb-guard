<script setup>
import { computed, ref } from 'vue'
import { api } from '@/lib/api'
import { guard } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Input, Label, Tip } from '@/components/ui'
import { Play, CircleCheck, CircleAlert, CircleSlash } from 'lucide-vue-next'
import { bytes } from '@/lib/format'

defineProps({ title: { type: String, default: '规则试运行' } })
const open = defineModel('open', { type: Boolean, default: false })

const peer = ref({
  ip: '198.51.100.7',
  port: 6881,
  peerId: '-hp0001-abcdefghijkl',
  clientName: 'hp/torrent 1.0',
  progress: 0.5,
  upSpeed: 1024,
  uploaded: 0,
})
const torrent = ref({ hash: 'abc123', name: '示例种子', size: 100000000, progress: 1 })

const result = ref(null)
const busy = ref(false)

const verdictTone = {
  ban: 'destructive',
  disconnect: 'warning',
  skip: 'muted',
  none: 'secondary',
}

async function run() {
  busy.value = true
  result.value = null
  await guard(async () => {
    result.value = await api.post('/api/modules/all/test', {
      peer: {
        ...peer.value,
        port: Number(peer.value.port) || 0,
        progress: Number(peer.value.progress) || 0,
        upSpeed: Number(peer.value.upSpeed) || 0,
        uploaded: Number(peer.value.uploaded) || 0,
      },
      torrent: { ...torrent.value, size: Number(torrent.value.size) || 0 },
    })
  })
  busy.value = false
}

const banner = computed(() => {
  const r = result.value
  if (!r) return null
  if (r.ignored) return { tone: 'warning', text: '该地址命中忽略网段，会跳过全部检查' }
  if (r.banned) return { tone: 'destructive', text: '该地址当前已被封禁' }
  if (!r.winner) return { tone: 'muted', text: '没有任何模块命中，该 Peer 会被放行' }
  return {
    tone: 'destructive',
    text: `命中 ${r.winner.module}：${r.winner.reason}（封禁 ${r.winner.duration}）`,
  }
})
</script>

<template>
  <Dialog v-model:open="open" wide :title="title" description="构造一个虚拟 Peer，跑完整判定链路，不会真的封禁">
    <div class="flex flex-col gap-4">
      <div class="grid gap-3 sm:grid-cols-3">
        <div class="flex flex-col gap-2">
          <Label>Peer IP</Label>
          <Input v-model="peer.ip" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>端口</Label>
          <Input v-model="peer.port" type="number" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>PeerID</Label>
          <Input v-model="peer.peerId" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>客户端名称</Label>
          <Input v-model="peer.clientName" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <div class="flex items-center gap-1">
            <Label>汇报进度</Label>
            <Tip text="对方自报的下载进度，0 到 1。虚假进度检查器就是拿它和「按实际上传量算出来的进度」比。" />
          </div>
          <Input v-model="peer.progress" type="number" step="0.01" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <div class="flex items-center gap-1">
            <Label>上传速度</Label>
            <Tip text="对方从本机下载的速度（字节/秒），用于主动监测这类按速率判断的模块。" />
          </div>
          <Input v-model="peer.upSpeed" type="number" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>种子名</Label>
          <Input v-model="torrent.name" class="mono" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>种子大小（字节）</Label>
          <Input v-model="torrent.size" type="number" class="mono" />
        </div>
      </div>

      <div class="flex justify-end">
        <Button :loading="busy" @click="run"><Play />运行判定</Button>
      </div>

      <template v-if="result">
        <div
          class="flex items-start gap-2 rounded-lg border p-3 text-sm"
          :class="{
            'border-destructive/40 bg-destructive/10 text-destructive': banner.tone === 'destructive',
            'border-warning/40 bg-warning/10 text-warning': banner.tone === 'warning',
            'border-border bg-muted text-muted-foreground': banner.tone === 'muted',
          }"
        >
          <CircleAlert v-if="banner.tone !== 'muted'" class="mt-0.5 size-4 shrink-0" />
          <CircleSlash v-else class="mt-0.5 size-4 shrink-0" />
          <span>{{ banner.text }}</span>
        </div>

        <div v-if="result.winner?.detail" class="rounded-lg bg-muted/50 p-3 text-xs">
          <div class="mono whitespace-pre-wrap break-all">
            {{ JSON.stringify(result.winner.detail, null, 2) }}
          </div>
        </div>

        <Card padded="false" class="overflow-hidden">
          <div class="border-b px-3 py-2 text-xs font-medium text-muted-foreground">
            全部模块结果（{{ (result.results || []).length }}）
          </div>
          <div v-if="!result.results?.length" class="p-4 text-center text-sm text-muted-foreground">
            没有模块产生结果
          </div>
          <div v-else class="flex flex-col divide-y">
            <div
              v-for="(r, i) in result.results"
              :key="i"
              class="flex items-center gap-2 px-3 py-2 text-sm"
            >
              <component
                :is="r.verdict === 'none' ? CircleSlash : CircleCheck"
                class="size-3.5 shrink-0"
                :class="r.verdict === 'none' ? 'text-muted-foreground' : 'text-warning'"
              />
              <span class="mono w-48 shrink-0 truncate">{{ r.module }}</span>
              <Badge :variant="verdictTone[r.verdict] || 'secondary'">{{ r.verdict }}</Badge>
              <span class="min-w-0 flex-1 truncate text-muted-foreground" :title="r.reason">
                {{ r.reason }}
              </span>
              <span class="mono shrink-0 text-xs text-muted-foreground">{{ r.duration }}</span>
            </div>
          </div>
        </Card>

        <p class="text-xs text-muted-foreground">
          种子大小 {{ bytes(Number(torrent.size) || 0) }}，判定只读取模块内部状态，
          不会写入下载器。
        </p>
      </template>
    </div>
  </Dialog>
</template>
