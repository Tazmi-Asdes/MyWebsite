import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
} from 'vue-router'
import LoginView from '../views/LoginView.vue'
import ArticlesView from '../views/ArticlesView.vue'
import ArticleEditorView from '../views/ArticleEditorView.vue'
import ProjectsView from '../views/ProjectsView.vue'
import ProjectEditorView from '../views/ProjectEditorView.vue'
import NotFoundView from '../views/NotFoundView.vue'

export const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/articles',
  },
  {
    path: '/login',
    name: 'admin-login',
    component: LoginView,
  },
  {
    path: '/articles',
    name: 'admin-articles',
    component: ArticlesView,
  },
  {
    path: '/articles/new',
    name: 'admin-article-new',
    component: ArticleEditorView,
  },
  {
    path: '/articles/:id',
    name: 'admin-article-edit',
    component: ArticleEditorView,
  },
  {
    path: '/projects',
    name: 'admin-projects',
    component: ProjectsView,
  },
  {
    path: '/projects/new',
    name: 'admin-project-new',
    component: ProjectEditorView,
  },
  {
    path: '/projects/:id',
    name: 'admin-project-edit',
    component: ProjectEditorView,
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'admin-not-found',
    component: NotFoundView,
  },
]

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})
