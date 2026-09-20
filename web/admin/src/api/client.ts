import type { components } from './schema'

export type ProblemDetails = components['schemas']['ProblemDetails']
export type MediaAsset = components['schemas']['MediaAsset']

export class ApiError extends Error {
  readonly status: number
  readonly problem: ProblemDetails | null

  constructor(status: number, problem: ProblemDetails | null, message?: string) {
    super(message ?? problem?.title ?? `请求失败（${status}）`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
  }
}

let csrfToken = ''

export function setCsrfToken(token: string | null | undefined): void {
  csrfToken = token ?? ''
}

export function getCsrfToken(): string {
  return csrfToken
}

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
  csrf?: boolean
}

const API_BASE = '/api/v1'

/**
 * Upload an image without manually setting multipart Content-Type. The browser
 * adds the boundary when a FormData body is passed to XMLHttpRequest.
 */
export function uploadMedia(file: File, onProgress?: (progress: number) => void): Promise<MediaAsset> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const formData = new FormData()
    formData.append('file', file)

    const fail = (error: unknown): void => reject(error)
    const parsePayload = (): unknown => {
      const text = xhr.responseText ?? ''
      if (!text) return null
      try {
        return JSON.parse(text) as unknown
      } catch {
        return text
      }
    }

    xhr.open('POST', `${API_BASE}/media`)
    xhr.withCredentials = true
    xhr.setRequestHeader('Accept', 'application/json')
    if (csrfToken) xhr.setRequestHeader('X-CSRF-Token', csrfToken)
    onProgress?.(0)

    xhr.upload.addEventListener('progress', (event) => {
      if (!event.lengthComputable) return
      const progress = Math.max(0, Math.min(100, Math.round((event.loaded / event.total) * 100)))
      onProgress?.(progress)
    })
    xhr.addEventListener('error', () => fail(new Error('上传失败，请稍后重试。')))
    xhr.addEventListener('abort', () => fail(new Error('上传已取消。')))
    xhr.addEventListener('load', () => {
      const payload = parsePayload()
      if (xhr.status < 200 || xhr.status >= 300) {
        const problem = isProblemDetails(payload) ? payload : null
        fail(new ApiError(xhr.status, problem, problem?.title ?? (typeof payload === 'string' ? payload : undefined)))
        return
      }
      onProgress?.(100)
      resolve(payload as MediaAsset)
    })

    try {
      xhr.send(formData)
    } catch (error) {
      fail(error)
    }
  })
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, csrf, headers: initialHeaders, ...init } = options
  const headers = new Headers(initialHeaders)
  headers.set('Accept', 'application/json')

  let requestBody: BodyInit | undefined
  if (body !== undefined) {
    headers.set('Content-Type', 'application/json')
    requestBody = JSON.stringify(body)
  }
  if (csrf && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    body: requestBody,
    credentials: 'include',
    headers,
  })

  if (response.status === 204) {
    return undefined as T
  }

  const contentType = response.headers.get('content-type') ?? ''
  const payload: unknown = contentType.includes('json') ? await response.json() : await response.text()
  if (!response.ok) {
    const problem = isProblemDetails(payload) ? payload : null
    throw new ApiError(response.status, problem, problem?.title ?? (typeof payload === 'string' ? payload : undefined))
  }

  return payload as T
}

function isProblemDetails(value: unknown): value is ProblemDetails {
  if (!value || typeof value !== 'object') return false
  const candidate = value as Partial<ProblemDetails>
  return typeof candidate.status === 'number' && typeof candidate.code === 'string'
}
