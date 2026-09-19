<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'

const route = useRoute()
const pageTitle = computed(() => (route.path.endsWith('/new') ? '新建项目' : '编辑项目'))
</script>

<template>
  <AdminLayout>
    <header class="page-header page-header--stacked">
      <div>
        <p class="eyebrow">项目管理</p>
        <h1>{{ pageTitle }}</h1>
        <p class="page-lede">项目编辑表单占位。</p>
      </div>
      <RouterLink class="text-link" to="/projects">返回项目管理</RouterLink>
    </header>

    <form class="editor-form" @submit.prevent>
      <div class="field">
        <label for="project-name">项目名称</label>
        <input id="project-name" name="name" type="text" placeholder="输入项目名称" />
      </div>
      <div class="field">
        <label for="project-description">项目简介</label>
        <textarea id="project-description" name="description" rows="5" placeholder="输入项目简介" />
      </div>
      <div class="field">
        <label for="project-link">项目链接</label>
        <input id="project-link" name="link" type="url" placeholder="https://" />
      </div>
      <div class="editor-actions">
        <button class="button button--primary" type="submit" disabled>保存（暂不可用）</button>
        <span class="stage-note">Stage 0 仅提供页面结构。</span>
      </div>
    </form>
  </AdminLayout>
</template>
