<template>
  <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8" dir="rtl">
    <div class="mb-8 rounded-lg border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <form class="flex flex-wrap items-end gap-4" @submit.prevent="submitQuery">
        <div class="min-w-[280px] flex-1">
          <label :for="dateInputId" class="mb-1.5 block text-sm font-medium text-slate-700">تاریخ (شمسی)</label>
          <PersianDateInput :id="dateInputId" v-model="selectedDate" />
        </div>
        <div class="min-w-[200px]">
          <label for="analysis-imei" class="mb-1.5 block text-sm font-medium text-slate-700">IMEI</label>
          <input
            id="analysis-imei"
            v-model="imeiInput"
            type="text"
            inputmode="numeric"
            maxlength="15"
            placeholder="867994064030931"
            dir="ltr"
            class="w-full rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
            :class="{ 'border-red-400 focus:border-red-500 focus:ring-red-500': imeiError }"
          />
        </div>
        <button
          type="submit"
          class="rounded-md bg-slate-800 px-5 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="loading || !selectedDate"
        >
          {{ loading ? 'در حال بارگذاری…' : 'جستجو' }}
        </button>
      </form>

      <div class="mt-5 flex flex-wrap items-center justify-between gap-4">
        <div
          class="inline-flex rounded-lg border border-slate-200 bg-slate-100 p-1"
          role="tablist"
          aria-label="نوع نمایش داده"
        >
          <label
            class="cursor-pointer rounded-md px-4 py-2 text-sm font-medium transition"
            :class="viewMode === 'parsed' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'"
          >
            <input v-model="viewMode" type="radio" value="parsed" class="sr-only" />
            داده پارس‌شده
          </label>
          <label
            class="cursor-pointer rounded-md px-4 py-2 text-sm font-medium transition"
            :class="viewMode === 'raw' ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-600 hover:text-slate-900'"
          >
            <input v-model="viewMode" type="radio" value="raw" class="sr-only" />
            داده خام
          </label>
        </div>
        <div class="flex flex-wrap items-center gap-3">
          <div
            v-if="resultRecords.length"
            class="flex flex-wrap items-center gap-2"
            aria-label="خروجی گرفتن"
          >
            <span class="text-xs font-medium text-slate-500">پارس‌شده:</span>
            <button
              type="button"
              class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition hover:bg-slate-50"
              @click="exportParsed('json')"
            >
              JSON
            </button>
            <button
              type="button"
              class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition hover:bg-slate-50"
              @click="exportParsed('excel')"
            >
              Excel
            </button>
            <span class="mx-1 text-slate-300">|</span>
            <span class="text-xs font-medium text-slate-500">خام:</span>
            <button
              type="button"
              class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition hover:bg-slate-50"
              @click="exportRaw('json')"
            >
              JSON
            </button>
            <button
              type="button"
              class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition hover:bg-slate-50"
              @click="exportRaw('excel')"
            >
              Excel
            </button>
          </div>
          <p v-if="resultCount !== null" class="text-sm text-slate-600">
            {{ resultCount }} رکورد یافت شد
          </p>
        </div>
      </div>
    </div>

    <div v-if="imeiError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ imeiError }}
    </div>
    <div v-if="queryError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ queryError }}
    </div>

    <div v-if="viewMode === 'parsed'" class="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-4 py-3 sm:px-6">
        <h2 class="text-lg font-semibold text-slate-800">تحویل‌های موفق</h2>
      </div>
      <div class="max-h-[70vh] overflow-auto">
        <table v-if="parsedRows.length" class="min-w-full divide-y divide-slate-200 text-sm">
          <thead class="sticky top-0 z-10 bg-slate-50">
            <tr>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">#</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">IMEI</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">عرض جغرافیایی</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">طول جغرافیایی</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">سرعت</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">وضعیت</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">تاریخ دستگاه</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">زمان ذخیره</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100 bg-white">
            <tr v-for="(row, index) in parsedRows" :key="row.id" class="hover:bg-slate-50/60">
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-700">{{ index + 1 }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center font-mono text-xs text-slate-800" dir="ltr">{{ row.imei }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center font-mono text-xs text-slate-800" dir="ltr">{{ row.latitude }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center font-mono text-xs text-slate-800" dir="ltr">{{ row.longitude }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.speed }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.statusLabel }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.deviceDateTime }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.parsedAt }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
          {{ hasSearched ? 'رکوردی برای فیلترهای انتخاب‌شده یافت نشد' : 'تاریخ را انتخاب کرده و جستجو کنید' }}
        </div>
      </div>
    </div>

    <div v-else class="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-4 py-3 sm:px-6">
        <h2 class="text-lg font-semibold text-slate-800">داده خام دریافتی</h2>
      </div>
      <div v-if="rawRecords.length" class="divide-y divide-slate-200">
        <div v-for="(record, index) in rawRecords" :key="record.id" class="px-4 sm:px-6">
          <button
            type="button"
            class="flex w-full items-center justify-between gap-3 py-3 text-left"
            @click="toggleRaw(record.id)"
          >
            <span class="text-sm font-medium text-slate-800">
              #{{ index + 1 }} — IMEI:
              <span class="font-mono" dir="ltr">{{ record.imei }}</span>
            </span>
            <span class="shrink-0 text-xs text-slate-500">{{ formatParsedAt(record.created_at) }}</span>
            <svg
              class="h-4 w-4 shrink-0 text-slate-500 transition-transform"
              :class="{ 'rotate-180': expandedRaw.has(record.id) }"
              viewBox="0 0 20 20"
              fill="currentColor"
              aria-hidden="true"
            >
              <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
            </svg>
          </button>
          <div v-show="expandedRaw.has(record.id)">
            <pre class="mb-3 overflow-x-auto rounded bg-slate-100 px-3 py-2 font-mono text-xs text-slate-700" dir="ltr">{{ record.raw_data }}</pre>
          </div>
          <div v-show="!expandedRaw.has(record.id)">
            <pre class="mb-3 overflow-x-auto rounded bg-slate-100 px-3 py-2 font-mono text-xs text-slate-700" dir="ltr">{{ rawPreview(record.raw_data) }}</pre>
            <button
              v-if="isLongRaw(record.raw_data)"
              type="button"
              class="mb-3 text-xs text-slate-600 underline hover:text-slate-900"
              @click="toggleRaw(record.id)"
            >
              نمایش کامل
            </button>
          </div>
        </div>
      </div>
      <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
        {{ hasSearched ? 'رکوردی برای فیلترهای انتخاب‌شده یافت نشد' : 'تاریخ را انتخاب کرده و جستجو کنید' }}
      </div>
    </div>
  </main>
</template>

<script setup>
import { computed, ref } from 'vue'
import PersianDateInput from '../components/PersianDateInput.vue'
import { IMEI_PATTERN, formatStatus, parseParsedData } from '../utils/gpsRecords'
import { formatPersianDate, formatPersianDateTime, todayGregorianISO } from '../utils/persianDate'
import { buildExportFilename, downloadExcel, downloadJson } from '../utils/exportData'

const RAW_COLLAPSE_THRESHOLD = 120
const dateInputId = 'analysis-date'

const selectedDate = ref(todayGregorianISO())
const imeiInput = ref('')
const viewMode = ref('parsed')
const loading = ref(false)
const queryError = ref(null)
const imeiError = ref(null)
const hasSearched = ref(false)
const resultRecords = ref([])
const resultCount = ref(null)
const expandedRaw = ref(new Set())

function formatParsedAt(iso) {
  return formatPersianDateTime(iso)
}

function isLongRaw(text) {
  return typeof text === 'string' && text.length > RAW_COLLAPSE_THRESHOLD
}

function rawPreview(text) {
  if (!text) return ''
  if (!isLongRaw(text)) return text
  return `${text.slice(0, RAW_COLLAPSE_THRESHOLD)}…`
}

function toggleRaw(id) {
  const next = new Set(expandedRaw.value)
  if (next.has(id)) {
    next.delete(id)
  } else {
    next.add(id)
  }
  expandedRaw.value = next
}

const parsedRows = computed(() =>
  resultRecords.value.map((record) => {
    const parsed = parseParsedData(record.parsed_data)
    const coord = Array.isArray(parsed.coordinate) ? parsed.coordinate : []
    return {
      id: record.id,
      imei: record.imei,
      latitude: coord[0] ?? '—',
      longitude: coord[1] ?? '—',
      speed: parsed.speed ?? '—',
      statusLabel: formatStatus(parsed.status),
      deviceDateTime: formatPersianDate(parsed.date_time),
      parsedAt: formatParsedAt(record.created_at),
    }
  }),
)

const rawRecords = computed(() => resultRecords.value)

function buildParsedExportRows() {
  return resultRecords.value.map((record, index) => {
    const parsed = parseParsedData(record.parsed_data)
    const coord = Array.isArray(parsed.coordinate) ? parsed.coordinate : []
    return {
      ردیف: index + 1,
      IMEI: record.imei,
      'عرض جغرافیایی': coord[0] ?? '',
      'طول جغرافیایی': coord[1] ?? '',
      سرعت: parsed.speed ?? '',
      وضعیت: formatStatus(parsed.status),
      'تاریخ دستگاه': formatPersianDate(parsed.date_time),
      'زمان ذخیره': formatParsedAt(record.created_at),
    }
  })
}

function buildParsedExportJson() {
  return resultRecords.value.map((record) => {
    const parsed = parseParsedData(record.parsed_data)
    const coord = Array.isArray(parsed.coordinate) ? parsed.coordinate : []
    return {
      id: record.id,
      imei: record.imei,
      latitude: coord[0] ?? null,
      longitude: coord[1] ?? null,
      speed: parsed.speed ?? null,
      status: parsed.status ?? null,
      directions: parsed.directions ?? null,
      device_date_time: parsed.date_time ?? null,
      created_at: record.created_at,
      parsed_data: parsed,
    }
  })
}

function buildRawExportRows() {
  return resultRecords.value.map((record, index) => ({
    ردیف: index + 1,
    IMEI: record.imei,
    'داده خام': record.raw_data ?? '',
    'زمان دریافت': formatParsedAt(record.created_at),
  }))
}

function buildRawExportJson() {
  return resultRecords.value.map((record) => ({
    id: record.id,
    imei: record.imei,
    raw_data: record.raw_data ?? '',
    created_at: record.created_at,
  }))
}

function currentImeiFilter() {
  return imeiInput.value.trim()
}

function exportParsed(format) {
  if (!resultRecords.value.length) return
  const imei = currentImeiFilter()
  if (format === 'json') {
    downloadJson(
      buildParsedExportJson(),
      buildExportFilename('gps-parsed', selectedDate.value, imei, 'json'),
    )
    return
  }
  downloadExcel(
    buildParsedExportRows(),
    buildExportFilename('gps-parsed', selectedDate.value, imei, 'xls'),
    'Parsed',
  )
}

function exportRaw(format) {
  if (!resultRecords.value.length) return
  const imei = currentImeiFilter()
  if (format === 'json') {
    downloadJson(
      buildRawExportJson(),
      buildExportFilename('gps-raw', selectedDate.value, imei, 'json'),
    )
    return
  }
  downloadExcel(
    buildRawExportRows(),
    buildExportFilename('gps-raw', selectedDate.value, imei, 'xls'),
    'Raw',
  )
}

async function submitQuery() {
  queryError.value = null
  imeiError.value = null

  const imei = imeiInput.value.trim()
  if (imei && !IMEI_PATTERN.test(imei)) {
    imeiError.value = 'IMEI باید دقیقاً ۱۵ رقم باشد.'
    return
  }

  if (!selectedDate.value) {
    queryError.value = 'لطفاً تاریخ را انتخاب کنید.'
    return
  }

  loading.value = true
  expandedRaw.value = new Set()

  try {
    const params = new URLSearchParams({ date: selectedDate.value })
    if (imei) params.set('imei', imei)

    const response = await fetch(`/api/gps/records?${params.toString()}`)
    const data = await response.json()

    if (!response.ok) {
      queryError.value = data.error || 'خطا در دریافت داده‌ها'
      resultRecords.value = []
      resultCount.value = null
      return
    }

    resultRecords.value = data.records || []
    resultCount.value = data.count ?? resultRecords.value.length
    hasSearched.value = true
  } catch {
    queryError.value = 'اتصال به سرور برقرار نشد.'
    resultRecords.value = []
    resultCount.value = null
  } finally {
    loading.value = false
  }
}
</script>
