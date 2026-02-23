<template>
  <div class="min-h-screen bg-slate-50 text-slate-900">
    <header class="border-b border-slate-200 bg-white shadow-sm">
      <div class="mx-auto max-w-[1600px] px-4 py-4 sm:px-6 lg:px-8">
        <div class="flex items-center justify-between">
          <div>
            <h1 class="text-2xl font-bold tracking-tight text-slate-800">
              GPS Data Receiver
            </h1>
            <p class="mt-1 text-sm text-slate-500">
              Real-time packet stream · received and delivered
            </p>
          </div>
          <div class="flex items-center gap-2">
            <span
              class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium"
              :class="
                connected
                  ? 'bg-emerald-100 text-emerald-800'
                  : 'bg-amber-100 text-amber-800'
             "
            >
              <span
                class="h-1.5 w-1.5 rounded-full"
                :class="connected ? 'bg-emerald-500' : 'bg-amber-500'"
              />
              {{ connected ? 'Live' : 'Disconnected' }}
            </span>
          </div>
        </div>
      </div>
    </header>
    <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8">
      <div v-if="error" class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
        {{ error }}
      </div>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <!-- Left: Received packets -->
        <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              Received
              <span class="ml-2 font-normal text-slate-500">({{ packets.length }} shown)</span>
            </h2>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <ul v-if="packets.length" class="divide-y divide-slate-200">
              <li
                v-for="(pkt, index) in packets"
                :key="pkt.message_id + '-' + index"
                class="px-4 py-3 sm:px-6 hover:bg-slate-50/50"
              >
                <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span class="font-mono text-sm font-medium text-slate-700">
                    {{ pkt.message_id }}
                  </span>
                  <span class="text-xs text-slate-500">
                    {{ formatTime(pkt.received_at) }}
                  </span>
                  <span class="text-xs text-slate-400">
                    {{ pkt.payload_size }} bytes
                  </span>
                </div>
                <pre
                  class="mt-2 overflow-x-auto rounded bg-slate-100 px-2 py-1.5 font-mono text-xs text-slate-700"
                >{{ payloadPreview(pkt.payload) }}</pre>
              </li>
            </ul>
            <div
              v-else
              class="px-4 py-12 text-center text-sm text-slate-500"
            >
              No packets yet
            </div>
          </div>
        </div>

        <!-- Right: Delivered to destination servers -->
        <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              Delivered to destinations
              <span class="ml-2 font-normal text-slate-500">({{ deliveredPackets.length }} shown)</span>
            </h2>
            <button
              type="button"
              class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 shadow-sm hover:bg-slate-50 disabled:opacity-50"
              :disabled="!packets.length && !deliveredPackets.length"
              @click="clearBothLists"
            >
              Clear both lists
            </button>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <ul v-if="deliveredPackets.length" class="divide-y divide-slate-200">
              <li
                v-for="(pkt, index) in deliveredPackets"
                :key="pkt.delivered_at + pkt.target_server + '-' + index"
                class="px-4 py-3 sm:px-6 hover:bg-slate-50/50"
              >
                <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span class="text-xs font-medium text-emerald-700 truncate max-w-[12rem]" :title="pkt.target_server">
                    {{ pkt.target_server }}
                  </span>
                  <span class="text-xs text-slate-500">
                    {{ formatTime(pkt.delivered_at) }}
                  </span>
                  <span class="text-xs text-slate-400">
                    {{ pkt.payload_size }} bytes
                  </span>
                </div>
                <pre
                  class="mt-2 overflow-x-auto rounded bg-slate-100 px-2 py-1.5 font-mono text-xs text-slate-700"
                >{{ payloadPreview(pkt.payload) }}</pre>
              </li>
            </ul>
            <div
              v-else
              class="px-4 py-12 text-center text-sm text-slate-500"
            >
              No deliveries yet
            </div>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { useGpsPackets } from './composables/useGpsPackets'

const { packets, deliveredPackets, connected, error, clearPackets, clearDeliveredPackets } = useGpsPackets()

function clearBothLists() {
  clearPackets()
  clearDeliveredPackets()
}

function formatTime(iso) {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toLocaleString()
  } catch {
    return iso
  }
}

function payloadPreview(payload, maxLen = 320) {
  if (typeof payload !== 'string') return ''
  if (payload.length <= maxLen) return payload
  return payload.slice(0, maxLen) + '…'
}
</script>
