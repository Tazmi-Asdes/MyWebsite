<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'

const route = useRoute()
const pageTitle = computed(() => (route.path.endsWith('/new') ? '新建文章' : '编辑文章'))
</script>

<template>
  <AdminLayout>
    <header class="page-header page-header--stacked">
      <div>
        <p class="eyebrow">文章管理</p>
        <h1>{{ pageTitle }}</h1>
        <p class="page-lede">文章编辑表单占位。</p>
      </div>
      <RouterLink class="text-link" to="/articles">返回文章管理</RouterLink>
    </header>

    <form class="editor-form" @submit.prevent>
      <div class="field">
        <label for="article-title">标题</label>
        <input id="article-title" name="title" type="text" placeholder="输入文章标题" />
      </div>
      <div class="field">
        <label for="article-summary">摘要</label>
        <textarea id="article-summary" name="summary" rows="3" placeholder="输入文章摘要" />
      </div>
      <div class="field">
        <label for="article-content">内容</label>
        <textarea id="article-content" name="content" rows="10" placeholder="使用 Markdown 编写文章内容" />
      </div>
      <div class="editor-actions">
        <button class="button button--primary" type="submit" disabled>保存（暂不可用）</button>
        <span class="stage-note">Stage 0 仅提供页面结构。</span>
      </div>
    </form>
  </AdminLayout>
</template>
