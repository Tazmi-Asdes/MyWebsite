<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, RouterLink, useRoute, useRouter } from 'vue-router'
import type { components } from '../api/schema'
import { ApiError, request } from '../api/client'
import { handleApiError } from '../composables/useSession'
import {
  consumePreparedLeave,
  confirmLeave,
  setUnsavedChanges,
} from '../composables/useUnsavedChanges'
import AdminLayout from '../components/AdminLayout.vue'

type ArticleEdit = components['schemas']['ArticleEdit']
type ArticleWriteRequest = components['schemas']['ArticleWriteRequest']

const route = useRoute()
const router = useRouter()
const isNew = computed(() => route.name === 'admin-article-new')
const articleId = computed(() => String(route.params.id ?? ''))
const pageTitle = computed(() => (isNew.value ? '新建文章' : '编辑文章'))

const title = ref('')
const body = ref('')
const version = ref(1)
const status = ref<ArticleEdit['status']>('draft')
const publicUlid = ref<string | null>(null)
const firstPublishedAt = ref<string | null>(null)
const baselineTitle = ref('')
const baselineBody = ref('')
const baselineVersion = ref(1)
const loading = ref(!isNew.value)
const saving = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const showWithdraw = ref(false)

const dirty = computed(() => title.value !== baselineTitle.value || body.value !== baselineBody.value || version.value !== baselineVersion.value)

watch(dirty, (value) => setUnsavedChanges(value), { immediate: true })

onMounted(() => {
  window.addEventListener('beforeunload', handleBeforeUnload)
  if (!isNew.value) void loadArticle()
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', handleBeforeUnload)
  setUnsavedChanges(false)
})

onBeforeRouteLeave(() => {
  if (consumePreparedLeave()) return true
  return confirmLeave()
})

function handleBeforeUnload(event: BeforeUnloadEvent): void {
  if (!dirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

async function loadArticle(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await request<ArticleEdit>(`/articles/${articleId.value}`)
    applyArticle(result)
  } catch (error) {
    handleApiError(error)
    errorMessage.value = error instanceof ApiError && error.status === 404 ? '文章不存在。' : '文章加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function applyArticle(article: ArticleEdit): void {
  title.value = article.title
  body.value = article.body_markdown ?? ''
  version.value = article.version
  status.value = article.status
  publicUlid.value = article.public_ulid ?? null
  firstPublishedAt.value = article.first_published_at ?? null
  baselineTitle.value = title.value
  baselineBody.value = body.value
  baselineVersion.value = version.value
  successMessage.value = ''
}

function requestBody(): ArticleWriteRequest {
  return { title: title.value, body_markdown: body.value.trim() ? body.value : null, version: version.value }
}

function updateFromResponse(article: ArticleEdit): void {
  version.value = article.version
  status.value = article.status
  publicUlid.value = article.public_ulid ?? null
  firstPublishedAt.value = article.first_published_at ?? null
  baselineTitle.value = title.value
  baselineBody.value = body.value
  baselineVersion.value = version.value
}

async function saveArticle(): Promise<void> {
  if (saving.value || loading.value) return
  await persist('save')
}

async function publishArticle(): Promise<void> {
  if (saving.value || loading.value) return
  if (isNew.value) {
    errorMessage.value = '请先保存草稿后发布。'
    return
  }
  if (!body.value.trim()) {
    errorMessage.value = '发布前请填写文章正文。'
    return
  }
  await persist('publish')
}

async function persist(action: 'save' | 'publish'): Promise<void> {
  saving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    let result: ArticleEdit
    if (isNew.value) {
      result = await request<ArticleEdit>('/articles', { method: 'POST', body: requestBody(), csrf: true })
      await router.replace(`/articles/${result.id}`)
    } else if (action === 'publish') {
      result = await request<ArticleEdit>(`/articles/${articleId.value}/publish`, { method: 'POST', body: requestBody(), csrf: true })
    } else {
      result = await request<ArticleEdit>(`/articles/${articleId.value}`, { method: 'PUT', body: requestBody(), csrf: true })
    }
    updateFromResponse(result)
    successMessage.value = action === 'publish' ? '文章已发布。' : '文章已保存。'
  } catch (error) {
    handleApiError(error)
    errorMessage.value = formatError(error)
  } finally {
    saving.value = false
  }
}

async function withdrawArticle(): Promise<void> {
  if (saving.value || loading.value) return
  saving.value = true
  errorMessage.value = ''
  try {
    await request<void>(`/articles/${articleId.value}/withdraw`, {
      method: 'POST',
      body: { version: version.value },
      csrf: true,
    })
    const result = await request<ArticleEdit>(`/articles/${articleId.value}`)
    updateFromResponse(result)
    status.value = 'draft'
    successMessage.value = '文章已撤回为草稿。'
    showWithdraw.value = false
  } catch (error) {
    handleApiError(error)
    errorMessage.value = formatError(error)
  } finally {
    saving.value = false
  }
}

function formatError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409 || error.problem?.code === 'version_conflict') {
      const current = error.problem?.errors?.version?.[0]?.match(/^current=(\d+)$/)?.[1]
      return current ? `保存冲突：服务器当前版本为 ${current}，请重新加载后再保存。当前输入已保留。` : '保存冲突：服务器版本已变化，请重新加载后再保存。当前输入已保留。'
    }
    if (error.problem?.code === 'session_expired') return '登录状态已过期，请重新认证后再次点击保存。'
    return error.problem?.title ?? error.message
  }
  return '请求失败，请稍后重试。'
}

function closeWithdraw(): void {
  showWithdraw.value = false
}
</script>

<template>
  <AdminLayout>
    <header class="page-header page-header--stacked">
      <div>
        <p class="eyebrow">文章管理</p>
        <div class="heading-row">
          <h1>{{ pageTitle }}</h1>
          <span v-if="!isNew" class="status" :class="status === 'published' ? 'status--solid' : 'status--outline'">{{ status === 'published' ? '已发布' : '草稿' }}</span>
        </div>
      </div>
      <div class="heading-links">
        <a v-if="publicUlid" class="text-link" :href="`/articles/${publicUlid}`" target="_blank" rel="noreferrer">查看公开文章</a>
        <RouterLink class="text-link" to="/articles">返回文章管理</RouterLink>
      </div>
    </header>

    <p v-if="loading" class="loading-state" role="status">加载中…</p>
    <form v-else class="editor-form" @submit.prevent="saveArticle">
      <div v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</div>
      <div v-if="successMessage" class="notice notice--success" role="status">{{ successMessage }}</div>

      <div class="editor-actions" aria-label="文章操作">
        <RouterLink class="button button--secondary" to="/articles">返回列表</RouterLink>
        <div class="editor-actions__group">
          <button class="button button--secondary" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存草稿' }}</button>
          <button class="button button--primary" type="button" :disabled="saving || isNew" :aria-describedby="isNew ? 'publish-hint' : undefined" @click="publishArticle">发布文章</button>
          <button v-if="!isNew && status === 'published'" class="button button--danger" type="button" :disabled="saving" @click="showWithdraw = true">撤回文章</button>
        </div>
        <p v-if="isNew" id="publish-hint" class="muted editor-actions__hint">请先保存草稿后发布。</p>
      </div>

      <div class="field">
        <label for="article-title">标题</label>
        <input id="article-title" v-model="title" name="title" class="input" type="text" placeholder="输入文章标题" required />
      </div>
      <div class="field">
        <label for="article-body">Markdown 正文</label>
        <span id="article-body-hint" class="field__hint">草稿允许正文为空；发布前必须填写正文。首版不提供草稿预览。</span>
        <textarea id="article-body" v-model="body" name="body" class="textarea" rows="18" aria-describedby="article-body-hint" placeholder="使用 Markdown 编写文章内容" />
      </div>
    </form>
  </AdminLayout>

  <div v-if="showWithdraw" class="dialog-backdrop" role="presentation">
    <section class="dialog" role="dialog" aria-modal="true" aria-labelledby="withdraw-title">
      <h2 id="withdraw-title">撤回这篇文章？</h2>
      <p>撤回后文章转为草稿，原公开地址将显示 404。正文不会被删除。</p>
      <div class="dialog__actions">
        <button class="button button--secondary" type="button" @click="closeWithdraw">取消</button>
        <button class="button button--danger" type="button" :disabled="saving" @click="withdrawArticle">确认撤回</button>
      </div>
    </section>
  </div>
</template>
