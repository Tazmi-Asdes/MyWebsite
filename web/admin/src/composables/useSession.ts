import { computed, ref } from 'vue'
import type { components } from '../api/schema'
import { ApiError, request, setCsrfToken } from '../api/client'

export type Session = components['schemas']['Session']

const session = ref<Session | null>(null)
const ready = ref(false)
export const reauthOpen = ref(false)
export const reauthError = ref('')
let initPromise: Promise<Session | null> | null = null

export function useSession() {
  return {
    session,
    ready,
    authenticated: computed(() => session.value !== null),
    reauthOpen,
    reauthError,
    initSession,
    login,
    logout,
    reauthenticate,
    ensureLoaded,
    openReauth,
    closeReauth,
    handleApiError,
  }
}

export function clearSession(): void {
  session.value = null
  setCsrfToken(null)
}

export function isSessionTerminationError(error: unknown): boolean {
  return error instanceof ApiError && (
    error.problem?.code === 'authentication_required' ||
    error.problem?.code === 'session_expired'
  )
}

export function ensureLoaded(): Promise<Session | null> {
  if (ready.value) return Promise.resolve(session.value)
  if (initPromise) return initPromise

  initPromise = request<Session>('/session')
    .then((value) => {
      session.value = value
      setCsrfToken(value.csrf_token)
      return value
    })
    .catch((error: unknown) => {
      if (error instanceof ApiError && error.status === 401) {
        clearSession()
        return null
      }
      clearSession()
      return null
    })
    .finally(() => {
      ready.value = true
      initPromise = null
    })
  return initPromise
}

export async function initSession(force = false): Promise<Session | null> {
  if (force) {
    ready.value = false
  }
  return ensureLoaded()
}

export async function login(username: string, password: string): Promise<Session> {
  const value = await request<Session>('/session', {
    method: 'POST',
    body: { username, password },
  })
  session.value = value
  setCsrfToken(value.csrf_token)
  ready.value = true
  return value
}

export async function logout(): Promise<void> {
  await request<void>('/session', { method: 'DELETE', csrf: true })
  clearSession()
}

export async function reauthenticate(password: string): Promise<Session> {
  const value = await request<Session>('/session/reauth', {
    method: 'POST',
    body: { password },
    csrf: true,
  })
  session.value = value
  setCsrfToken(value.csrf_token)
  reauthError.value = ''
  reauthOpen.value = false
  return value
}

export function openReauth(): void {
  reauthError.value = ''
  reauthOpen.value = true
}

export function closeReauth(): void {
  reauthOpen.value = false
  reauthError.value = ''
}

export function handleApiError(error: unknown): void {
  if (error instanceof ApiError && error.problem?.code === 'session_expired') {
    openReauth()
    return
  }
  if (error instanceof ApiError && error.problem?.code === 'authentication_required') {
    clearSession()
  }
}
