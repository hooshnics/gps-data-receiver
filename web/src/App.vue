<template>
  <div class="min-h-screen bg-slate-50 text-slate-900" dir="rtl">
    <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8">
      <div v-if="error" class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
        {{ error }}
      </div>

      <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <!-- Right: Delivered to destination servers -->
        <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              داده‌های ارسال‌شده به سرورها
              <span class="ml-2 font-normal text-slate-500">({{ displayedDeliveredRows.length }} از ۱۵ رکورد)</span>
            </h2>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <div v-if="displayedDeliveredRows.length" class="overflow-x-auto">
              <table class="min-w-full divide-y divide-slate-200 text-sm">
                <thead class="bg-slate-50 sticky top-0 z-10">
                  <tr>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">IMEI</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">مختصات</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">سرعت</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">وضعیت</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">جهت</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">تاریخ</th>
                    <th class="px-4 py-2 text-xs font-medium text-slate-600 text-center">زمان</th>
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
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
              هنوز داده‌ای به سرورها ارسال نشده است
            </div>
          </div>
        </div>
        <!-- Left: Received packets -->
        <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
          <div class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 sm:px-6">
            <h2 class="text-lg font-semibold text-slate-800">
              داده‌های دریافتی
              <span class="ml-2 font-normal text-slate-500">({{ displayedPackets.length }} از ۱۵ مورد)</span>
            </h2>
          </div>
          <div class="max-h-[70vh] overflow-y-auto">
            <ul v-if="displayedPackets.length" class="divide-y divide-slate-200">
              <li v-for="(pkt, index) in displayedPackets" :key="pkt.message_id + '-' + index"
                class="px-4 py-3 sm:px-6 hover:bg-slate-50/50">
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
                  class="mt-2 overflow-x-auto rounded bg-slate-100 px-2 py-1.5 font-mono text-xs text-slate-700">{{ payloadPreview(pkt.payload) }}</pre>
              </li>
            </ul>
            <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
              هنوز داده‌ای دریافت نشده است
            </div>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useGpsPackets } from './composables/useGpsPackets'

const { packets, deliveredPackets, connected, error, clearPackets, clearDeliveredPackets } = useGpsPackets()

const persianDateFormatter = new Intl.DateTimeFormat('fa-IR-u-ca-persian', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

function clearBothLists() {
  clearPackets()
  clearDeliveredPackets()
}

function formatTime(iso) {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toLocaleString('fa-IR')
  } catch {
    return iso
  }
}

function payloadPreview(payload, maxLen = 320) {
  if (typeof payload !== 'string') return ''
  if (payload.length <= maxLen) return payload
  return payload.slice(0, maxLen) + '…'
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

function formatJalaliFromDateTime(value) {
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
    return persianDateFormatter.format(d)
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
        return d.toLocaleTimeString('fa-IR', {
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
    return d.toLocaleTimeString('fa-IR', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return '—'
  }
}

const deliveredRows = computed(() => {
  const rows = []
  for (const pkt of deliveredPackets.value) {
    const records = extractRecordsFromPayload(pkt.payload)
    for (let i = 0; i < records.length; i += 1) {
      const rec = records[i] || {}
      const coord =
        Array.isArray(rec.coordinate) && rec.coordinate.length === 2
          ? `${rec.coordinate[0]}, ${rec.coordinate[1]}`
          : ''

      const dateTime = rec.date_time || pkt.delivered_at

      rows.push({
        key: `${pkt.delivered_at}-${pkt.target_server}-${i}`,
        imei: rec.imei || '',
        coordinate: coord,
        speed: rec.speed ?? '',
        status: rec.status ?? '',
        directions: formatDirections(rec.directions),
        date: formatJalaliFromDateTime(dateTime),
        time: formatTimeFromDateTime(dateTime),
      })
    }
  }
  return rows
})

/** Latest 15 records, newest at top (composable already keeps newest first). */
const displayedDeliveredRows = computed(() => deliveredRows.value.slice(0, 15))

/** Latest 15 packets, newest at top (composable already keeps newest first). */
const displayedPackets = computed(() => packets.value.slice(0, 15))
</script>
