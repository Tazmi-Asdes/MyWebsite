<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
</script>

<template>
  <AdminLayout>
    <header class="page-header">
      <div>
        <p class="eyebrow">内容管理</p>
        <h1>项目管理</h1>
        <p class="page-lede">创建、编辑和整理公开项目。</p>
      </div>
      <RouterLink class="button button--primary" to="/projects/new">新建项目</RouterLink>
    </header>

    <section class="placeholder-panel" aria-labelledby="projects-placeholder-title">
      <span class="placeholder-index" aria-hidden="true">02</span>
      <div>
        <h2 id="projects-placeholder-title">项目列表占位</h2>
        <p>Stage 0 先建立管理端页面和路由，项目数据接入将在后续阶段完成。</p>
      </div>
    </section>
  </AdminLayout>
</template>
