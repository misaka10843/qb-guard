<script setup>
import { computed, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Input, Label, Select, Switch, Tip } from '@/components/ui'
import { Plus, Trash2, Wand2, CircleCheck, CircleAlert, ChevronDown } from 'lucide-vue-next'

const props = defineProps({
  module: { type: String, required: true },
  title: { type: String, default: '' },
})
const open = defineModel('open', { type: Boolean, default: false })

const METHODS = ['STARTS_WITH', 'ENDS_WITH', 'CONTAINS', 'EQUALS', 'LENGTH', 'REGEX']
const RESULTS = [
  { value: '', label: '默认' },
  { value: 'TRUE', label: 'TRUE（命中）' },
  { value: 'FALSE', label: 'FALSE（排除）' },
  { value: 'DEFAULT', label: 'DEFAULT（继续）' },
]

const METHOD_HINT =
  'STARTS_WITH 以…开头、ENDS_WITH 以…结尾、CONTAINS 包含、EQUALS 完全相同、LENGTH 按长度范围（配 min/max）、REGEX 正则。除 REGEX 外一律忽略大小写；REGEX 是全串匹配且区分大小写。'

const HIT_HINT =
  '本条规则的内容被匹配上时给出什么结论。TRUE = 判定为封禁；FALSE = 判定为「排除」，优先级最高，一旦命中立刻跳过其余所有规则；DEFAULT = 本条不作结论，继续看后面的规则。'

const MISS_HINT =
  '本条规则的内容没匹配上时给出什么结论。默认的 DEFAULT 表示不作结论。想用前置条件做白名单，要把「条件未命中时」改成 FALSE。'

const IF_HINT =
  '给本条规则加一个门槛：只有前置条件成立时才会检查它。例如「仅当客户端名以 -qB 开头时才检查 -hp 规则」。'

const rules = ref([])
const checks = ref([])
const loading = ref(false)
const saving = ref(false)
const expanded = ref(-1)

function blank() {
  return { method: 'STARTS_WITH', content: '', hit: '', miss: '', hasIf: false, if: null }
}

function blankIf() {
  return { method: 'STARTS_WITH', content: '', hit: '', miss: '' }
}

function parse(raw) {
  return raw.map((line) => {
    try {
      const o = JSON.parse(line)
      const item = {
        method: o.method || 'STARTS_WITH',
        content: o.content ?? '',
        min: o.min ?? 0,
        max: o.max ?? 0,
        hit: o.hit || '',
        miss: o.miss || '',
        hasIf: !!o.if,
        if: o.if ? { ...blankIf(), ...o.if } : null,
      }
      return item
    } catch {
      return { ...blank(), broken: line }
    }
  })
}

function serialize(r) {
  const o = { method: r.method }
  if (r.method === 'LENGTH') {
    o.min = Number(r.min) || 0
    o.max = Number(r.max) || 0
  } else {
    o.content = r.content
  }
  if (r.hasIf && r.if) {
    o.if = { method: r.if.method, content: r.if.content }
    if (r.if.hit) o.if.hit = r.if.hit
    if (r.if.miss) o.if.miss = r.if.miss
  }
  if (r.hit) o.hit = r.hit
  if (r.miss) o.miss = r.miss
  return JSON.stringify(o)
}

const payload = computed(() => rules.value.map(serialize))

async function load() {
  loading.value = true
  await guard(async () => {
    const res = await api.get(`/api/rules/${props.module}`)
    rules.value = parse(res.rules || [])
    checks.value = []
  })
  loading.value = false
}

async function validate() {
  await guard(async () => {
    const res = await api.post(`/api/rules/${props.module}/validate`, { rules: payload.value })
    checks.value = res.results || []
    const bad = checks.value.filter((c) => !c.ok).length
    if (bad) toast.error(`有 ${bad} 条规则有问题`)
    else toast.ok('全部规则校验通过')
  })
}

async function save() {
  saving.value = true
  await guard(async () => {
    const res = await api.put(`/api/rules/${props.module}`, { rules: payload.value })
    toast.ok(`已保存 ${res.count} 条规则`)
    open.value = false
  }, null)
  saving.value = false
}

function add() {
  rules.value.push(blank())
  expanded.value = rules.value.length - 1
}

function remove(i) {
  rules.value.splice(i, 1)
  checks.value = []
}

function describe(r) {
  if (r.broken) return r.broken
  const parts = [r.method]
  if (r.method === 'LENGTH') parts.push(`${r.min}~${r.max}`)
  else parts.push(`"${r.content}"`)
  if (r.hasIf && r.if) parts.push(`若 ${r.if.method} "${r.if.content}"`)
  if (r.hit) parts.push(`命中→${r.hit}`)
  if (r.miss) parts.push(`未命中→${r.miss}`)
  return parts.join(' ')
}

const summary = computed(() => {
  const total = rules.value.length
  const bad = checks.value.filter((c) => !c.ok).length
  return { total, bad }
})

watch(open, (v) => v && load())
</script>

<template>
  <Dialog
    v-model:open="open"
    wide
    :title="title || '规则列表'"
    description="每条规则是一个 JSON 对象，命中即封禁；hit 设为 FALSE 可做排除（优先级最高）"
  >
    <div class="flex flex-col gap-3">
      <div class="flex flex-wrap items-center gap-2">
        <Badge variant="muted">{{ summary.total }} 条</Badge>
        <Badge v-if="summary.bad" variant="destructive">{{ summary.bad }} 条有问题</Badge>
        <div class="flex-1" />
        <Tip text="只检查语法（正则能不能编译、长度范围合不合理），不会真的拿 Peer 数据跑判定。想跑判定用「试运行」。">
          <Button variant="outline" size="sm" @click="validate">
            <Wand2 />校验
          </Button>
        </Tip>
        <Button variant="outline" size="sm" @click="add"><Plus />添加</Button>
      </div>

      <div v-if="loading" class="py-8 text-center text-sm text-muted-foreground">加载中…</div>

      <div v-else-if="!rules.length" class="py-8 text-center text-sm text-muted-foreground">
        规则列表为空，点击「添加」新建一条
      </div>

      <div v-else class="flex flex-col gap-2">
        <Card v-for="(r, i) in rules" :key="i" padded="false" class="overflow-hidden">
          <div class="flex items-center gap-2 p-2.5">
            <button
              class="flex min-w-0 flex-1 items-center gap-2 text-left"
              @click="expanded = expanded === i ? -1 : i"
            >
              <ChevronDown
                class="size-4 shrink-0 text-muted-foreground transition-transform"
                :class="expanded === i ? '' : '-rotate-90'"
              />
              <span class="mono min-w-0 flex-1 truncate text-xs">{{ describe(r) }}</span>
            </button>
            <component
              v-if="checks[i]"
              :is="checks[i].ok ? CircleCheck : CircleAlert"
              class="size-4 shrink-0"
              :class="checks[i].ok ? 'text-success' : 'text-destructive'"
              :title="checks[i].error || checks[i].note"
            />
            <Button variant="ghost" size="icon-sm" title="删除" @click="remove(i)">
              <Trash2 />
            </Button>
          </div>

          <div v-if="checks[i] && !checks[i].ok" class="px-3 pb-2 text-xs text-destructive">
            {{ checks[i].error }}
          </div>

          <div v-if="expanded === i" class="flex flex-col gap-3 border-t p-3">
            <div class="grid gap-3 sm:grid-cols-2">
              <div class="flex flex-col gap-2">
                <div class="flex items-center gap-1">
                  <Label>匹配方式</Label>
                  <Tip :text="METHOD_HINT" />
                </div>
                <Select
                  v-model="r.method"
                  :options="METHODS.map((m) => ({ value: m, label: m }))"
                />
              </div>
              <div v-if="r.method === 'LENGTH'" class="grid grid-cols-2 gap-3">
                <div class="flex flex-col gap-2">
                  <Label>最短长度</Label>
                  <Input v-model="r.min" type="number" class="mono" />
                </div>
                <div class="flex flex-col gap-2">
                  <Label>最长长度</Label>
                  <Input v-model="r.max" type="number" class="mono" />
                </div>
              </div>
              <div v-else class="flex flex-col gap-2">
                <Label>匹配内容</Label>
                <Input v-model="r.content" class="mono" placeholder="例如 -hp" />
              </div>
            </div>

            <div class="grid gap-3 sm:grid-cols-2">
              <div class="flex flex-col gap-2">
                <div class="flex items-center gap-1">
                  <Label>命中时</Label>
                  <Tip :text="HIT_HINT" />
                </div>
                <Select v-model="r.hit" :options="RESULTS" />
              </div>
              <div class="flex flex-col gap-2">
                <div class="flex items-center gap-1">
                  <Label>未命中时</Label>
                  <Tip :text="MISS_HINT" />
                </div>
                <Select v-model="r.miss" :options="RESULTS" />
              </div>
            </div>

            <div class="flex items-center gap-2">
              <Switch v-model="r.hasIf" />
              <Label>附加前置条件（仅在条件成立时才检查本条）</Label>
              <Tip :text="IF_HINT" />
            </div>

            <div v-if="r.hasIf" class="grid gap-3 rounded-lg bg-muted/50 p-3 sm:grid-cols-2">
              <div class="flex flex-col gap-2">
                <Label>条件方式</Label>
                <Select
                  v-model="r.if.method"
                  :options="METHODS.map((m) => ({ value: m, label: m }))"
                />
              </div>
              <div class="flex flex-col gap-2">
                <Label>条件内容</Label>
                <Input v-model="r.if.content" class="mono" placeholder="例如 -qB" />
              </div>
              <div class="flex flex-col gap-2">
                <Label>条件命中时</Label>
                <Select v-model="r.if.hit" :options="RESULTS" />
              </div>
              <div class="flex flex-col gap-2">
                <Label>条件未命中时</Label>
                <Select v-model="r.if.miss" :options="RESULTS" />
              </div>
              <p class="text-xs text-muted-foreground sm:col-span-2">
                条件未命中时默认是 DEFAULT（不排除本条）。要做白名单必须把这里改成 FALSE。
              </p>
            </div>
          </div>
        </Card>
      </div>

      <div class="flex justify-end gap-2 border-t pt-3">
        <Button variant="outline" @click="open = false">取消</Button>
        <Button :loading="saving" @click="save">保存</Button>
      </div>
    </div>
  </Dialog>
</template>
