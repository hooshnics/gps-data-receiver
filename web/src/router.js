import { createRouter, createWebHistory } from 'vue-router'
import MonitorView from './views/MonitorView.vue'
import AnalysisView from './views/AnalysisView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'monitor', component: MonitorView },
    { path: '/analysis', name: 'analysis', component: AnalysisView },
  ],
})

export default router
