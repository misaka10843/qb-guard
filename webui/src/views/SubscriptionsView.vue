<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Input, Label, Switch, Tip } from '@/components/ui'
import { Plus, Trash2, RefreshCw, Rss, Pencil } from 'lucide-vue-next'
import { ago } from '@/lib/format'

const MODULE = 'ip-address-blocker-rules'

const sources = ref([])
const enabled = ref(false)
const interval = ref('4h')
const loading = ref(false)
const refreshing = ref(false)

const dialogOpen = ref(false)
const editing = ref(null)
const form = ref({ id: '', name: '', url: '', enabled: true })

const original = ref([])
const dirty = ref(false)

async function load() {
  loading.value = true
  await guard(async () => {
    const [subs, mods] = await Promise.all([
      api.get('/api/subscriptions'),
      api.get('/api/modules'),
    ])
    sources.value = (subs.subscriptions || []).map((s) => ({ ...s }))
    original.value = sources.value.map((s) => s.id)
    enabled.value = !!subs.enabled
    const mod = (mods.modules || []).find((m) => m.name === MODULE)
    if (mod?.config?.['check-interval'] !== undefined) interval.value = mod.config['check-interval']
    dirty.value = false
  })
  loading.value = false
}

function buildRules() {
  const rules = {}
  const kept = new Set()
  for (const s of sources.value) {
    kept.add(s.id)
    rules[s.id] = { name: s.name || s.id, url: s.url, enabled: s.enabled !== false }
  }
  for (const id of original.value) {
    if (!kept.has(id)) rules[id] = null
  }
  return rules
}

async function save() {
  await guard(async () => {
    await api.put(`/api/modules/${MODULE}`, {
      enabled: enabled.value,
      'check-interval': interval.value,
      rules: buildRules(),
    })
    toast.ok('订阅已保存')
    await load()
  })
}

async function refresh() {
  refreshing.value = true
  await guard(async () => {
    await api.post('/api/subscriptions/refresh')
    toast.ok('已触发刷新，稍后自动生效')
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
  if (!id || !form.value.url.trim()) {
    toast.error('ID 与 URL 都不能为空')
    return
  }
  const next = { id, name: form.value.name.trim() || id, url: form.value.url.trim(), enabled: form.value.enabled }
  const idx = sources.value.findIndex((s) => s.id === editing.value)
  if (idx >= 0) sources.value[idx] = { ...sources.value[idx], ...next }
  else if (sources.value.some((s) => s.id === id)) {
    toast.error(`ID ${id} 已存在`)
    return
  } else {
    sources.value.push({ ...next, prefixes: undefined })
  }
  dirty.value = true
  dialogOpen.value = false
}

function remove(id) {
  sources.value = sources.value.filter((s) => s.id !== id)
  dirty.value = true
}

const totalPrefixes = computed(() =>
  sources.value.reduce((n, s) => n + (Number(s.prefixes) || 0), 0),
)

onMounted(load)
watch(() => store.reloaded, load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <Card>
      <div class="flex flex-wrap items-center gap-3">
        <Rss class="size-4 text-muted-foreground" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 text-sm font-medium">
            <span>IP 集规则订阅</span>
            <Tip text="把远程维护的网段列表定期拉下来当黑名单用。列表里的每一条网段命中即封，判定过程不看任何行为特征。" />
          </div>
          <div class="text-xs text-muted-foreground">
            订阅到的网段会作为封禁依据，共 {{ totalPrefixes }} 条网段生效
          </div>
        </div>
        <div class="flex items-center gap-2">
          <Label>启用</Label>
          <Switch v-model="enabled" title="关闭后不再拉取，已拉到的网段也不再参与判定" @update:model-value="dirty = true" />
        </div>
        <div class="flex items-center gap-2">
          <Label>检查间隔</Label>
          <Input
            v-model="interval"
            class="mono w-24"
            placeholder="4h"
            title="多久重新拉一次所有源。支持 30m / 4h / 1d 这类写法"
            @input="dirty = true"
          />
        </div>
      </div>
    </Card>

    <div class="flex flex-wrap items-center gap-2">
      <Badge variant="muted">{{ sources.length }} 个源</Badge>
      <Tip v-if="dirty" text="改完记得点「保存」，否则关掉页面就丢了。">
        <Badge variant="warning" class="cursor-help">有未保存的修改</Badge>
      </Tip>
      <div class="flex-1" />
      <Button variant="ghost" size="icon-sm" :loading="loading" title="重新加载" @click="load">
        <RefreshCw />
      </Button>
      <Tip text="不等下一个检查间隔，立刻把所有源拉一遍。保存后的修改也会在这时生效。">
        <Button variant="outline" size="sm" :loading="refreshing" @click="refresh">
          <RefreshCw />立即拉取
        </Button>
      </Tip>
      <Button variant="outline" size="sm" @click="openAdd"><Plus />添加源</Button>
      <Button size="sm" :disabled="!dirty" @click="save">保存</Button>
    </div>

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
                  网段数
                  <Tip text="上次成功拉取时解析出的网段条数。「—」表示还没拉过。" />
                </span>
              </th>
              <th class="px-4 py-2.5 text-left font-medium">启用</th>
              <th class="px-4 py-2.5 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y">
            <tr v-if="!sources.length">
              <td colspan="6" class="px-4 py-10 text-center text-muted-foreground">
                还没有订阅源
              </td>
            </tr>
            <tr v-for="s in sources" :key="s.id" class="hover:bg-muted/30">
              <td class="mono px-4 py-2.5 whitespace-nowrap">{{ s.id }}</td>
              <td class="px-4 py-2.5">{{ s.name }}</td>
              <td class="mono max-w-xs truncate px-4 py-2.5 text-muted-foreground" :title="s.url">
                {{ s.url }}
              </td>
              <td class="mono px-4 py-2.5">{{ s.prefixes ?? '—' }}</td>
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

    <p class="text-xs text-muted-foreground">
      支持纯 IP / CIDR、DAT/eMule 区间（start,end,level）以及 # 与 // 注释。
      单行写错的只跳过那一行，不影响整份列表。
    </p>

    <Dialog
      v-model:open="dialogOpen"
      :title="editing ? '编辑订阅源' : '添加订阅源'"
      description="这里填的是网段列表文件的地址，不是单个 IP"
    >
      <div class="flex flex-col gap-4">
        <div class="flex flex-col gap-2">
          <Label>ID</Label>
          <Input v-model="form.id" class="mono" :disabled="!!editing" placeholder="my-ipset" />
          <p class="text-xs text-muted-foreground">
            这个名字只在本服务内部用，随便取；保存后就别改了，改 ID 等于删掉旧源再加一个新源
          </p>
        </div>
        <div class="flex flex-col gap-2">
          <Label>名称</Label>
          <Input v-model="form.name" placeholder="留空则用 ID" />
        </div>
        <div class="flex flex-col gap-2">
          <Label>URL</Label>
          <Input v-model="form.url" class="mono" placeholder="https://example.com/list.txt" />
          <p class="text-xs text-muted-foreground">
            每次拉取失败时会退回上次缓存的内容；只有从没成功拉过才会提示失败
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
