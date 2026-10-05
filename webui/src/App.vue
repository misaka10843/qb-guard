<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { authStatus, onUnauthorized } from '@/lib/api'
import LoginView from '@/components/LoginView.vue'
import AppShell from '@/components/AppShell.vue'
import Toaster from '@/components/ui/Toaster.vue'

const ready = ref(false)
const authed = ref(false)
const username = ref('')

let off = null

onMounted(async () => {
  off = onUnauthorized(() => {
    authed.value = false
  })
  try {
    const res = await authStatus()
    authed.value = !!res.authenticated
    username.value = res.username || ''
  } catch {
    authed.value = false
  }
  ready.value = true
})

onUnmounted(() => off && off())
</script>

<template>
  <div v-if="!ready" class="flex min-h-screen items-center justify-center">
    <div class="size-6 animate-spin rounded-full border-2 border-muted border-t-foreground" />
  </div>
  <LoginView
    v-else-if="!authed"
    @success="
      (name) => {
        username = name
        authed = true
      }
    "
  />
  <AppShell v-else :username="username" @logout="authed = false" />
  <Toaster />
</template>
