<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, RouterLink } from 'vue-router'
import type { components } from '../api/schema'
import { ApiError, request } from '../api/client'
import { handleApiError } from '../composables/useSession'
import {
  consumePreparedLeave,
  confirmLeave,
  setUnsavedChanges,
} from '../composables/useUnsavedChanges'
import AdminLayout from '../components/AdminLayout.vue'

type ProjectEdit = components['schemas']['ProjectEdit']
type ProjectGroups = components['schemas']['ProjectGroups']
type ProjectOrderResult = components['schemas']['ProjectOrderResult']

const publicProjects = ref<ProjectEdit[]>([])
const hiddenProjects = ref<ProjectEdit[]>([])
const baselinePublicIds = ref<number[]>([])
const orderVersion = ref(0)
const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const draggedId = ref<number | null>(null)

const publicIds = computed(() => publicProjects.value.map((project) => project.id))
const dirty = computed(() => !sameIds(publicIds.value, baselinePublicIds.value))

watch(dirty, (value) => setUnsavedChanges(value), { immediate: true })

onMounted(() => {
  void loadGroups()
})

onBeforeUnmount(() => setUnsavedChanges(false))

onBeforeRouteLeave(() => {
  if (consumePreparedLeave()) return true
  return confirmLeave()
})

async function loadGroups(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await request<ProjectGroups>('/projects')
    applyGroups(result)
  } catch (error) {
    handleApiError(error)
    errorMessage.value = '项目列表加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function applyGroups(result: ProjectGroups): void {
  publicProjects.value = Array.isArray(result?.public) ? result.public : []
  hiddenProjects.value = Array.isArray(result?.hidden) ? result.hidden : []
  orderVersion.value = typeof result?.order_version === 'number' ? result.order_version : 0
  baselinePublicIds.value = publicProjects.value.map((project) => project.id)
  successMessage.value = ''
}

function sameIds(left: number[], right: number[]): boolean {
  return left.length === right.length && left.every((id, index) => id === right[index])
}

function moveProject(index: number, delta: -1 | 1): void {
  if (saving.value) return
  const nextIndex = index + delta
  if (nextIndex < 0 || nextIndex >= publicProjects.value.length) return
  const next = [...publicProjects.value]
  const [project] = next.splice(index, 1)
  next.splice(nextIndex, 0, project)
  publicProjects.value = next
  errorMessage.value = ''
  successMessage.value = ''
}

function handleDragStart(event: DragEvent, id: number): void {
  if (saving.value) {
    event.preventDefault()
    return
  }
  draggedId.value = id
  event.dataTransfer?.setData('text/plain', String(id))
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}

function handleDrop(event: DragEvent, targetIndex: number): void {
  event.preventDefault()
  if (saving.value) return
  const sourceId = draggedId.value ?? Number(event.dataTransfer?.getData('text/plain'))
  draggedId.value = null
  if (!Number.isFinite(sourceId)) return
  const sourceIndex = publicProjects.value.findIndex((project) => project.id === sourceId)
  if (sourceIndex < 0 || sourceIndex === targetIndex) return
  const next = [...publicProjects.value]
  const [project] = next.splice(sourceIndex, 1)
  next.splice(targetIndex, 0, project)
  publicProjects.value = next
  errorMessage.value = ''
  successMessage.value = ''
}

function handleDragEnd(): void {
  draggedId.value = null
}

async function saveOrder(): Promise<void> {
  if (saving.value || loading.value || !dirty.value) return
  saving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const result = await request<ProjectOrderResult>('/projects/order', {
      method: 'PUT',
      body: { public_ids: publicIds.value, order_version: orderVersion.value },
      csrf: true,
    })
    orderVersion.value = result.order_version
    baselinePublicIds.value = [...publicIds.value]
    successMessage.value = '公开项目顺序已保存。'
  } catch (error) {
    handleApiError(error)
    if (isConflict(error)) {
      errorMessage.value = '保存冲突：公开项目顺序已在其他位置更新，请刷新后再保存。当前顺序已保留。'
    } else {
      errorMessage.value = formatError(error)
    }
  } finally {
    saving.value = false
  }
}

function isConflict(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 409 || error.problem?.code === 'version_conflict')
}

function formatError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.problem?.code === 'session_expired') return '登录状态已过期，请重新认证后再次点击保存。'
    return error.problem?.title ?? error.message
  }
  return '请求失败，请稍后重试。'
}
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

    <p v-if="loading" class="loading-state" role="status">加载中…</p>
    <template v-else>
      <div v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</div>
      <div v-if="successMessage" class="notice notice--success" role="status">{{ successMessage }}</div>

      <section class="projects-section" aria-labelledby="public-projects-title">
        <div class="projects-section__header">
          <div>
            <p class="eyebrow">公开组</p>
            <h2 id="public-projects-title">公开项目</h2>
          </div>
          <button class="button button--secondary" type="button" :disabled="saving || !dirty" @click="saveOrder">
            {{ saving ? '保存中…' : '保存顺序' }}
          </button>
        </div>
        <p class="field__hint">拖动项目调整顺序，也可以使用每项的上移/下移按钮。调整只会在点击“保存顺序”后提交。</p>
        <div v-if="publicProjects.length === 0" class="empty-state project-group-empty">
          <div>
            <h3>暂无公开项目</h3>
            <p>新建项目并发布后，会显示在公开组中。</p>
          </div>
        </div>
        <ol v-else class="project-list" aria-label="公开项目排序">
          <li
            v-for="(project, index) in publicProjects"
            :key="project.id"
            class="project-list__item"
            :class="{ 'project-list__item--dragging': draggedId === project.id }"
            draggable="true"
            @dragstart="handleDragStart($event, project.id)"
            @dragover.prevent
            @drop="handleDrop($event, index)"
            @dragend="handleDragEnd"
          >
            <span class="project-list__handle" aria-hidden="true">⋮⋮</span>
            <div class="project-list__content">
              <RouterLink class="project-list__name" :to="`/projects/${project.id}`">{{ project.name }}</RouterLink>
              <span v-if="project.github_url" class="project-list__meta">{{ project.github_url }}</span>
            </div>
            <div class="project-list__actions">
              <button class="button button--small button--secondary" type="button" :disabled="saving || index === 0" @click="moveProject(index, -1)">上移</button>
              <button class="button button--small button--secondary" type="button" :disabled="saving || index === publicProjects.length - 1" @click="moveProject(index, 1)">下移</button>
              <RouterLink class="text-link" :to="`/projects/${project.id}`">编辑</RouterLink>
            </div>
          </li>
        </ol>
      </section>

      <section class="projects-section" aria-labelledby="hidden-projects-title">
        <div class="projects-section__header">
          <div>
            <p class="eyebrow">隐藏组</p>
            <h2 id="hidden-projects-title">隐藏项目</h2>
          </div>
        </div>
        <p class="field__hint">隐藏项目不参与公开排序。</p>
        <div v-if="hiddenProjects.length === 0" class="empty-state project-group-empty">
          <div>
            <h3>暂无隐藏项目</h3>
            <p>保存为草稿的新项目会显示在隐藏组中。</p>
          </div>
        </div>
        <ul v-else class="project-list project-list--hidden" aria-label="隐藏项目">
          <li v-for="project in hiddenProjects" :key="project.id" class="project-list__item">
            <div class="project-list__content">
              <RouterLink class="project-list__name" :to="`/projects/${project.id}`">{{ project.name }}</RouterLink>
              <span class="status status--outline">隐藏</span>
            </div>
            <RouterLink class="text-link" :to="`/projects/${project.id}`">编辑</RouterLink>
          </li>
        </ul>
      </section>
    </template>
  </AdminLayout>
</template>
