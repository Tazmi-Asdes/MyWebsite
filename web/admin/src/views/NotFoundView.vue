<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
</script>

<template>
  <AdminLayout>
    <div class="admin-content">
      <section class="not-found" aria-labelledby="not-found-title">
        <p class="not-found__code" aria-hidden="true">404</p>
        <p class="eyebrow">管理端</p>
        <h1 id="not-found-title">页面不存在</h1>
        <p>你访问的管理端页面不存在，或已被移动。</p>
        <RouterLink class="button button--primary" to="/articles">返回文章管理</RouterLink>
      </section>
    </div>
  </AdminLayout>
</template>
