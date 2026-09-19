import type { components } from './schema'

export type ProblemDetails = components['schemas']['ProblemDetails']

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
