import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it } from 'vitest'
import App from './App.vue'
import { routes } from './router'

async function renderAt(path: string) {
  const testRouter = createRouter({
    history: createMemoryHistory('/admin/'),
    routes,
  })
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
  it('渲染登录页并提供正确的表单标签', async () => {
    const { wrapper } = await renderAt('/login')

    expect(wrapper.get('h1').text()).toBe('管理员登录')
    expect(wrapper.get('label[for="username"]').text()).toBe('用户名')
    expect(wrapper.get('label[for="password"]').text()).toBe('密码')
  })

  it('渲染文章管理占位页', async () => {
    const { wrapper } = await renderAt('/articles')

    expect(wrapper.get('h1').text()).toBe('文章管理')
    expect(wrapper.get('[aria-labelledby="articles-placeholder-title"]')).toBeTruthy()
  })

  it('渲染项目管理占位页', async () => {
    const { wrapper } = await renderAt('/projects')

    expect(wrapper.get('h1').text()).toBe('项目管理')
    expect(wrapper.get('[aria-labelledby="projects-placeholder-title"]')).toBeTruthy()
  })

  it('未知管理路由渲染管理端 404', async () => {
    const { wrapper } = await renderAt('/unknown')

    expect(wrapper.get('h1').text()).toBe('页面不存在')
    expect(wrapper.get('.not-found__code').text()).toBe('404')
  })
})
