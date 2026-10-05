<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Input, Label, Switch, Tabs, Tip } from '@/components/ui'
import { Plus, Trash2, RefreshCw, Radar, Pencil, AlertTriangle, Copy } from 'lucide-vue-next'
import { ago } from '@/lib/format'

const data = ref(null)
const loading = ref(false)
const refreshing = ref(false)
const saving = ref(false)
const tab = ref('sources')

const sources = ref([])
const enabled = ref(false)
const interval = ref('24h')
const original = ref([])
const dirty = ref(false)

const dialogOpen = ref(false)
const editing = ref(null)
const form = ref({ id: '', name: '', url: '', enabled: true })

const snapshot = computed(() => data.value?.snapshot || {})
const downloader = computed(() => data.value?.downloader || null)
const list = computed(() => snapshot.value.list || [])

const duplicateWarning = computed(() => {
  const d = downloader.value
  if (!d || !enabled.value) return ''
  if (d.addTrackersFromURL) {
    return '下载器自己也在从 URL 订阅 tracker，两部分会重复。建议在下载器设置里关掉它，只保留本页的聚合。'
  }
  return ''
})

async function load() {
  loading.value = true
  await guard(async () => {
    data.value = await api.get('/api/trackers')
    const srcs = data.value.sources || []
    sources.value = srcs.map((s) => ({ ...s }))
    original.value = srcs.map((s) => s.id)
    enabled.value = !!data.value.enabled
    interval.value = data.value.refreshInterval || '24h'
    dirty.value = false
  })
  loading.value = false
}

function buildSources() {
  const out = {}
  const kept = new Set()
  for (const s of sources.value) {
    kept.add(s.id)
    out[s.id] = { name: s.name || s.id, url: s.url, enabled: s.enabled !== false }
  }
  for (const id of original.value) {
    if (!kept.has(id)) out[id] = null
  }
  return out
}

async function save() {
  saving.value = true
  await guard(async () => {
    await api.put('/api/config', {
      trackers: {
        enabled: enabled.value,
        'refresh-interval': interval.value,
        sources: buildSources(),
      },
    })
    toast.ok('Tracker 配置已保存')
    await load()
  })
  saving.value = false
}

async function refresh() {
  refreshing.value = true
  await guard(async () => {
    await api.post('/api/trackers/refresh')
    toast.ok('已开始刷新')
    setTimeout(load, 1500)
  })
  refreshing.value = false
}

function openAdd() {
  editing.value = null
  form.value = { id: '', name: '', url: '', enabled: true }
  dialogOpen.value = true
}

function openEdit(s) {
  editing.value = s.id
  form.value = { id: s.id, name: s.name, url: s.url, enabled: s.enabled !== false }
  dialogOpen.value = true
}

function commit() {
  const id = form.value.id.trim()
  const url = form.value.url.trim()
  if (!id || !url) {
    toast.error('ID 与 URL 都不能为空')
    return
  }
  if (!/^https?:\/\//i.test(url)) {
    toast.error('源地址必须是 http/https 链接（这里填的是列表文件的地址，不是 tracker 地址）')
    return
  }
  const next = { id, name: form.value.name.trim() || id, url, enabled: form.value.enabled }
  const idx = sources.value.findIndex((s) => s.id === editing.value)
  if (idx >= 0) sources.value[idx] = { ...sources.value[idx], ...next }
  else if (sources.value.some((s) => s.id === id)) {
    toast.error(`ID ${id} 已存在`)
    return
  } else {
    sources.value.push(next)
  }
  dirty.value = true
  dialogOpen.value = false
}

function remove(id) {
  sources.value = sources.value.filter((s) => s.id !== id)
  dirty.value = true
}

async function copyAll() {
  const text = list.value.join('\n')
  try {
    await navigator.clipboard.writeText(text)
    toast.ok(`已复制 ${list.value.length} 条 tracker`)
  } catch {
    toast.error('复制失败，请手动选择')
  }
}

onMounted(load)
watch(() => store.trackers, load)
watch(() => store.reloaded, load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <Card>
      <div class="flex flex-wrap items-center gap-3">
        <Radar class="size-4 text-muted-foreground" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-sm font-medium">
            <span>Tracker 订阅聚合</span>
            <Tip text="把多个公开 tracker 列表合并去重后，写进下载器的「添加的 Tracker 列表」。tracker 越多，能找到的 Peer 越多，下载通常越快。" />
          </div>
          <div class="text-xs text-muted-foreground">
            把多个列表合并去重后写入下载器的「添加的 Tracker 列表」。下载器自带的 URL 订阅只支持一个地址，多源聚合需要在这里做。
          </div>
        </div>
        <div class="flex items-center gap-2">
          <Label>启用</Label>
          <Switch
            v-model="enabled"
            title="关闭不会清空下载器里已有的列表，只是不再由本服务更新它"
            @update:model-value="dirty = true"
          />
        </div>
        <div class="flex items-center gap-2">
          <Label>刷新间隔</Label>
          <Input
            v-model="interval"
            class="mono w-24"
            placeholder="24h"
            title="多久重新拉一次所有源。tracker 列表变动很慢，一天一次足够"
            @input="dirty = true"
          />
        </div>
      </div>
    </Card>

    <div
      v-if="duplicateWarning"
      class="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-warning"
    >
      <AlertTriangle class="mt-0.5 size-4 shrink-0" />
      <span>{{ duplicateWarning }}</span>
    </div>

    <div
      v-if="snapshot.lastError"
      class="flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
    >
      <AlertTriangle class="mt-0.5 size-4 shrink-0" />
      <span>{{ snapshot.lastError }}</span>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <Badge variant="muted">{{ sources.length }} 个源</Badge>
      <Tip text="所有源合并、去重、并剔除无效地址之后的条数。这就是最终写入下载器的内容。">
        <Badge variant="secondary" class="cursor-help">合并后 {{ snapshot.total ?? 0 }} 条</Badge>
      </Tip>
      <Badge v-if="snapshot.running" variant="warning">正在刷新</Badge>
      <Tip v-if="dirty" text="改完记得点「保存」，否则关掉页面就丢了。">
        <Badge variant="warning" class="cursor-help">有未保存的修改</Badge>
      </Tip>
      <span class="text-xs text-muted-foreground">
        上次刷新 {{ ago(snapshot.lastRun) }}<template v-if="snapshot.pushedAt">，写入下载器 {{ ago(snapshot.pushedAt) }}</template>
      </span>
      <div class="flex-1" />
      <Button variant="ghost" size="icon-sm" :loading="loading" title="重新加载" @click="load">
        <RefreshCw />
      </Button>
      <Tip text="立刻把所有源拉一遍并重新合并。合并结果和上次一样就不会写下载器。">
        <Button variant="outline" size="sm" :loading="refreshing" @click="refresh">
          <RefreshCw />立即刷新
        </Button>
      </Tip>
      <Button variant="outline" size="sm" @click="openAdd"><Plus />添加源</Button>
      <Button size="sm" :loading="saving" :disabled="!dirty" @click="save">保存</Button>
    </div>

    <Tabs
      v-model="tab"
      :tabs="[
        { value: 'sources', label: '订阅源' },
        { value: 'merged', label: `合并结果 (${snapshot.total ?? 0})` },
      ]"
    >
      <template #sources>
        <Card padded="false" class="overflow-hidden">
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  <th class="px-4 py-2.5 text-left font-medium">ID</th>
                  <th class="px-4 py-2.5 text-left font-medium">名称</th>
                  <th class="px-4 py-2.5 text-left font-medium">地址</th>
                  <th class="px-4 py-2.5 text-left font-medium">
                    <span class="inline-flex items-center gap-1">
                      解析
                      <Tip text="这个源上次拉取时读出了多少条 tracker。「—」表示还没拉过。" />
                    </span>
                  </th>
                  <th class="px-4 py-2.5 text-left font-medium">
                    <span class="inline-flex items-center gap-1">
                      去重后新增
                      <Tip text="这个源贡献了多少条前面没出现过的 tracker。数值低说明它和别的源大量重叠。" />
                    </span>
                  </th>
                  <th class="px-4 py-2.5 text-left font-medium">启用</th>
                  <th class="px-4 py-2.5 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody class="divide-y">
                <tr v-if="!sources.length">
                  <td colspan="7" class="px-4 py-10 text-center text-muted-foreground">
                    还没有订阅源
                  </td>
                </tr>
                <tr v-for="s in sources" :key="s.id" class="hover:bg-muted/30">
                  <td class="mono px-4 py-2.5 whitespace-nowrap">{{ s.id }}</td>
                  <td class="px-4 py-2.5">{{ s.name }}</td>
                  <td class="mono max-w-xs truncate px-4 py-2.5 text-muted-foreground" :title="s.url">
                    {{ s.url }}
                  </td>
                  <td class="mono px-4 py-2.5">{{ snapshot.stats?.[s.id]?.count ?? '—' }}</td>
                  <td class="mono px-4 py-2.5">{{ snapshot.stats?.[s.id]?.added ?? '—' }}</td>
                  <td class="px-4 py-2.5">
                    <Badge :variant="s.enabled !== false ? 'success' : 'muted'">
                      {{ s.enabled !== false ? '启用' : '停用' }}
                    </Badge>
                  </td>
                  <td class="px-4 py-2.5 text-right whitespace-nowrap">
                    <Button variant="ghost" size="icon-sm" title="编辑" @click="openEdit(s)">
                      <Pencil />
                    </Button>
                    <Button variant="ghost" size="icon-sm" title="删除" @click="remove(s.id)">
                      <Trash2 />
                    </Button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </Card>

        <p class="mt-3 text-xs text-muted-foreground">
          内置源走 <span class="mono">gh.huoshen80.top</span> 反代，因为
          <span class="mono">raw.githubusercontent.com</span> 在常见网络环境下不可达。
          换源直接改地址即可。
        </p>
      </template>

      <template #merged>
        <Card padded="false" class="overflow-hidden">
          <div class="flex items-center gap-2 border-b px-4 py-2.5">
            <span class="flex items-center gap-1 text-xs text-muted-foreground">
              去重后写入下载器的完整列表
              <Tip text="与下载器当前 add_trackers 完全一致。点「复制全部」可以拿去别处用。" />
            </span>
            <div class="flex-1" />
            <Button variant="outline" size="xs" :disabled="!list.length" @click="copyAll">
              <Copy />复制全部
            </Button>
          </div>
          <div v-if="!list.length" class="px-4 py-10 text-center text-sm text-muted-foreground">
            还没有合并结果，点击「立即刷新」拉取
          </div>
          <div v-else class="max-h-[28rem] overflow-y-auto">
            <div
              v-for="(t, i) in list"
              :key="t"
              class="mono flex items-center gap-3 border-b px-4 py-1.5 text-xs last:border-0"
            >
              <span class="w-8 shrink-0 text-muted-foreground">{{ i + 1 }}</span>
              <span class="truncate">{{ t }}</span>
            </div>
          </div>
        </Card>
      </template>
    </Tabs>

    <Card v-if="downloader">
      <div class="flex items-center gap-1">
        <h2 class="text-sm font-semibold">下载器侧现状</h2>
        <Tip text="直接读下载器的偏好设置。用来确认本服务的写入有没有真的生效、以及下载器自己是不是也在订阅 tracker。" />
      </div>
      <div class="mt-3 grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <div class="text-xs text-muted-foreground">当前 add_trackers 条数</div>
          <div class="mono">{{ downloader.addTrackersCount }}</div>
        </div>
        <div>
          <div class="text-xs text-muted-foreground">自动加到新任务</div>
          <div class="mono">{{ downloader.addTrackersEnabled ? '已开启' : '未开启' }}</div>
        </div>
        <div>
          <div class="text-xs text-muted-foreground">自带的 URL 订阅</div>
          <div class="mono">{{ downloader.addTrackersFromURL ? '已开启' : '未开启' }}</div>
        </div>
        <div class="min-w-0">
          <div class="text-xs text-muted-foreground">自带订阅地址</div>
          <div class="mono truncate" :title="downloader.addTrackersURL || ''">
            {{ downloader.addTrackersURL || '—' }}
          </div>
        </div>
      </div>
    </Card>

    <p class="text-xs text-muted-foreground">
      注意：启用后本服务接管下载器的 <span class="mono">add_trackers</span>，
      你在下载器界面里手改的条目会被覆盖；写入只影响新任务，已有任务不会补加 tracker。
      关闭本功能不会清空下载器里已有的列表。
    </p>

    <Dialog
      v-model:open="dialogOpen"
      :title="editing ? '编辑订阅源' : '添加订阅源'"
      description="这里填的是 tracker 列表文件的地址，不是 tracker 本身"
    >
      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-2">
          <Label>ID</Label>
          <Input v-model="form.id" class="mono" :disabled="!!editing" placeholder="my-list" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>名称</Label>
          <Input v-model="form.name" placeholder="留空则用 ID" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>列表地址</Label>
          <Input
            v-model="form.url"
            class="mono"
            placeholder="https://gh.huoshen80.top/https://raw.githubusercontent.com/XIU2/TrackersListCollection/master/best.txt"
          />
          <p class="text-xs text-muted-foreground">
            填 tracker 列表文件（一行一个 tracker）的地址，不是单个 tracker。
            走 GitHub 上的列表时如果连不上 raw.githubusercontent.com，在前面加
            <span class="mono">https://gh.huoshen80.top/</span> 即可
          </p>
        </div>
        <div class="flex items-center gap-2">
          <Switch v-model="form.enabled" />
          <Label>启用</Label>
        </div>
        <div class="flex justify-end gap-2 border-t pt-3">
          <Button variant="outline" @click="dialogOpen = false">取消</Button>
          <Button @click="commit">确定</Button>
        </div>
      </div>
    </Dialog>
  </div>
</template>
