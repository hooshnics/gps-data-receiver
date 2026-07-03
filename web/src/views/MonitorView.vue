<template>
  <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8">
      <div v-if="error" class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
        {{ error }}
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              Data Delivered to Servers
              <span class="ml-2 font-normal text-slate-500">({{ displayedDeliveredRows.length }} of 15 records)</span>
            </h2>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <div v-if="displayedDeliveredRows.length" class="overflow-x-auto">
              <table class="min-w-full divide-y divide-slate-200 text-sm">
                <thead class="bg-slate-50 sticky top-0 z-10">
                  <tr>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">IMEI</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Coordinates</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Speed</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Status</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Direction</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Date</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Time</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">Delivery</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100 bg-white">
                  <tr v-for="(row, index) in displayedDeliveredRows" :key="row.key || index" class="hover:bg-slate-50/60">
                    <td class="whitespace-nowrap px-4 py-2 text-center font-mono text-xs text-slate-800">
                      {{ row.imei }}
                    </td>
                    <td class="px-4 py-2 text-xs text-slate-800">
                      {{ row.coordinate }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs text-slate-800">
                      {{ row.speed }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs text-slate-800">
                      {{ row.status }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs text-slate-800">
                      {{ row.directions }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs text-slate-800">
                      {{ row.date }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs text-slate-800">
                      {{ row.time }}
                    </td>
                    <td class="whitespace-nowrap px-4 py-2 text-center text-xs">
                      <div class="inline-flex items-center justify-center gap-1.5" :title="row.deliveryTitle">
                        <span
                          v-if="row.deliveryStatus === 'sending'"
                          class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-blue-600"
                          aria-hidden="true"
                        />
                        <svg
                          v-else-if="row.deliveryStatus === 'delivered'"
                          class="h-4 w-4 text-emerald-600"
                          viewBox="0 0 20 20"
                          fill="currentColor"
                          aria-hidden="true"
                        >
                          <path
                            fill-rule="evenodd"
                            d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z"
                            clip-rule="evenodd"
                          />
                        </svg>
                        <svg
                          v-else-if="row.deliveryStatus === 'failed'"
                          class="h-4 w-4 text-red-500"
                          viewBox="0 0 20 20"
                          fill="currentColor"
                          aria-hidden="true"
                        >
                          <path
                            fill-rule="evenodd"
                            d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
                            clip-rule="evenodd"
                          />
                        </svg>
                        <span class="max-w-[8rem] truncate text-slate-600" :class="deliveryLabelClass(row.deliveryStatus)">
                          {{ row.deliveryLabel }}
                        </span>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
              No parsed data to deliver yet
            </div>
          </div>
        </div>
        <div class="min-w-0 rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              Received Data
              <span class="ml-2 font-normal text-slate-500">({{ displayedPackets.length }} of 15 items)</span>
            </h2>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <ul v-if="displayedPackets.length" class="divide-y divide-slate-200">
              <li v-for="(pkt, index) in displayedPackets" :key="pkt.message_id + '-' + index"
                class="min-w-0 px-4 py-3 sm:px-6 hover:bg-slate-50/50">
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
                  class="mt-2 max-w-full whitespace-pre-wrap break-all rounded bg-slate-100 px-2 py-1.5 font-mono text-xs text-slate-700"
                >{{ normalizePayload(pkt.payload) }}</pre>
              </li>
            </ul>
            <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
              No data received yet
            </div>
          </div>
        </div>
      </div>
  </main>
</template>

<script setup>
import { computed } from 'vue'
import { useGpsPackets } from '../composables/useGpsPackets'

const { packets, deliveryBatches, error } = useGpsPackets()

function normalizePayload(payload) {
  if (payload == null) return ''
  if (typeof payload === 'string') return payload
  try {
    return JSON.stringify(payload)
  } catch {
    return String(payload)
  }
}

const dateFormatter = new Intl.DateTimeFormat('en-US', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

function formatTime(iso) {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toLocaleString('en-US')
  } catch {
    return iso
  }
}

function extractRecordsFromPayload(payload) {
  if (!payload || typeof payload !== 'string') return []
  try {
    const parsed = JSON.parse(payload)
    const data = Array.isArray(parsed?.data) ? parsed.data : []
    return data
  } catch {
    return []
  }
}

function formatDirections(directions) {
  if (!directions || typeof directions !== 'object') return ''
  const ew = directions.ew === 1 ? 'W' : 'E'
  const ns = directions.ns === 1 ? 'S' : 'N'
  return `${ns}${ew}`
}

function formatDateFromDateTime(value) {
  if (!value) return '—'
  try {
    let d
    if (typeof value === 'string') {
      if (value.includes('T')) {
        d = new Date(value)
      } else {
        const [datePart, timePart = '00:00:00'] = value.split(' ')
        d = new Date(`${datePart}T${timePart}`)
      }
    } else {
      d = new Date(value)
    }
    if (Number.isNaN(d.getTime())) return value
    return dateFormatter.format(d)
  } catch {
    return value
  }
}

function formatTimeFromDateTime(value) {
  if (!value) return '—'
  if (typeof value === 'string') {
    if (value.includes('T')) {
      try {
        const d = new Date(value)
        return d.toLocaleTimeString('en-US', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
        })
      } catch {
        // fall through to plain split
      }
    }
    const parts = value.split(' ')
    if (parts.length === 2) return parts[1]
    return value
  }
  try {
    const d = new Date(value)
    return d.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return '—'
  }
}

function shortServerLabel(server) {
  if (!server) return 'Sending…'
  try {
    const url = new URL(server)
    return url.host || server
  } catch {
    return server
  }
}

function deliveryStatusLabel(status, server) {
  const label = shortServerLabel(server)
  if (status === 'delivered') return label
  if (status === 'failed') return `Failed: ${label}`
  if (server) return label
  return 'Sending…'
}

function deliveryStatusTitle(status, server) {
  if (status === 'delivered') return `Delivered to ${server || 'server'}`
  if (status === 'failed') return `Failed to deliver to ${server || 'server'}`
  if (server) return `Sending to ${server}`
  return 'Preparing delivery'
}

function deliveryLabelClass(status) {
  if (status === 'delivered') return 'text-emerald-700'
  if (status === 'failed') return 'text-red-600'
  return 'text-blue-600'
}

const deliveredRows = computed(() => {
  const rows = []
  for (const batch of deliveryBatches.value) {
    const records = extractRecordsFromPayload(batch.payload)
    for (let i = 0; i < records.length; i += 1) {
      const rec = records[i] || {}
      const coord =
        Array.isArray(rec.coordinate) && rec.coordinate.length === 2
          ? `${rec.coordinate[0]}, ${rec.coordinate[1]}`
          : ''

      const dateTime = rec.date_time || batch.updated_at

      rows.push({
        key: `${batch.delivery_id}-${i}`,
        imei: rec.imei || '',
        coordinate: coord,
        speed: rec.speed ?? '',
        status: rec.status ?? '',
        directions: formatDirections(rec.directions),
        date: formatDateFromDateTime(dateTime),
        time: formatTimeFromDateTime(dateTime),
        deliveryStatus: batch.status,
        deliveryLabel: deliveryStatusLabel(batch.status, batch.target_server),
        deliveryTitle: deliveryStatusTitle(batch.status, batch.target_server),
      })
    }
  }
  return rows
})

const displayedDeliveredRows = computed(() => deliveredRows.value.slice(0, 15))

const displayedPackets = computed(() => packets.value.slice(0, 15))
</script>
