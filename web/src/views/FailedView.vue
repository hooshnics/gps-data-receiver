<template>
  <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8" dir="rtl">
    <div class="mb-8 rounded-lg border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <form class="flex flex-wrap items-end gap-4" @submit.prevent="submitQuery">
        <div class="min-w-[280px] flex-1">
          <label :for="dateInputId" class="mb-1.5 block text-sm font-medium text-slate-700">تاریخ (شمسی)</label>
          <PersianDateInput :id="dateInputId" v-model="selectedDate" />
        </div>
        <div class="min-w-[200px]">
          <label for="failed-imei" class="mb-1.5 block text-sm font-medium text-slate-700">IMEI</label>
          <input
            id="failed-imei"
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
        <p v-if="resultCount !== null" class="text-sm text-slate-600">
          {{ resultCount }} رکورد یافت شد
        </p>
      </div>
    </div>

    <div v-if="imeiError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ imeiError }}
    </div>
    <div v-if="queryError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ queryError }}
    </div>

    <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-4 py-3 sm:px-6">
        <h2 class="text-lg font-semibold text-slate-800">تحویل‌های ناموفق</h2>
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
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">سرور مقصد</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">خطا</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">زمان خطا</th>
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
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-red-700" dir="ltr">{{ row.targetServer }}</td>
              <td class="max-w-[14rem] truncate px-3 py-2 text-center text-xs text-red-600" :title="row.errorMessage" dir="ltr">
                {{ row.errorMessage }}
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.failedAt }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
          {{ hasSearched ? 'رکوردی برای فیلترهای انتخاب‌شده یافت نشد' : 'تاریخ را انتخاب کرده و جستجو کنید' }}
        </div>
      </div>
    </div>
  </main>
</template>

<script setup>
import { computed, ref } from 'vue'
import PersianDateInput from '../components/PersianDateInput.vue'
import { IMEI_PATTERN, formatStatus, parseParsedData, shortServerLabel } from '../utils/gpsRecords'
import { formatPersianDate, formatPersianDateTime, todayGregorianISO } from '../utils/persianDate'

const dateInputId = 'failed-date'
const selectedDate = ref(todayGregorianISO())
const imeiInput = ref('')
const loading = ref(false)
const queryError = ref(null)
const imeiError = ref(null)
const hasSearched = ref(false)
const resultRecords = ref([])
const resultCount = ref(null)

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
      targetServer: shortServerLabel(record.target_server),
      errorMessage: record.error_message || '—',
      failedAt: formatPersianDateTime(record.failed_at),
    }
  }),
)

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

  try {
    const params = new URLSearchParams({ date: selectedDate.value })
    if (imei) params.set('imei', imei)

    const response = await fetch(`/api/gps/failed-records?${params.toString()}`)
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
