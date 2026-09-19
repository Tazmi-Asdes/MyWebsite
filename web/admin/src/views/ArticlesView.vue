<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
</script>

<template>
  <AdminLayout>
    <header class="page-header">
      <div>
        <p class="eyebrow">内容管理</p>
        <h1>文章管理</h1>
        <p class="page-lede">创建、编辑和整理公开文章。</p>
      </div>
      <RouterLink class="button button--primary" to="/articles/new">新建文章</RouterLink>
    </header>

    <section class="placeholder-panel" aria-labelledby="articles-placeholder-title">
      <span class="placeholder-index" aria-hidden="true">01</span>
      <div>
        <h2 id="articles-placeholder-title">文章列表占位</h2>
        <p>Stage 0 先建立管理端页面和路由，文章数据接入将在后续阶段完成。</p>
      </div>
    </section>
  </AdminLayout>
</template>
