import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { routes, sessionGuard } from './router'
import { closeReauth, initSession, openReauth } from './composables/useSession'

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

const projectOne = {
  id: 1,
  name: '第一个项目',
  github_url: 'https://github.com/example/one',
  image_asset_id: null,
  image_preview_url: null,
  status: 'public' as const,
  sort_order: 0,
  version: 1,
  created_at: '2026-09-19T00:00:00Z',
  updated_at: '2026-09-19T00:00:00Z',
}

const projectTwo = {
  ...projectOne,
  id: 2,
  name: '第二个项目',
  github_url: 'https://github.com/example/two',
  sort_order: 1,
}

const hiddenProject = {
  ...projectOne,
  id: 3,
  name: '隐藏项目',
  github_url: null,
  status: 'hidden' as const,
  sort_order: null,
}

function projectGroups() {
  return { public: [projectOne, projectTwo], hidden: [hiddenProject], order_version: 4 }
}

async function renderAt(path: string, attachToBody = false) {
  const testRouter = createRouter({
    history: createMemoryHistory('/admin/'),
    routes,
  })
  testRouter.beforeEach(sessionGuard)
  await testRouter.push(path)
  await testRouter.isReady()
  const wrapper = mount(App, {
    attachTo: attachToBody ? document.body : undefined,
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

  it('渲染 V2 登录结构并提供完整的键盘入口和表单语义', async () => {
    const { wrapper } = await renderAt('/login')

    expect(wrapper.get('.skip-link').text()).toBe('跳到登录表单')
    expect(wrapper.get('.skip-link').attributes('href')).toBe('#login-main')
    expect(wrapper.find('main#login-main').exists()).toBe(true)
    expect(wrapper.get('.login-panel__brand').text()).toBe('个人技术网站')
    expect(wrapper.get('h1').text()).toBe('管理员登录')
    expect(wrapper.get('.login-intro').text()).toBe('登录后管理文章与公开项目。')
    expect(wrapper.get('form.form-grid').attributes('novalidate')).toBeDefined()
    expect(wrapper.get('label[for="username"]').text()).toBe('用户名')
    expect(wrapper.get('label[for="password"]').text()).toBe('密码')

    const usernameInput = wrapper.get('#username')
    const passwordInput = wrapper.get('#password')
    expect(usernameInput.classes()).toContain('input')
    expect(usernameInput.attributes('autocomplete')).toBe('username')
    expect(usernameInput.attributes('required')).toBeDefined()
    expect(usernameInput.attributes('autofocus')).toBeDefined()
    expect(passwordInput.classes()).toContain('input')
    expect(passwordInput.attributes('autocomplete')).toBe('current-password')
    expect(passwordInput.attributes('required')).toBeDefined()

    const submitButton = wrapper.get('form.form-grid button[type="submit"]')
    expect(submitButton.classes()).toEqual(expect.arrayContaining(['button', 'button--primary']))
    expect(submitButton.text()).toBe('登录')
    expect(submitButton.attributes('disabled')).toBeUndefined()
  })

  it('渲染文章管理真实空状态', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, { items: [], pagination: { page: 1, per_page: 20, total: 0, total_pages: 0 } }))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/articles')

    expect(wrapper.get('h1').text()).toBe('文章管理')
    expect(wrapper.get('#articles-empty-title').text()).toBe('还没有文章')
    expect(wrapper.get('.empty-state').text()).toContain('先创建一篇草稿，再决定何时公开发布。')
    expect(wrapper.get('.empty-state a').text()).toBe('新建文章')
    expect(wrapper.get('.empty-state a').attributes('href')).toBe('/admin/articles/new')
    expect(wrapper.find('[aria-labelledby="articles-placeholder-title"]').exists()).toBe(false)
    const main = wrapper.get('main#main-content')
    expect(main.classes()).not.toContain('admin-content')
    expect(wrapper.find('main#main-content > .admin-page-header').exists()).toBe(true)
    expect(wrapper.find('main#main-content > .admin-content').exists()).toBe(true)
  })

  it('后台共用外壳支持移动导航开关、键盘关闭和账户入口', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, { items: [], pagination: { page: 1, per_page: 20, total: 0, total_pages: 0 } }))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/articles', true)

    const aside = wrapper.get('#admin-navigation')
    const menuButton = wrapper.get('button.menu-button')
    expect(menuButton.attributes('aria-controls')).toBe('admin-navigation')
    expect(menuButton.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('.admin-nav--bottom a[href="/"]').attributes()).toEqual(expect.objectContaining({
      target: '_blank',
      rel: 'noreferrer',
    }))
    expect(wrapper.get('.admin-nav--bottom .nav-button').text()).toContain('退出')

    await menuButton.trigger('click')
    expect(menuButton.attributes('aria-expanded')).toBe('true')
    expect(aside.classes()).toContain('is-open')

    await menuButton.trigger('keydown', { key: 'Escape' })
    expect(menuButton.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(menuButton.element)

    await menuButton.trigger('click')
    await wrapper.get('.admin-nav:not(.admin-nav--bottom) a[href="/admin/projects"]').trigger('click')
    await flushPromises()
    expect(menuButton.attributes('aria-expanded')).toBe('false')
    expect(aside.classes()).not.toContain('is-open')

    wrapper.unmount()
  })

  it('渲染项目管理公开组与隐藏组', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {}))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/projects')

    expect(wrapper.get('h1').text()).toBe('项目管理')
    expect(wrapper.get('[aria-labelledby="public-projects-title"]')).toBeTruthy()
    expect(wrapper.get('[aria-labelledby="hidden-projects-title"]')).toBeTruthy()
    const main = wrapper.get('main#main-content')
    expect(main.classes()).not.toContain('admin-content')
    expect(wrapper.find('main#main-content > .admin-page-header').exists()).toBe(true)
    expect(wrapper.find('main#main-content > .admin-content').exists()).toBe(true)
  })

  it('编辑页使用统一后台页头与内容区', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {}))
    }))
    await initSession(true)
    const { wrapper } = await renderAt('/articles/new')

    const main = wrapper.get('main#main-content')
    expect(main.classes()).not.toContain('admin-content')
    expect(wrapper.find('main#main-content > .admin-page-header').exists()).toBe(true)
    expect(wrapper.find('main#main-content > .admin-content').exists()).toBe(true)
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

  it('登录返回 validation_failed 时显示校验错误并保留输入', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session') && !init?.method) {
        return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
      }
      if (url.endsWith('/session') && init?.method === 'POST') {
        return Promise.resolve(jsonResponse(422, {
          status: 422,
          code: 'validation_failed',
          type: 'https://example.test/problems/validation_failed',
          title: '请求参数无效',
          request_id: 'request-validation-failed',
          errors: {},
        }))
      }
      return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { testRouter, wrapper } = await renderAt('/login')

    await wrapper.get('#username').setValue('  admin  ')
    await wrapper.get('#password').setValue('bad-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(testRouter.currentRoute.value.path).toBe('/login')
    expect(wrapper.get('[role="alert"]').text()).toBe('请输入有效的用户名和密码。')
    expect(wrapper.get('#username').element).toHaveProperty('value', '  admin  ')
    expect(wrapper.get('#password').element).toHaveProperty('value', 'bad-password')
  })

  it('普通认证失败时显示认证错误并保留输入', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session') && !init?.method) {
        return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
      }
      if (url.endsWith('/session') && init?.method === 'POST') {
        return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
      }
      return Promise.resolve(jsonResponse(401, { status: 401, code: 'authentication_required' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { testRouter, wrapper } = await renderAt('/login')

    await wrapper.get('#username').setValue('admin')
    await wrapper.get('#password').setValue('wrong-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(testRouter.currentRoute.value.path).toBe('/login')
    expect(wrapper.get('[role="alert"]').text()).toBe('用户名或密码不正确，请检查后重试。')
    expect(wrapper.get('#username').element).toHaveProperty('value', 'admin')
    expect(wrapper.get('#password').element).toHaveProperty('value', 'wrong-password')
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
    expect(wrapper.findAll('.data-table tbody tr')).toHaveLength(1)
    expect(wrapper.findAll('.mobile-records .record-card')).toHaveLength(1)
    expect(wrapper.get('.data-table .status.status--outline').text()).toBe('草稿')
    expect(wrapper.get('.mobile-records .status.status--outline').text()).toBe('草稿')
    expect(wrapper.get('.data-table a[href="/admin/articles/1"]').text()).toBe('编辑')
    expect(wrapper.get('.mobile-records a[href="/admin/articles/1"]').text()).toBe('编辑文章')
    expect(wrapper.find('.empty-state').exists()).toBe(false)
  })

  it('提交搜索和状态筛选会从第一页请求对应参数', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      return Promise.resolve(jsonResponse(200, {
        items: [{ ...article, title: '筛选结果' }],
        pagination: { page: 1, per_page: 20, total: 1, total_pages: 1 },
      }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/articles')
    await flushPromises()
    fetchMock.mockClear()

    await wrapper.get('#article-search').setValue('云原生')
    await wrapper.get('form.admin-toolbar').trigger('submit')
    await flushPromises()

    const searchRequest = fetchMock.mock.calls
      .map(([input]) => new URL(String(input), 'http://localhost'))
      .find((url) => url.pathname.endsWith('/articles'))
    expect(searchRequest?.searchParams.get('q')).toBe('云原生')
    expect(searchRequest?.searchParams.get('page')).toBe('1')

    await wrapper.get('#article-status').setValue('draft')
    await flushPromises()

    const statusRequest = fetchMock.mock.calls
      .map(([input]) => new URL(String(input), 'http://localhost'))
      .find((url) => url.pathname.endsWith('/articles') && url.searchParams.get('status') === 'draft')
    expect(statusRequest?.searchParams.get('status')).toBe('draft')
    expect(statusRequest?.searchParams.get('page')).toBe('1')
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

    expect(wrapper.find('main#main-content > .admin-page-header').exists()).toBe(true)
    expect(wrapper.find('main#main-content > .admin-content').exists()).toBe(true)
    expect(wrapper.find('form.editor-form > .editor-actions').exists()).toBe(true)
    expect(wrapper.find('form.editor-form > .form-grid').exists()).toBe(true)
    expect(wrapper.find('form.editor-form > .form-grid #article-title').exists()).toBe(true)
    expect(wrapper.find('form.editor-form > .form-grid #article-body').exists()).toBe(true)
    expect(wrapper.find('form.editor-form > .form-grid .field.upload.media-upload-field').exists()).toBe(true)
    wrapper.find('form.editor-form > .editor-actions').findAll('a, button').forEach((control) => {
      expect(control.classes()).toContain('button--small')
    })
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

  it('重新认证对话框打开后聚焦密码，Escape 取消并恢复触发按钮', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, article))
      if (url.endsWith('/articles/1') && init?.method === 'PUT') return Promise.resolve(jsonResponse(401, { status: 401, code: 'session_expired' }))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await renderAt('/articles/1', true)
    await flushPromises()
    await wrapper.get('#article-body').setValue('触发重新认证')
    const trigger = wrapper.get('button[type="submit"]')
    ;(trigger.element as HTMLElement).focus()
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(document.activeElement).toBe(wrapper.get('#session-password').element)
    await wrapper.get('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await flushPromises()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    openReauth()
    await flushPromises()
    expect(wrapper.get('#session-password').element).toHaveProperty('value', '')
    wrapper.unmount()
  })

  it('撤回对话框聚焦取消并在 Tab 边界循环，Escape 恢复触发按钮', async () => {
    const publishedArticle = { ...article, status: 'published' as const, public_ulid: '01ARZ3NDEKTSV4RRFFQ69G5FAV' }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, publishedArticle))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/articles/1', true)
    await flushPromises()
    expect(wrapper.find('.heading-row .status').exists()).toBe(false)
    expect(wrapper.find('.heading-links .status').exists()).toBe(true)
    const headingLinks = wrapper.findAll('.heading-links a.text-link').map((link) => link.text())
    expect(headingLinks).toContain('查看公开文章')
    expect(headingLinks).toContain('返回文章管理')
    const trigger = wrapper.get('button.button--danger')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await flushPromises()

    const dialog = wrapper.get('[role="dialog"]')
    const cancel = dialog.get('button.button--secondary')
    const confirm = dialog.get('button.button--danger')
    expect(document.activeElement).toBe(cancel.element)
    ;(confirm.element as HTMLElement).focus()
    await dialog.trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(cancel.element)
    ;(cancel.element as HTMLElement).focus()
    await dialog.trigger('keydown', { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(confirm.element)
    await dialog.trigger('keydown', { key: 'Escape' })
    await flushPromises()

    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('隐藏对话框打开后聚焦取消按钮', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/projects/1') && !init?.method) return Promise.resolve(jsonResponse(200, projectOne))
      if (url.endsWith('/projects') && !init?.method) return Promise.resolve(jsonResponse(200, projectGroups()))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/projects/1', true)
    await flushPromises()
    const trigger = wrapper.get('button.button--danger')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await flushPromises()

    expect(document.activeElement).toBe(wrapper.get('[role="dialog"] button.button--secondary').element)
    wrapper.unmount()
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

  it('项目分组按响应顺序渲染，显式上移后只在保存顺序时提交', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/projects/order') && init?.method === 'PUT') return Promise.resolve(jsonResponse(200, { order_version: 5 }))
      if (url.endsWith('/projects')) return Promise.resolve(jsonResponse(200, projectGroups()))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/projects')
    await flushPromises()

    const names = () => wrapper.findAll('.project-list:not(.project-list--hidden) .project-list__name').map((item) => item.text())
    expect(names()).toEqual(['第一个项目', '第二个项目'])
    const moveUp = wrapper.findAll('button').find((button) => button.text() === '上移' && button.attributes('disabled') === undefined)
    expect(moveUp).toBeDefined()
    await moveUp!.trigger('click')
    expect(names()).toEqual(['第二个项目', '第一个项目'])
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'PUT')).toHaveLength(0)

    const save = wrapper.findAll('button').find((button) => button.text() === '保存顺序')
    expect(save).toBeDefined()
    await save!.trigger('click')
    await flushPromises()
    const orderCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(orderCall?.[1]?.body).toBe(JSON.stringify({ public_ids: [2, 1], order_version: 4 }))
    expect(wrapper.text()).toContain('公开项目顺序已保存')
  })

  it('项目顺序冲突时保留本地顺序并提示刷新', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/projects/order') && init?.method === 'PUT') {
        return Promise.resolve(jsonResponse(409, {
          type: 'about:blank',
          title: '版本冲突',
          status: 409,
          code: 'version_conflict',
          request_id: 'order-conflict',
          errors: { order_version: ['current=9'] },
        }))
      }
      if (url.endsWith('/projects')) return Promise.resolve(jsonResponse(200, projectGroups()))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/projects')
    await flushPromises()
    const moveUp = wrapper.findAll('button').find((button) => button.text() === '上移' && button.attributes('disabled') === undefined)
    await moveUp!.trigger('click')
    const save = wrapper.findAll('button').find((button) => button.text() === '保存顺序')
    await save!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('.project-list:not(.project-list--hidden) .project-list__name').map((item) => item.text())).toEqual(['第二个项目', '第一个项目'])
    expect(wrapper.get('[role="alert"]').text()).toContain('当前顺序已保留')
  })

  it('新建项目保存后使用最新排序版本立即公开', async () => {
    const created = { ...hiddenProject, id: 9, name: '新项目', github_url: null, image_asset_id: null, image_preview_url: null, version: 1 }
    const updated = { ...created, github_url: 'https://github.com/example/new', image_asset_id: '01JMEDIA', image_preview_url: '/api/v1/media/01JMEDIA', version: 2 }
    const published = { ...updated, status: 'public' as const, sort_order: 0, version: 3 }
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/projects') && init?.method === 'POST') return Promise.resolve(jsonResponse(201, created))
      if (url.endsWith('/projects/9') && init?.method === 'PUT') return Promise.resolve(jsonResponse(200, updated))
      if (url.endsWith('/projects/9/publish') && init?.method === 'POST') return Promise.resolve(jsonResponse(200, published))
      if (url.endsWith('/projects')) return Promise.resolve(jsonResponse(200, { public: [], hidden: [updated], order_version: 7 }))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    class UploadSuccessXHR {
      upload = { addEventListener: (_type: string, callback: (event: { lengthComputable: boolean; loaded: number; total: number }) => void) => callback({ lengthComputable: true, loaded: 1, total: 1 }) }
      status = 201
      responseText = JSON.stringify({ id: '01JMEDIA', preview_url: '/api/v1/media/01JMEDIA', markdown_reference: '![请填写图片说明](/media/01JMEDIA)' })
      private listeners = new Map<string, (event: Event) => void>()
      open(): void {}
      setRequestHeader(): void {}
      addEventListener(type: string, callback: (event: Event) => void): void { this.listeners.set(type, callback) }
      send(): void { this.listeners.get('load')?.(new Event('load')) }
    }
    vi.stubGlobal('XMLHttpRequest', UploadSuccessXHR)
    await initSession(true)
    const { testRouter, wrapper } = await renderAt('/projects/new')
    await flushPromises()
    await wrapper.get('#project-name').setValue('新项目')
    await wrapper.get('#project-github').setValue('https://github.com/example/new')
    const projectFile = new File(['image'], 'project.png', { type: 'image/png' })
    const projectImageInput = wrapper.get('#project-image').element as HTMLInputElement
    Object.defineProperty(projectImageInput, 'files', { configurable: true, value: [projectFile] })
    await wrapper.get('#project-image').trigger('change')
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(testRouter.currentRoute.value.path).toBe('/projects/9')
    const createCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    const updateCall = fetchMock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(createCall?.[1]?.body).toBe(JSON.stringify({ name: '新项目' }))
    expect(updateCall?.[1]?.body).toBe(JSON.stringify({ name: '新项目', github_url: 'https://github.com/example/new', image_asset_id: '01JMEDIA', version: 1 }))

    await wrapper.get('button.button--primary').trigger('click')
    await flushPromises()

    const publishCall = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/projects/9/publish') && init?.method === 'POST')
    expect(publishCall?.[1]?.body).toBe(JSON.stringify({ name: '新项目', github_url: 'https://github.com/example/new', image_asset_id: '01JMEDIA', version: 2, order_version: 7 }))
    expect(wrapper.get('.status').text()).toBe('公开')
  })

  it('项目公开和隐藏使用完整请求体，并在状态变更后同步排序版本', async () => {
    const hidden = { ...hiddenProject, id: 5, name: '待公开项目', github_url: 'https://github.com/example/five', version: 1 }
    type ProjectState = Omit<typeof hidden, 'status' | 'sort_order'> & { status: 'hidden' | 'public'; sort_order: number | null }
    const published: ProjectState = { ...hidden, status: 'public', version: 2, sort_order: 0 }
    const rehiden: ProjectState = { ...published, status: 'hidden', version: 3, sort_order: null }
    let current: ProjectState = hidden
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/projects/5/publish') && init?.method === 'POST') {
        current = published
        return Promise.resolve(jsonResponse(200, published))
      }
      if (url.endsWith('/projects/5/hide') && init?.method === 'POST') {
        current = rehiden
        return Promise.resolve(new Response(null, { status: 204 }))
      }
      if (url.endsWith('/projects/5')) return Promise.resolve(jsonResponse(200, current))
      if (url.endsWith('/projects')) return Promise.resolve(jsonResponse(200, { public: current.status === 'public' ? [current] : [], hidden: current.status === 'hidden' ? [current] : [], order_version: current.version === 1 ? 4 : current.status === 'public' ? 8 : 9 }))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    await initSession(true)
    const { wrapper } = await renderAt('/projects/5')
    await flushPromises()
    await wrapper.get('button.button--primary').trigger('click')
    await flushPromises()
    const publishCall = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/projects/5/publish') && init?.method === 'POST')
    expect(publishCall?.[1]?.body).toBe(JSON.stringify({ name: '待公开项目', github_url: 'https://github.com/example/five', image_asset_id: null, version: 1, order_version: 4 }))

    await wrapper.get('button.button--danger').trigger('click')
    await wrapper.get('[role="dialog"] .button--danger').trigger('click')
    await flushPromises()
    const hideCall = fetchMock.mock.calls.find(([input, init]) => String(input).endsWith('/projects/5/hide') && init?.method === 'POST')
    expect(hideCall?.[1]?.body).toBe(JSON.stringify({ version: 2, order_version: 8 }))
    expect(wrapper.text()).toContain('项目已隐藏')
  })

  it('文章图片上传失败保留文件，重试后把引用插入正文', async () => {
    const responses = [
      { status: 500, body: { type: 'about:blank', title: '上传失败', status: 500, code: 'save_failed', request_id: 'upload-failed', errors: {} } },
      { status: 201, body: { id: '01JMEDIA', preview_url: '/api/v1/media/01JMEDIA', markdown_reference: '![请填写图片说明](/media/01JMEDIA)' } },
    ]
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/session')) return Promise.resolve(jsonResponse(200, session))
      if (url.endsWith('/articles/1') && !init?.method) return Promise.resolve(jsonResponse(200, article))
      return Promise.resolve(jsonResponse(404, { status: 404, code: 'not_found' }))
    })
    vi.stubGlobal('fetch', fetchMock)
    class UploadSequenceXHR {
      upload = { addEventListener: (_type: string, callback: (event: { lengthComputable: boolean; loaded: number; total: number }) => void) => callback({ lengthComputable: true, loaded: 1, total: 1 }) }
      status: number
      responseText: string
      private listeners = new Map<string, (event: Event) => void>()
      constructor() {
        const response = responses.shift()!
        this.status = response.status
        this.responseText = JSON.stringify(response.body)
      }
      open(): void {}
      setRequestHeader(): void {}
      addEventListener(type: string, callback: (event: Event) => void): void { this.listeners.set(type, callback) }
      send(): void { this.listeners.get('load')?.(new Event('load')) }
    }
    vi.stubGlobal('XMLHttpRequest', UploadSequenceXHR)
    await initSession(true)
    const { wrapper } = await renderAt('/articles/1')
    await flushPromises()
    await wrapper.get('#article-body').setValue('正文')
    const textarea = wrapper.get('#article-body').element as HTMLTextAreaElement
    textarea.setSelectionRange(2, 2)
    const file = new File(['image'], 'article.png', { type: 'image/png' })
    const articleImageInput = wrapper.get('#article-image').element as HTMLInputElement
    Object.defineProperty(articleImageInput, 'files', { configurable: true, value: [file] })
    await wrapper.get('#article-image').trigger('change')
    await flushPromises()
    expect(wrapper.get('#article-body').element).toHaveProperty('value', '正文')
    expect(wrapper.get('[role="alert"]').text()).toContain('上传失败')
    await wrapper.findAll('button').find((button) => button.text() === '重试上传')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('#article-body').element).toHaveProperty('value', '正文\n![请填写图片说明](/media/01JMEDIA)')
  })
})
