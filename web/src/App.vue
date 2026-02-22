<template>
  <div class="app">
    <header class="header">
      <h1>GPS Data Receiver</h1>
      <p class="subtitle">Vue.js frontend</p>
    </header>
    <main class="main">
      <section class="card">
        <h2>Health</h2>
        <p v-if="health">{{ health.status }}</p>
        <p v-else class="muted">Checking…</p>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'

const health = ref(null)

onMounted(async () => {
  try {
    const res = await fetch('/health')
    const data = await res.json()
    health.value = data
  } catch (e) {
    health.value = { status: 'error', error: e.message }
  }
})
</script>

<style>
:root {
  --bg: #0f1419;
  --surface: #1a2332;
  --text: #e6edf3;
  --muted: #8b949e;
  --accent: #58a6ff;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
  font-family: system-ui, -apple-system, sans-serif;
  background: var(--bg);
  color: var(--text);
  min-height: 100vh;
}

.app {
  max-width: 720px;
  margin: 0 auto;
  padding: 2rem;
}

.header {
  margin-bottom: 2rem;
}

.header h1 {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 600;
}

.subtitle {
  margin: 0.25rem 0 0;
  color: var(--muted);
  font-size: 0.95rem;
}

.main {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.card {
  background: var(--surface);
  border-radius: 8px;
  padding: 1.25rem;
  border: 1px solid rgba(255, 255, 255, 0.06);
}

.card h2 {
  margin: 0 0 0.5rem;
  font-size: 1rem;
  font-weight: 600;
  color: var(--muted);
}

.card p {
  margin: 0;
  font-size: 1rem;
}

.muted {
  color: var(--muted);
}
</style>
