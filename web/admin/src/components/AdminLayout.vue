<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { clearSession, isSessionTerminationError, logout } from '../composables/useSession'
import {
  cancelPreparedLeave,
  confirmLeave,
  hasUnsavedChanges,
  prepareNextLeave,
} from '../composables/useUnsavedChanges'

const route = useRoute()
const router = useRouter()
const logoutError = ref('')

function isActive(section: 'articles' | 'projects'): boolean {
  return route.path.startsWith(`/${section}`)
}

async function signOut(): Promise<void> {
  if (hasUnsavedChanges() && !confirmLeave()) return
  if (hasUnsavedChanges()) prepareNextLeave()
  logoutError.value = ''
  try {
    await logout()
    await router.replace('/login')
  } catch (error) {
    if (isSessionTerminationError(error)) {
      clearSession()
      await router.replace('/login')
      return
    }
    cancelPreparedLeave()
    logoutError.value = '退出失败，请稍后重试。'
  }
}
</script>

<template>
  <div class="admin-shell">
    <a class="skip-link" href="#main-content">跳到主要内容</a>

    <aside class="admin-sidebar" aria-label="管理端导航">
      <div class="brand-block">
        <span class="brand-mark" aria-hidden="true">M</span>
        <span>个人技术网站</span>
      </div>

      <nav class="admin-nav" aria-label="内容管理">
        <RouterLink
          to="/articles"
          :aria-current="isActive('articles') ? 'page' : undefined"
        >
          文章管理
        </RouterLink>
        <RouterLink
          to="/projects"
          :aria-current="isActive('projects') ? 'page' : undefined"
        >
          项目管理
        </RouterLink>
      </nav>

      <div class="admin-nav-footer">
        <button
          class="nav-button"
          type="button"
          @click="signOut"
        >
          退出
        </button>
      </div>
    </aside>

    <div class="admin-main">
      <header class="mobile-header">
        <span class="mobile-header__title">个人技术网站</span>
        <span class="mobile-header__status">管理端</span>
      </header>
      <main id="main-content" class="admin-content">
        <p v-if="logoutError" class="notice notice--error" role="alert">{{ logoutError }}</p>
        <slot />
      </main>
    </div>
  </div>
</template>
