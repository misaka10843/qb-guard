<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard } from '@/lib/toast'
import { Badge, Button, Card, Switch, Tip } from '@/components/ui'
import ConfigField from '@/components/ConfigField.vue'
import RulesEditor from '@/components/RulesEditor.vue'
import TestDialog from '@/components/TestDialog.vue'
import { ListChecks, Play, Save, Rss, RefreshCw } from 'lucide-vue-next'

const emit = defineEmits(['goto'])

const modules = ref([])
const schema = ref([])
const drafts = reactive({})
const busy = reactive({})
const loading = ref(false)

const ruleModule = ref('')
const ruleTitle = ref('')
const ruleOpen = ref(false)
const testOpen = ref(false)

const SUBSCRIPTION_MODULES = ['ip-address-blocker-rules']

function editableFields(mod) {
  const def = schema.value.find((s) => s.key === mod.name)
  if (!def) return []
  return def.fields.filter(
    (f) => f.key !== 'enabled' && !['rules', 'subscriptions', 'trackerSources'].includes(f.type),
  )
}

function actionField(mod) {
  const def = schema.value.find((s) => s.key === mod.name)
  return def?.fields.find((f) => ['rules', 'subscriptions'].includes(f.type))
}

async function load() {
  loading.value = true
  await guard(async () => {
    const [mods, sch] = await Promise.all([api.get('/api/modules'), api.get('/api/config/schema')])
    modules.value = mods.modules || []
    schema.value = sch.modules || []
    for (const m of modules.value) {
      drafts[m.name] = JSON.parse(JSON.stringify(m.config || {}))
      if (drafts[m.name].enabled === undefined) drafts[m.name].enabled = false
    }
  })
  loading.value = false
}

async function save(mod) {
  busy[mod.name] = true
  await guard(
    () => api.put(`/api/modules/${mod.name}`, drafts[mod.name]),
    `${mod.label || mod.name} 已保存`,
  )
  busy[mod.name] = false
  await load()
}

async function toggle(mod, value) {
  drafts[mod.name].enabled = value
  await save(mod)
}

function openRules(mod) {
  ruleModule.value = mod.name
  ruleTitle.value = `${mod.label || mod.name} · 规则列表`
  ruleOpen.value = true
}

function status(mod) {
  if (!mod.enabled) {
    return { variant: 'muted', label: '已关闭', hint: '已关闭的模块完全不参与判定，也不会写入任何封禁。' }
  }
  if (mod.active) {
    return {
      variant: 'success',
      label: '运行中',
      hint: '每轮检测都会调用这个模块。任一模块判定为封禁即封禁该 Peer。',
    }
  }
  return {
    variant: 'warning',
    label: '独立循环',
    hint: '已启用且在工作，但不参与 Peer 判定，由自己的循环承担（例如主动监测按日统计上传量）。',
  }
}

const CONFIGURED_HINT =
  '配置文件里还没有这个模块的段落，当前用的是内置默认值。在页面上点一次「保存」就会把它写进配置。'

onMounted(load)
watch(() => store.reloaded, load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center gap-2">
      <p class="flex-1 text-sm text-muted-foreground">
        每个模块负责一类反吸血判定，命中即封禁该 Peer。
        <Tip text="模块关闭时完全不参与判定。全部关闭即为只读模式，本服务不会向下载器写入任何封禁——想先观察一段时间再启用时可以这样用。">
          <span class="cursor-help underline decoration-dotted underline-offset-2">全部关闭即只读</span>
        </Tip>
      </p>
      <Button variant="ghost" size="icon-sm" :loading="loading" title="重新加载" @click="load">
        <RefreshCw />
      </Button>
    </div>

    <Card v-for="mod in modules" :key="mod.name">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-sm font-semibold">{{ mod.label || mod.name }}</h2>
        <Tip :text="status(mod).hint">
          <Badge :variant="status(mod).variant" class="cursor-help">{{ status(mod).label }}</Badge>
        </Tip>
        <Tip v-if="!mod.configured" :text="CONFIGURED_HINT">
          <Badge variant="outline" class="cursor-help">未配置</Badge>
        </Tip>
        <span class="mono text-xs text-muted-foreground">{{ mod.name }}</span>
        <div class="flex-1" />
        <Switch
          :model-value="drafts[mod.name]?.enabled === true"
          :title="mod.enabled ? '点击关闭该模块，立即生效' : '点击启用该模块，立即生效'"
          @update:model-value="(v) => toggle(mod, v)"
        />
      </div>

      <p v-if="mod.help" class="mt-2 text-xs text-muted-foreground">{{ mod.help }}</p>

      <div v-if="editableFields(mod).length" class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <ConfigField
          v-for="f in editableFields(mod)"
          :key="f.key"
          :cfg="drafts[mod.name]"
          :field="f"
        />
      </div>

      <div class="mt-4 flex flex-wrap items-center gap-2 border-t pt-4">
        <Button v-if="actionField(mod)?.type === 'rules'" variant="outline" size="sm" @click="openRules(mod)">
          <ListChecks />编辑规则
        </Button>
        <Button
          v-else-if="actionField(mod)?.type === 'subscriptions'"
          variant="outline"
          size="sm"
          @click="emit('goto', 'subs')"
        >
          <Rss />在「IP 集订阅」页管理
        </Button>
        <Tip text="不真的封禁任何人，只拿一条模拟的 Peer / 任务数据跑一遍判定，用来确认规则写得对不对。">
          <Button variant="outline" size="sm" @click="testOpen = true"><Play />试运行</Button>
        </Tip>
        <div class="flex-1" />
        <Button size="sm" :loading="busy[mod.name]" @click="save(mod)"><Save />保存</Button>
      </div>
    </Card>

    <RulesEditor v-model:open="ruleOpen" :module="ruleModule" :title="ruleTitle" />
    <TestDialog v-model:open="testOpen" />
  </div>
</template>
