<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, RouterLink, useRoute, useRouter } from 'vue-router'
import type { components } from '../api/schema'
import { ApiError, request, uploadMedia } from '../api/client'
import { handleApiError } from '../composables/useSession'
import { useDialogFocus } from '../composables/useDialogFocus'
import {
  consumePreparedLeave,
  confirmLeave,
  setUnsavedChanges,
} from '../composables/useUnsavedChanges'
import AdminLayout from '../components/AdminLayout.vue'

type ProjectEdit = components['schemas']['ProjectEdit']
type ProjectGroups = components['schemas']['ProjectGroups']
type ProjectUpdateRequest = components['schemas']['ProjectUpdateRequest']
type ProjectPublishRequest = components['schemas']['ProjectPublishRequest']

const route = useRoute()
const router = useRouter()
const isNew = computed(() => route.name === 'admin-project-new')
const projectId = computed(() => String(route.params.id ?? ''))
const pageTitle = computed(() => (isNew.value ? '新建项目' : '编辑项目'))

const name = ref('')
const githubUrl = ref('')
const imageAssetId = ref<string | null>(null)
const imagePreviewUrl = ref<string | null>(null)
const status = ref<ProjectEdit['status']>('hidden')
const version = ref(1)
const orderVersion = ref(0)

const baselineName = ref('')
const baselineGithubUrl = ref('')
const baselineImageAssetId = ref<string | null>(null)

const loading = ref(!isNew.value)
const saving = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const showHide = ref(false)
const hideDialogRef = ref<HTMLElement | null>(null)
const hideCancelRef = ref<HTMLButtonElement | null>(null)

const selectedFile = ref<File | null>(null)
const imageUploading = ref(false)
const imageUploadProgress = ref(0)
const imageUploadError = ref('')
const imageInput = ref<HTMLInputElement | null>(null)

const dirty = computed(() => (
  name.value !== baselineName.value
  || githubUrl.value !== baselineGithubUrl.value
  || imageAssetId.value !== baselineImageAssetId.value
))

watch(dirty, (value) => setUnsavedChanges(value), { immediate: true })

onMounted(() => {
  if (!isNew.value) void loadProjectAndGroups()
})

onBeforeUnmount(() => setUnsavedChanges(false))

onBeforeRouteLeave(() => {
  if (consumePreparedLeave()) return true
  return confirmLeave()
})

async function loadProjectAndGroups(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    const [project, groups] = await Promise.all([
      request<ProjectEdit>(`/projects/${projectId.value}`),
      request<ProjectGroups>('/projects'),
    ])
    applyProject(project)
    orderVersion.value = groups.order_version
  } catch (error) {
    handleApiError(error)
    errorMessage.value = error instanceof ApiError && error.status === 404 ? '项目不存在。' : '项目加载失败，请稍后重试。'
  } finally {
    loading.value = false
  }
}

function applyProject(project: ProjectEdit): void {
  name.value = project.name
  githubUrl.value = project.github_url ?? ''
  imageAssetId.value = project.image_asset_id ?? null
  imagePreviewUrl.value = project.image_preview_url ?? null
  status.value = project.status
  version.value = project.version
  baselineName.value = name.value
  baselineGithubUrl.value = githubUrl.value
  baselineImageAssetId.value = imageAssetId.value
  successMessage.value = ''
}

function updateVersionFromCreate(project: ProjectEdit): void {
  version.value = project.version
  status.value = project.status
  baselineName.value = project.name
  baselineGithubUrl.value = ''
  baselineImageAssetId.value = null
}

function updateRequestBody(currentVersion: number): ProjectUpdateRequest {
  return {
    name: name.value.trim(),
    github_url: githubUrl.value.trim() || null,
    image_asset_id: imageAssetId.value,
    version: currentVersion,
  }
}

function publishRequestBody(): ProjectPublishRequest | null {
  const github = githubUrl.value.trim()
  if (!github) {
    errorMessage.value = '公开项目必须填写 GitHub 链接。'
    return null
  }
  return {
    name: name.value.trim(),
    github_url: github,
    image_asset_id: imageAssetId.value,
    version: version.value,
    order_version: orderVersion.value,
  }
}

async function saveProject(): Promise<void> {
  if (saving.value || loading.value || imageUploading.value) return
  const creating = isNew.value
  saving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    if (creating) {
      const created = await request<ProjectEdit>('/projects', {
        method: 'POST',
        body: { name: name.value.trim() },
        csrf: true,
      })
      const pendingGithubUrl = githubUrl.value.trim()
      const pendingImageAssetId = imageAssetId.value
      await router.replace(`/projects/${created.id}`)
      updateVersionFromCreate(created)
      if (pendingGithubUrl || pendingImageAssetId) {
        const updated = await request<ProjectEdit>(`/projects/${created.id}`, {
          method: 'PUT',
          body: updateRequestBody(created.version),
          csrf: true,
        })
        applyProject(updated)
      } else {
        applyProject(created)
      }
      await refreshOrderVersion()
    } else {
      const updated = await request<ProjectEdit>(`/projects/${projectId.value}`, {
        method: 'PUT',
        body: updateRequestBody(version.value),
        csrf: true,
      })
      applyProject(updated)
    }
    successMessage.value = '项目已保存。'
  } catch (error) {
    handleApiError(error)
    errorMessage.value = formatError(error)
  } finally {
    saving.value = false
  }
}

async function publishProject(): Promise<void> {
  if (saving.value || loading.value || imageUploading.value || isNew.value) return
  const body = publishRequestBody()
  if (!body) return
  saving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const published = await request<ProjectEdit>(`/projects/${projectId.value}/publish`, {
      method: 'POST',
      body,
      csrf: true,
    })
    applyProject(published)
    await refreshOrderVersion()
    successMessage.value = '项目已公开。'
  } catch (error) {
    handleApiError(error)
    errorMessage.value = formatError(error)
  } finally {
    saving.value = false
  }
}

async function hideProject(): Promise<void> {
  if (saving.value || loading.value || imageUploading.value || isNew.value) return
  saving.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await request<void>(`/projects/${projectId.value}/hide`, {
      method: 'POST',
      body: { version: version.value, order_version: orderVersion.value },
      csrf: true,
    })
    const hidden = await request<ProjectEdit>(`/projects/${projectId.value}`)
    applyProject(hidden)
    await refreshOrderVersion()
    showHide.value = false
    successMessage.value = '项目已隐藏。'
  } catch (error) {
    handleApiError(error)
    errorMessage.value = formatError(error)
  } finally {
    saving.value = false
  }
}

async function refreshOrderVersion(): Promise<void> {
  const groups = await request<ProjectGroups>('/projects')
  orderVersion.value = groups.order_version
}

function selectImage(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  selectedFile.value = file
  imageUploadError.value = ''
  void uploadSelectedImage()
}

async function uploadSelectedImage(): Promise<void> {
  const file = selectedFile.value
  if (!file || imageUploading.value) return
  const accepted = new Set(['image/jpeg', 'image/png', 'image/webp'])
  if (!accepted.has(file.type)) {
    imageUploadError.value = '仅支持 JPEG、PNG 或 WebP 图片。'
    return
  }
  imageUploading.value = true
  imageUploadProgress.value = 0
  imageUploadError.value = ''
  try {
    const asset = await uploadMedia(file, (progress) => {
      imageUploadProgress.value = progress
    })
    imageAssetId.value = asset.id
    imagePreviewUrl.value = asset.preview_url
    imageUploadProgress.value = 100
  } catch (error) {
    handleApiError(error)
    imageUploadError.value = formatError(error)
  } finally {
    imageUploading.value = false
  }
}

function removeImage(): void {
  if (imageUploading.value) return
  imageAssetId.value = null
  imagePreviewUrl.value = null
  selectedFile.value = null
  imageUploadError.value = ''
  if (imageInput.value) imageInput.value.value = ''
}

function formatError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409 || error.problem?.code === 'version_conflict') {
      return '保存冲突：版本或公开排序已变化，请重新加载后再保存。当前输入已保留。'
    }
    if (error.problem?.code === 'session_expired') return '登录状态已过期，请重新认证后再次保存。'
    return error.problem?.title ?? error.message
  }
  return error instanceof Error ? error.message : '请求失败，请稍后重试。'
}

function closeHide(): void {
  showHide.value = false
}

useDialogFocus(showHide, hideDialogRef, hideCancelRef, closeHide)
</script>

<template>
  <AdminLayout>
    <header class="admin-page-header">
      <div>
        <p class="eyebrow">项目管理</p>
        <div class="heading-row">
          <h1>{{ pageTitle }}</h1>
          <span v-if="!isNew" class="status" :class="status === 'public' ? 'status--solid' : 'status--outline'">{{ status === 'public' ? '公开' : '隐藏' }}</span>
        </div>
        <p class="page-lede">填写项目名称、GitHub 链接和项目图片。</p>
      </div>
      <RouterLink class="text-link" to="/projects">返回项目管理</RouterLink>
    </header>

    <div class="admin-content">
      <p v-if="loading" class="loading-state" role="status">加载中…</p>
      <form v-else class="editor-form project-editor-form" @submit.prevent="saveProject">
        <div v-if="errorMessage" class="notice notice--error" role="alert">{{ errorMessage }}</div>
        <div v-if="successMessage" class="notice notice--success" role="status">{{ successMessage }}</div>

        <div class="editor-actions" aria-label="项目操作">
          <RouterLink class="button button--secondary" to="/projects">返回列表</RouterLink>
          <div class="editor-actions__group">
            <button class="button button--secondary" type="submit" :disabled="saving || imageUploading">
              {{ saving ? '保存中…' : isNew ? '保存项目' : '保存更改' }}
            </button>
            <button v-if="!isNew && status === 'hidden'" class="button button--primary" type="button" :disabled="saving || imageUploading" @click="publishProject">公开项目</button>
            <button v-if="!isNew && status === 'public'" class="button button--danger" type="button" :disabled="saving || imageUploading" @click="showHide = true">隐藏项目</button>
          </div>
        </div>

        <div class="field">
          <label for="project-name">项目名称</label>
          <input id="project-name" v-model="name" name="name" class="input" type="text" placeholder="输入项目名称" required />
        </div>
        <div class="field">
          <label for="project-github">GitHub 链接</label>
          <input id="project-github" v-model="githubUrl" name="github_url" class="input" type="url" placeholder="https://github.com/…" />
        </div>
        <div class="field">
          <label for="project-image">项目图片</label>
          <span id="project-image-hint" class="field__hint">支持 JPEG、PNG、WebP。上传后还需保存项目或公开项目才会绑定。</span>
          <div v-if="imagePreviewUrl" class="image-upload-preview">
            <img :src="imagePreviewUrl" alt="项目图片预览" />
            <button class="button button--secondary" type="button" :disabled="imageUploading" @click="removeImage">移除图片</button>
          </div>
          <p v-else class="image-upload-empty">暂无项目图片，将使用统一默认图。</p>
          <input
            id="project-image"
            ref="imageInput"
            class="input"
            type="file"
            accept="image/jpeg,image/png,image/webp"
            aria-describedby="project-image-hint"
            :disabled="imageUploading"
            @change="selectImage"
          />
          <progress v-if="imageUploading" class="upload-progress" max="100" :value="imageUploadProgress">{{ imageUploadProgress }}%</progress>
          <p v-if="imageUploading" class="field__hint" role="status">上传中 {{ imageUploadProgress }}%</p>
          <p v-if="imageUploadError" class="field__hint field__hint--error" role="alert">{{ imageUploadError }}</p>
          <button v-if="imageUploadError && selectedFile && !imageUploading" class="button button--secondary" type="button" @click="uploadSelectedImage">重试上传</button>
        </div>
      </form>
    </div>
  </AdminLayout>

  <div v-if="showHide" class="dialog-backdrop" role="presentation">
    <section ref="hideDialogRef" class="dialog" role="dialog" aria-modal="true" aria-labelledby="hide-project-title">
      <h2 id="hide-project-title">隐藏这个项目？</h2>
      <p>隐藏后项目将从公开站点移除，但项目资料不会被删除。</p>
      <div class="dialog__actions">
        <button ref="hideCancelRef" class="button button--secondary" type="button" :disabled="saving" @click="closeHide">取消</button>
        <button class="button button--danger" type="button" :disabled="saving" @click="hideProject">确认隐藏</button>
      </div>
    </section>
  </div>
</template>
