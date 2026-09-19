<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterView } from 'vue-router'
import { reauthenticate, closeReauth, initSession, reauthError, reauthOpen } from './composables/useSession'

const password = ref('')
const submitting = ref(false)

onMounted(() => {
  void initSession()
})

async function submitReauth(): Promise<void> {
  if (!password.value || submitting.value) return
  submitting.value = true
  try {
    await reauthenticate(password.value)
    password.value = ''
  } catch (error) {
    reauthError.value = error instanceof Error ? error.message : '重新认证失败，请重试。'
  } finally {
    submitting.value = false
  }
}

function cancelReauth(): void {
  password.value = ''
  closeReauth()
}
</script>

<template>
  <RouterView />

  <div v-if="reauthOpen" class="dialog-backdrop" role="presentation">
    <section class="dialog" role="dialog" aria-modal="true" aria-labelledby="session-title">
      <h2 id="session-title">登录状态已过期</h2>
      <p>当前页面和未保存内容已保留。重新登录后可继续保存。</p>
      <form class="form-stack" @submit.prevent="submitReauth">
        <div class="field">
          <label for="session-password">密码</label>
          <input
            id="session-password"
            v-model="password"
            class="input"
            type="password"
            autocomplete="current-password"
            autofocus
          />
        </div>
        <p v-if="reauthError" class="notice notice--error" role="alert">{{ reauthError }}</p>
        <div class="dialog__actions">
          <button class="button button--secondary" type="button" @click="cancelReauth">取消</button>
          <button class="button button--primary" type="submit" :disabled="submitting || !password">
            {{ submitting ? '认证中…' : '重新登录' }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
