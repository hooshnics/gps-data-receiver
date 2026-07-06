<template>
  <main class="mx-auto max-w-[1600px] px-4 py-8 sm:px-6 lg:px-8" dir="rtl">
    <div class="mb-8 rounded-lg border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <p v-if="totalCount !== null" class="text-sm text-slate-600">
          {{ totalCount }} رکورد یافت شد
          <span v-if="totalPages > 0" class="text-slate-500"> — صفحه {{ currentPage }} از {{ totalPages }}</span>
        </p>
        <div v-if="totalPages > 1" class="flex items-center gap-2">
          <button
            type="button"
            class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="loading || currentPage <= 1"
            @click="goToPage(currentPage - 1)"
          >
            قبلی
          </button>
          <button
            type="button"
            class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="loading || currentPage >= totalPages"
            @click="goToPage(currentPage + 1)"
          >
            بعدی
          </button>
        </div>
      </div>
    </div>

    <div v-if="queryError" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ queryError }}
    </div>

    <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-4 py-3 sm:px-6">
        <h2 class="text-lg font-semibold text-slate-800">داده‌های نامعتبر / پارس‌نشده</h2>
      </div>
      <div class="max-h-[70vh] overflow-auto">
        <table v-if="rows.length" class="min-w-full divide-y divide-slate-200 text-sm">
          <thead class="sticky top-0 z-10 bg-slate-50">
            <tr>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">#</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">داده خام</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">علت خطا</th>
              <th class="px-3 py-2 text-center text-xs font-medium text-slate-600">زمان ثبت</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100 bg-white">
            <tr v-for="(row, index) in rows" :key="row.id" class="hover:bg-slate-50/60">
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-700">
                {{ rowOffset + index + 1 }}
              </td>
              <td class="max-w-[32rem] break-all px-3 py-2 text-center font-mono text-xs text-slate-800" dir="ltr">
                {{ row.rawData }}
              </td>
              <td class="max-w-[14rem] truncate px-3 py-2 text-center text-xs text-red-600" :title="row.errorReason" dir="ltr">
                {{ row.errorReason }}
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-center text-xs text-slate-800">{{ row.createdAt }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else-if="loading" class="px-4 py-12 text-center text-sm text-slate-500">
          در حال بارگذاری…
        </div>
        <div v-else class="px-4 py-12 text-center text-sm text-slate-500">
          رکوردی یافت نشد
        </div>
      </div>
    </div>
  </main>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { formatPersianDateTime } from '../utils/persianDate'

const pageSize = 50
const currentPage = ref(1)
const loading = ref(false)
const queryError = ref(null)
const resultRecords = ref([])
const totalCount = ref(null)

const totalPages = computed(() => {
  if (totalCount.value === null || totalCount.value === 0) return 0
  return Math.ceil(totalCount.value / pageSize)
})

const rowOffset = computed(() => (currentPage.value - 1) * pageSize)

const rows = computed(() =>
  resultRecords.value.map((record) => ({
    id: record.id,
    rawData: record.raw_data || '—',
    errorReason: record.error_reason || '—',
    createdAt: formatPersianDateTime(record.created_at),
  })),
)

async function fetchPage(page) {
  queryError.value = null
  loading.value = true

  try {
    const params = new URLSearchParams({
      page: String(page),
      limit: String(pageSize),
    })

    const response = await fetch(`/api/gps/invalid-records?${params.toString()}`)
    const data = await response.json()

    if (!response.ok) {
      queryError.value = data.error || 'خطا در دریافت داده‌ها'
      resultRecords.value = []
      totalCount.value = null
      return
    }

    resultRecords.value = data.records || []
    totalCount.value = data.total ?? resultRecords.value.length
    currentPage.value = data.page ?? page
  } catch {
    queryError.value = 'اتصال به سرور برقرار نشد.'
    resultRecords.value = []
    totalCount.value = null
  } finally {
    loading.value = false
  }
}

async function goToPage(page) {
  if (page < 1 || (totalPages.value > 0 && page > totalPages.value)) return
  await fetchPage(page)
}

onMounted(() => {
  fetchPage(1)
})
</script>
