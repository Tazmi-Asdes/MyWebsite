import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { routes, sessionGuard } from './router'
import { closeReauth, initSession } from './composables/useSession'

function jsonResponse(status: number, value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}

const session = {
  admin_id: 1,
  username: 'admin',
  csrf_token: 'csrf-token',
  last_seen_at: '2026-09-19T00:00:00Z',
  expires_at: '2026-09-19T08:00:00Z',
  absolute_expires_at: '2026-09-20T00:00:00Z',
}

const article = {
  id: 1,
  public_ulid: null,
  title: '原始标题',
  body_markdown: '# 正文',
  body_html: null,
  toc_json: {},
  preview_text: null,
  renderer_version: null,
  status: 'draft' as const,
  first_published_at: null,
  version: 1,
  created_at: '2026-09-19T00:00:00Z',
  updated_at: '2026-09-19T00:00:00Z',
}

async function renderAt(path: string) {
  const testRouter = createRouter({
    history: createMemoryHistory('/admin/'),
    routes,
  })
  testRouter.beforeEach(sessionGuard)
  await testRouter.push(path)
  await testRouter.isReady()
  const wrapper = mount(App, {
    global: {
      plugins: [testRouter],
    },
  })
  await flushPromises()
  return { testRouter, wrapper }
}

describe('管理端路由页面', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    closeReauth()
  })

  it('渲染登录页并提供正确的表单标签', async () => {
    const { wrapper } = await renderAt('/login')

    expect(wrapper.get('h1').text()).toBe('管理员登录')
    expect(wrapper.get('label[for="username"]').text()).toBe('用户名')
    expect(wrapper.get('label[for="password"]').text()).toBe('密码')
  })

  it('渲染文章管理占位页', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, { items: [], pagination: { page: 1, per_page: 20, total: 0, total_pages: 0 } }))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/articles')

    expect(wrapper.get('h1').text()).toBe('文章管理')
    expect(wrapper.get('[aria-labelledby="articles-placeholder-title"]')).toBeTruthy()
  })

  it('渲染项目管理占位页', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {}))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/projects')

    expect(wrapper.get('h1').text()).toBe('项目管理')
    expect(wrapper.get('[aria-labelledby="projects-placeholder-title"]')).toBeTruthy()
  })

  it('未知管理路由渲染管理端 404', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {}))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/unknown')

    expect(wrapper.get('h1').text()).toBe('页面不存在')
    expect(wrapper.get('.not-found__code').text()).toBe('404')
  })

  it('直接访问受保护路由时先加载 session 并重定向登录', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' })))
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('confirm', vi.fn(() => true))
    await initSession(true)

    const { testRouter, wrapper } = await renderAt('/articles')

    expect(testRouter.currentRoute.value.path).toBe('/login')
    expect(wrapper.get('h1').text()).toBe('管理员登录')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('登录成功后跳转文章管理', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session') && !init?.method) return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
      if (url.endsWith('/session') && init?.method === 'POST') return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { testRouter, wrapper } = await renderAt('/login')

    await wrapper.get('#username').setValue('admin')
    await wrapper.get('#password').setValue('password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(testRouter.currentRoute.value.path).toBe('/articles')
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'POST')).toBe(true)
  })

  it('文章列表请求并渲染标题和状态', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {
        items: [{ ...article, title: '列表中的文章' }],
        pagination: { page: 1, per_page: 20, total: 1, total_pages: 1 },
      }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/articles')
    await flushPromises()

    expect(wrapper.text()).toContain('列表中的文章')
    expect(wrapper.text()).toContain('草稿')
  })

  it('编辑保存使用响应版本并保留已保存表单', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, article))
      if (url.endsWith('/articles/1') && init?.method === 'PUT') return Promise.resolve(jsonResponse(200, { ...article, title: '更新后的标题', version: 2 }))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/articles/1')
    await flushPromises()
    await wrapper.get('#article-title').setValue('更新后的标题')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('#article-title').element).toHaveProperty('value', '更新后的标题')
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === 'PUT' && String(init?.body).includes('"version":1'))).toBe(true)
  })

  it('版本冲突读取服务器当前版本并保留表单输入', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, article))
      if (url.endsWith('/articles/1') && init?.method === 'PUT') {
        return Promise.resolve(jsonResponse(409, {
          status: 409,
          code: 'version_conflict',
          title: '版本冲突',
          type: 'about:blank',
          request_id: 'test-request',
          errors: { version: ['current=8'] },
        }))
      }
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/articles/1')
    await flushPromises()
    await wrapper.get('#article-title').setValue('本地修改标题')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('#article-title').element).toHaveProperty('value', '本地修改标题')
    expect(wrapper.get('[role="alert"]').text()).toContain('服务器当前版本为 8')
  })

  it('新文章发布按钮禁用且不会隐式创建草稿', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, _init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('confirm', vi.fn(() => true))
    await initSession(true)
    const { wrapper } = await renderAt('/articles/new')

    const publishButton = wrapper.get('button.button--primary')
    expect(publishButton.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('请先保存草稿后发布')
    await publishButton.trigger('click')
    await flushPromises()

    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('保存返回 session_expired 时打开重新认证且保留正文', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, article))
      if (url.endsWith('/articles/1') && init?.method === 'PUT') return Promise.resolve(jsonResponse(401, { status: 401, code: 'session_expired' }))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await renderAt('/articles/1')
    await flushPromises()
    await wrapper.get('#article-body').setValue('保留这段未保存正文')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[role="dialog"]').text()).toContain('登录状态已过期')
    expect(wrapper.get('#article-body').element).toHaveProperty('value', '保留这段未保存正文')
  })

  it('退出返回 session_expired 时清除本地会话并跳转登录', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session') && !init?.method) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/session') && init?.method === 'DELETE') return Promise.resolve(jsonResponse(401, { status: 401, code: 'session_expired' }))
      return Promise.resolve(jsonResponse(200, { items: [], pagination: { page: 1, per_page: 20, total: 0, total_pages: 0 } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('confirm', vi.fn(() => true))
    await initSession(true)
    const { testRouter, wrapper } = await renderAt('/articles')
    await wrapper.get('.nav-button').trigger('click')
    await flushPromises()

    expect(testRouter.currentRoute.value.path).toBe('/login')
    expect(wrapper.get('h1').text()).toBe('管理员登录')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })
})
