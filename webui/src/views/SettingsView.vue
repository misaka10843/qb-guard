<script setup>
import { computed, onMounted, ref } from 'vue'
import { api } from '@/lib/api'
import { store } from '@/lib/store'
import { guard, toast } from '@/lib/toast'
import { Badge, Button, Card, Dialog, Tip } from '@/components/ui'
import ConfigField from '@/components/ConfigField.vue'
import { Save, RefreshCw, AlertTriangle, RotateCw } from 'lucide-vue-next'

const sections = ref([])
const cfg = ref({})
const loading = ref(false)
const saving = ref(false)
const savedSnapshot = ref('')
const restartOpen = ref(false)
const restartFields = ref([])

const dirty = computed(() => JSON.stringify(cfg.value) !== savedSnapshot.value)

const visible = computed(() => sections.value.filter((s) => s.key !== 'trackers'))

async function load() {
  loading.value = true
  await guard(async () => {
    const [schema, config] = await Promise.all([
      api.get('/api/config/schema'),
      api.get('/api/config'),
    ])
    sections.value = schema.sections || []
    cfg.value = config
    savedSnapshot.value = JSON.stringify(config)
  })
  loading.value = false
}

function sectionValues(section) {
  if (section.key === 'root') {
    const out = {}
    for (const f of section.fields) out[f.key] = cfg.value[f.key]
    return out
  }
  return cfg.value[section.key] || {}
}

async function save() {
  saving.value = true
  await guard(async () => {
    const patch = {}
    for (const section of visible.value) {
      if (section.key === 'root') {
        for (const f of section.fields) patch[f.key] = cfg.value[f.key]
      } else {
        patch[section.key] = cfg.value[section.key]
      }
    }
    const res = await api.put('/api/config', patch)
    savedSnapshot.value = JSON.stringify(cfg.value)
    if (res.needRestart?.length) {
      restartFields.value = res.needRestart
      restartOpen.value = true
    } else {
      toast.ok('配置已保存并热重载')
    }
  })
  saving.value = false
}

async function reload() {
  await guard(async () => {
    await api.post('/api/reload')
    toast.ok('已重新加载模块')
  })
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-center gap-2">
      <p class="flex-1 text-sm text-muted-foreground">
        改完点「保存」即时生效，不必重启。时长支持
        <span class="mono">5s</span> / <span class="mono">72h</span> /
        <span class="mono">7d</span> / <span class="mono">2w</span>，也可以直接写毫秒整数。
      </p>
      <Button variant="ghost" size="icon-sm" :loading="loading" title="重新加载" @click="load">
        <RefreshCw />
      </Button>
      <Tip text="重新读一遍配置文件并重建所有检测模块。改了脚本文件（data-dir/scripts/*.expr）后需要点这里。">
        <Button variant="outline" size="sm" @click="reload"><RotateCw />重载模块</Button>
      </Tip>
      <Button size="sm" :loading="saving" :disabled="!dirty" @click="save"><Save />保存</Button>
    </div>

    <Tip v-if="dirty" text="改完记得点「保存」，否则关掉页面就丢了。">
      <Badge variant="warning" class="w-fit cursor-help">有未保存的修改</Badge>
    </Tip>

    <Card v-for="section in visible" :key="section.key">
      <div class="flex items-center gap-2">
        <h2 class="text-sm font-semibold">{{ section.label }}</h2>
        <span class="mono text-xs text-muted-foreground">{{ section.key }}</span>
      </div>
      <p v-if="section.help" class="mt-1.5 text-xs text-muted-foreground">{{ section.help }}</p>
      <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <ConfigField
          v-for="f in section.fields"
          :key="f.key"
          :cfg="section.key === 'root' ? cfg : sectionValues(section)"
          :field="f"
        />
      </div>
    </Card>

    <Dialog
      v-model:open="restartOpen"
      title="配置已保存，部分字段需要重启"
      description="下面这几项在进程启动时就被读入并固化（端口已经绑定、路径已经展开），没法热重载"
    >
      <div class="flex flex-col gap-3">
        <div class="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-warning">
          <AlertTriangle class="mt-0.5 size-4 shrink-0" />
          <span>其余字段已即时生效，不必重启。要生效请手动重启本服务（容器部署执行 <span class="mono">docker compose restart qb-guard</span>）。</span>
        </div>
        <div class="flex flex-wrap gap-1.5">
          <Badge v-for="f in restartFields" :key="f" variant="outline" class="mono">{{ f }}</Badge>
        </div>
        <div class="flex justify-end border-t pt-3">
          <Button @click="restartOpen = false">知道了</Button>
        </div>
      </div>
    </Dialog>
  </div>
</template>
