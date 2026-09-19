<script setup lang="ts">
import { RouterLink, useRoute } from 'vue-router'

const route = useRoute()

function isActive(section: 'articles' | 'projects'): boolean {
  return route.path.startsWith(`/${section}`)
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
          <span aria-hidden="true">文章</span>
          <span class="sr-only">管理</span>
        </RouterLink>
        <RouterLink
          to="/projects"
          :aria-current="isActive('projects') ? 'page' : undefined"
        >
          <span aria-hidden="true">项目</span>
          <span class="sr-only">管理</span>
        </RouterLink>
      </nav>

      <div class="admin-nav-footer">
        <button
          class="nav-button"
          type="button"
          disabled
          aria-describedby="logout-help"
        >
          退出
        </button>
        <p id="logout-help" class="nav-help">Stage 0 暂不提供退出功能</p>
      </div>
    </aside>

    <div class="admin-main">
      <header class="mobile-header">
        <span class="mobile-header__title">个人技术网站</span>
        <span class="mobile-header__status">管理端</span>
      </header>
      <main id="main-content" class="admin-content">
        <slot />
      </main>
    </div>
  </div>
</template>
