<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/client'
import { initSession, login } from '../composables/useSession'

const router = useRouter()
const username = ref('')
const password = ref('')
const errorMessage = ref('')
const submitting = ref(false)

onMounted(async () => {
  const current = await initSession()
  if (current) await router.replace('/articles')
})

async function submitLogin(): Promise<void> {
  if (submitting.value) return
  errorMessage.value = ''
  submitting.value = true
  try {
    await login(username.value.trim(), password.value)
    password.value = ''
    await router.replace('/articles')
  } catch (error) {
    if (error instanceof ApiError && error.problem?.code === 'validation_failed') {
      errorMessage.value = '请输入有效的用户名和密码。'
    } else {
      errorMessage.value = '用户名或密码不正确，请检查后重试。'
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <a class="skip-link" href="#login-main">跳到登录表单</a>
  <main id="login-main" class="login-page">
    <section class="login-panel" aria-labelledby="login-title">
      <div class="login-panel__brand">个人技术网站</div>
      <h1 id="login-title">管理员登录</h1>
      <p class="login-intro">登录后管理文章与公开项目。</p>

      <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>

      <form class="form-grid" novalidate @submit.prevent="submitLogin">
        <div class="field">
          <label for="username">用户名</label>
          <input
            id="username"
            name="username"
            type="text"
            class="input"
            autocomplete="username"
            required
            autofocus
            v-model="username"
          />
        </div>

        <div class="field">
          <label for="password">密码</label>
          <input
            id="password"
            name="password"
            type="password"
            class="input"
            autocomplete="current-password"
            required
            v-model="password"
          />
        </div>

        <button class="button button--primary" type="submit" :disabled="submitting">
          {{ submitting ? '登录中…' : '登录' }}
        </button>
      </form>
    </section>
  </main>
</template>
