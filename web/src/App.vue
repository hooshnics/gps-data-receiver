<template>
  <div class="min-h-screen bg-slate-50 text-slate-900" dir="rtl">
    <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8">
      <div class="mb-6 flex flex-wrap items-start justify-between gap-4">
        <form class="flex flex-wrap items-center gap-2" dir="ltr" @submit.prevent="applyImeiFilter">
          <button
            type="submit"
            class="rounded-md bg-slate-800 px-4 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="!imeiInput.trim()"
          >
            فیلتر
          </button>
          <input
            v-model="imeiInput"
            type="text"
            inputmode="numeric"
            maxlength="10"
            pattern="\d{10}"
            placeholder="IMEI (10 digits)"
            class="w-40 rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
            :class="{ 'border-red-400 focus:border-red-500 focus:ring-red-500': imeiFilterError }"
          />
        </form>
        <div v-if="activeImeiFilter" class="text-sm text-slate-600">
          فیلتر IMEI:
          <span class="font-mono font-medium text-slate-800">{{ activeImeiFilter }}</span>
        </div>
      </div>
      <div v-if="imeiFilterError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
        {{ imeiFilterError }}
      </div>
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
import { computed, ref } from 'vue'
import { useGpsPackets } from './composables/useGpsPackets'

const { packets, deliveredPackets, connected, error, clearPackets, clearDeliveredPackets } = useGpsPackets()

const imeiInput = ref('')
const activeImeiFilter = ref(/** @type {string|null} */ (null))
const imeiFilterError = ref(/** @type {string|null} */ (null))

const IMEI_FILTER_PATTERN = /^\d{10}$/

function applyImeiFilter() {
  const value = imeiInput.value.trim()
  if (!value) {
    activeImeiFilter.value = null
    imeiFilterError.value = null
    return
  }
  if (!IMEI_FILTER_PATTERN.test(value)) {
    imeiFilterError.value = 'IMEI باید دقیقاً ۱۰ رقم باشد.'
    return
  }
  activeImeiFilter.value = value
  imeiFilterError.value = null
}

function imeiMatches(recordImei, filterImei) {
  if (!recordImei || !filterImei) return false
  const normalized = String(recordImei)
  return normalized === filterImei || normalized.endsWith(filterImei)
}

function extractImeisFromHooshnic(dataStr) {
  if (typeof dataStr !== 'string' || !dataStr) return []
  const lastComma = dataStr.lastIndexOf(',')
  if (lastComma === -1) return []
  const imei = dataStr.slice(lastComma + 1).trim()
  return imei ? [imei] : []
}

function extractImeisFromPayload(payload) {
  if (!payload || typeof payload !== 'string') return []
  const imeis = new Set()
  try {
    const parsed = JSON.parse(payload)
    const items = Array.isArray(parsed?.data) ? parsed.data : Array.isArray(parsed) ? parsed : []
    for (const item of items) {
      if (item && typeof item === 'object' && item.imei) {
        imeis.add(String(item.imei))
        continue
      }
      const raw = item?.data ?? item
      if (typeof raw === 'string') {
        for (const imei of extractImeisFromHooshnic(raw)) {
          imeis.add(imei)
        }
      }
    }
  } catch {
    for (const imei of extractImeisFromHooshnic(payload)) {
      imeis.add(imei)
    }
  }
  return [...imeis]
}

function payloadMatchesImeiFilter(payload, filterImei) {
  if (!filterImei) return true
  return extractImeisFromPayload(payload).some((imei) => imeiMatches(imei, filterImei))
}

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
const displayedDeliveredRows = computed(() => {
  const filter = activeImeiFilter.value
  const rows = filter
    ? deliveredRows.value.filter((row) => imeiMatches(row.imei, filter))
    : deliveredRows.value
  return rows.slice(0, 15)
})

/** Latest 15 packets, newest at top (composable already keeps newest first). */
const displayedPackets = computed(() => {
  const filter = activeImeiFilter.value
  const list = filter
    ? packets.value.filter((pkt) => payloadMatchesImeiFilter(pkt.payload, filter))
    : packets.value
  return list.slice(0, 15)
})
</script>
