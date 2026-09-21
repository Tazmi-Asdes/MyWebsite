<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { components } from '../api/schema'
import { ApiError, request } from '../api/client'
import { handleApiError } from '../composables/useSession'
import AdminLayout from '../components/AdminLayout.vue'

type ArticleSummary = components['schemas']['ArticleSummary']
type ArticleListResponse = components['schemas']['ArticleListResponse']
type ArticleStatus = 'draft' | 'published'

const items = ref<ArticleSummary[]>([])
const page = ref(1)
const totalPages = ref(1)
const total = ref(0)
const query = ref('')
const searchInput = ref('')
const status = ref<'all' | ArticleStatus>('all')
const loading = ref(false)
const errorMessage = ref('')

onMounted(() => {
  void loadArticles()
})

async function loadArticles(nextPage = page.value): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  const params = new URLSearchParams({ page: String(nextPage), per_page: '20' })
  if (query.value) params.set('q', query.value)
  if (status.value !== 'all') params.set('status', status.value)
  try {
    const response = await request<ArticleListResponse>(`/articles?${params.toString()}`)
    items.value = response.items
    page.value = response.pagination.page
    totalPages.value = Math.max(response.pagination.total_pages, 1)
    total.value = response.pagination.total
  } catch (error) {
    handleApiError(error)
    if (error instanceof ApiError && error.problem?.code === 'authentication_required') {
      errorMessage.value = '请先登录后管理文章。'
    } else {
      errorMessage.value = '文章列表加载失败，请稍后重试。'
    }
  } finally {
    loading.value = false
  }
}

function submitSearch(): void {
  query.value = searchInput.value.trim()
  void loadArticles(1)
}

function updateStatus(value: string): void {
  status.value = value === 'draft' || value === 'published' ? value : 'all'
  void loadArticles(1)
}

function formatDate(value: string | null | undefined, withTime = false): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return value
  return new Intl.DateTimeFormat('zh-CN', withTime ? {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  } : { year: 'numeric', month: '2-digit', day: '2-digit' }).format(date)
}

function statusLabel(value: ArticleStatus): string {
  return value === 'published' ? '已发布' : '草稿'
}
</script>

<template>
  <AdminLayout>
    <header class="admin-page-header">
      <div>
        <p class="eyebrow">内容管理</p>
        <h1>文章管理</h1>
      </div>
      <RouterLink class="button button--primary button--small" to="/articles/new">新建文章</RouterLink>
    </header>

    <section class="admin-content" aria-label="文章列表">
      <form class="admin-toolbar" @submit.prevent="submitSearch">
        <div class="admin-toolbar__filters">
          <div class="field">
            <label for="article-search">搜索标题</label>
            <input id="article-search" v-model="searchInput" class="input" type="search" placeholder="输入文章标题" />
          </div>
          <div class="field">
            <label for="article-status">状态</label>
            <select id="article-status" class="select" :value="status" @change="updateStatus(($event.target as HTMLSelectElement).value)">
              <option value="all">全部状态</option>
              <option value="published">已发布</option>
              <option value="draft">草稿</option>
            </select>
          </div>
          <button class="button button--secondary" type="submit">搜索</button>
        </div>
        <p class="muted">共 {{ total }} 篇文章 · 每页 20 篇</p>
      </form>

      <p v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</p>
      <p v-if="loading" class="loading-state" role="status">加载中…</p>

      <template v-if="!loading && items.length">
        <table class="data-table">
          <caption class="sr-only">文章列表</caption>
          <thead>
            <tr><th scope="col">标题</th><th scope="col">状态</th><th scope="col">首次发布</th><th scope="col">最后修改</th><th scope="col"><span class="sr-only">操作</span></th></tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.id">
              <td>{{ item.title }}</td>
              <td><span class="status" :class="item.status === 'published' ? 'status--solid' : 'status--outline'">{{ statusLabel(item.status) }}</span></td>
              <td>{{ formatDate(item.first_published_at) }}</td>
              <td>{{ formatDate(item.updated_at, true) }}</td>
              <td><RouterLink class="text-link" :to="`/articles/${item.id}`">编辑</RouterLink></td>
            </tr>
          </tbody>
        </table>

        <div class="mobile-records" aria-label="文章列表（移动版）">
          <article v-for="item in items" :key="`mobile-${item.id}`" class="record-card">
            <div class="record-card__head"><h3>{{ item.title }}</h3><span class="status" :class="item.status === 'published' ? 'status--solid' : 'status--outline'">{{ statusLabel(item.status) }}</span></div>
            <dl><dt>首次发布</dt><dd>{{ formatDate(item.first_published_at) }}</dd><dt>最后修改</dt><dd>{{ formatDate(item.updated_at, true) }}</dd></dl>
            <RouterLink class="text-link" :to="`/articles/${item.id}`">编辑文章</RouterLink>
          </article>
        </div>

        <nav v-if="totalPages > 1" class="pagination" aria-label="文章分页">
          <button class="button button--secondary" type="button" :disabled="page <= 1" @click="loadArticles(page - 1)">上一页</button>
          <span>第 {{ page }} / {{ totalPages }} 页</span>
          <button class="button button--secondary" type="button" :disabled="page >= totalPages" @click="loadArticles(page + 1)">下一页</button>
        </nav>
      </template>

      <section v-else-if="!loading" class="empty-state" aria-labelledby="articles-empty-title">
        <div><h2 id="articles-empty-title">还没有文章</h2><p>先创建一篇草稿，再决定何时公开发布。</p><RouterLink class="button button--primary" to="/articles/new">新建文章</RouterLink></div>
      </section>

    </section>
  </AdminLayout>
</template>
