<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
const sidebarOpen = ref(false)
const menuButtonRef = ref<HTMLButtonElement | null>(null)

function isActive(section: 'articles' | 'projects'): boolean {
  return route.path.startsWith(`/${section}`)
}

function closeSidebar(): void {
  sidebarOpen.value = false
}

function handleKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || !sidebarOpen.value) return
  closeSidebar()
  menuButtonRef.value?.focus()
}

watch(() => route.fullPath, closeSidebar)

onMounted(() => document.addEventListener('keydown', handleKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', handleKeydown))

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

    <aside
      id="admin-navigation"
      class="admin-sidebar"
      :class="{ 'is-open': sidebarOpen }"
      aria-label="后台导航"
    >
      <div class="admin-sidebar__brand">个人技术网站</div>

      <nav class="admin-nav" aria-label="内容管理">
        <RouterLink
          to="/articles"
          :aria-current="isActive('articles') ? 'page' : undefined"
          @click="closeSidebar"
        >
          文章管理
        </RouterLink>
        <RouterLink
          to="/projects"
          :aria-current="isActive('projects') ? 'page' : undefined"
          @click="closeSidebar"
        >
          项目管理
        </RouterLink>
      </nav>

      <nav class="admin-nav admin-nav--bottom" aria-label="账户与站点">
        <a href="/" target="_blank" rel="noreferrer" @click="closeSidebar">查看公开站</a>
        <button
          class="nav-button"
          type="button"
          @click="signOut"
        >
          退出
        </button>
      </nav>
    </aside>

    <div class="admin-main">
      <header class="admin-mobile-header">
        <span class="admin-mobile-header__brand">个人技术网站</span>
        <button
          ref="menuButtonRef"
          class="menu-button"
          type="button"
          aria-label="切换后台导航"
          aria-controls="admin-navigation"
          :aria-expanded="sidebarOpen"
          @click="sidebarOpen = !sidebarOpen"
        >
          菜单
        </button>
      </header>
      <main id="main-content" class="admin-content">
        <p v-if="logoutError" class="notice notice--error" role="alert">{{ logoutError }}</p>
        <slot />
      </main>
    </div>
  </div>
</template>
