<script setup>
import { ref } from 'vue'
import { login } from '@/lib/api'
import { Button, Card, Input, Label } from '@/components/ui'
import { ShieldCheck, Loader2 } from 'lucide-vue-next'

const emit = defineEmits(['success'])

const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const res = await login(username.value, password.value)
    emit('success', res.username || username.value)
  } catch (err) {
    error.value = err?.message || '登录失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center p-4">
    <div class="w-full max-w-sm">
      <div class="mb-6 flex flex-col items-center gap-2 text-center">
        <div class="flex size-11 items-center justify-center rounded-xl border bg-card">
          <ShieldCheck class="size-5" />
        </div>
        <h1 class="text-lg font-semibold">qB 下载守卫</h1>
        <p class="text-sm text-muted-foreground">请用 qBittorrent WebUI 的账号和密码登录</p>
      </div>

      <Card>
        <form class="flex flex-col gap-4" @submit.prevent="submit">
          <div class="flex flex-col gap-2">
            <Label for="login-user">用户名</Label>
            <Input
              id="login-user"
              v-model="username"
              autocomplete="username"
              placeholder="qBittorrent 的用户名"
              autofocus
            />
          </div>
          <div class="flex flex-col gap-2">
            <Label for="login-pass">密码</Label>
            <Input
              id="login-pass"
              v-model="password"
              type="password"
              autocomplete="current-password"
              placeholder="qBittorrent 的密码"
            />
          </div>
          <p v-if="error" class="text-sm text-destructive">{{ error }}</p>
          <Button type="submit" :loading="busy" class="w-full">
            <Loader2 v-if="busy" class="animate-spin" />
            登录
          </Button>
        </form>
      </Card>

      <p class="mt-4 text-center text-xs text-muted-foreground">
        账号密码取自配置文件里的 <span class="mono">qbittorrent</span> 段，不会发送给下载器
      </p>
    </div>
  </div>
</template>
