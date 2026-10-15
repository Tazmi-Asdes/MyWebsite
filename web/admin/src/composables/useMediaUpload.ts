import { ref } from 'vue'
import { uploadMedia, type MediaAsset } from '../api/client'

const acceptedImageTypes = new Set(['image/jpeg', 'image/png', 'image/webp'])

interface UseMediaUploadOptions {
  formatError: (error: unknown) => string
  onSuccess: (asset: MediaAsset) => void
  onError?: (error: unknown) => void
}

export function useMediaUpload(options: UseMediaUploadOptions) {
  const file = ref<File | null>(null)
  const uploading = ref(false)
  const progress = ref(0)
  const error = ref('')

  function selectFile(selected: File): void {
    file.value = selected
    error.value = ''
  }

  function reset(): void {
    file.value = null
    error.value = ''
  }

  async function upload(): Promise<MediaAsset | null> {
    const selected = file.value
    if (!selected || uploading.value) return null
    if (!acceptedImageTypes.has(selected.type)) {
      error.value = '仅支持 JPEG、PNG 或 WebP 图片。'
      return null
    }

    uploading.value = true
    progress.value = 0
    error.value = ''
    try {
      const asset = await uploadMedia(selected, (value) => {
        progress.value = value
      })
      options.onSuccess(asset)
      progress.value = 100
      return asset
    } catch (caught) {
      options.onError?.(caught)
      error.value = options.formatError(caught)
      return null
    } finally {
      uploading.value = false
    }
  }

  return { file, uploading, progress, error, selectFile, reset, upload }
}
