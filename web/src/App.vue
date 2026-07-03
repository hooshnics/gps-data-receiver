<template>
  <div class="min-h-screen bg-slate-50 text-slate-900" dir="ltr">
    <header class="border-b border-slate-200 bg-white shadow-sm">
      <div class="mx-auto flex max-w-[1600px] flex-wrap items-center justify-between gap-4 px-4 py-4 sm:px-6 lg:px-8">
        <h1 class="text-lg font-semibold text-slate-800">GPS Data Receiver</h1>
        <nav class="flex gap-1 rounded-lg border border-slate-200 bg-slate-100 p-1">
          <RouterLink
            to="/"
            class="rounded-md px-4 py-2 text-sm font-medium transition"
            :class="route.path === '/' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'"
          >
            Monitor
          </RouterLink>
          <RouterLink
            to="/analysis"
            class="rounded-md px-4 py-2 text-sm font-medium transition"
            :class="route.path === '/analysis' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'"
          >
            Delivered
          </RouterLink>
          <RouterLink
            to="/failed"
            class="rounded-md px-4 py-2 text-sm font-medium transition"
            :class="route.path === '/failed' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'"
          >
            Failed
          </RouterLink>
        </nav>
      </div>
    </header>
    <RouterView />
  </div>
</template>

<script setup>
import { useRoute } from 'vue-router'

const route = useRoute()
</script>
