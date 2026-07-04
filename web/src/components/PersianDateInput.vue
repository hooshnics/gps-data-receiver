<template>
  <div class="flex flex-wrap gap-2">
    <select
      :id="id ? `${id}-day` : undefined"
      v-model.number="jalaliDay"
      class="min-w-[4.5rem] rounded-md border border-slate-300 bg-white px-2 py-2 text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      @change="emitGregorian"
    >
      <option v-for="d in dayOptions" :key="d" :value="d">{{ d }}</option>
    </select>
    <select
      :id="id ? `${id}-month` : undefined"
      v-model.number="jalaliMonth"
      class="min-w-[7rem] flex-1 rounded-md border border-slate-300 bg-white px-2 py-2 text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      @change="onMonthChange"
    >
      <option v-for="(name, index) in persianMonths" :key="name" :value="index + 1">
        {{ name }}
      </option>
    </select>
    <select
      :id="id ? `${id}-year` : undefined"
      v-model.number="jalaliYear"
      class="min-w-[5.5rem] rounded-md border border-slate-300 bg-white px-2 py-2 text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
      @change="onYearChange"
    >
      <option v-for="y in yearOptions" :key="y" :value="y">{{ y }}</option>
    </select>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import {
  PERSIAN_MONTHS,
  gregorianISOToJalali,
  jalaliDaysInMonth,
  jalaliToGregorianISO,
  todayGregorianISO,
} from '../utils/persianDate'

const props = defineProps({
  modelValue: {
    type: String,
    default: '',
  },
  id: {
    type: String,
    default: '',
  },
})

const emit = defineEmits(['update:modelValue'])

const persianMonths = PERSIAN_MONTHS

function initJalaliFromGregorian(iso) {
  const jalali = gregorianISOToJalali(iso || todayGregorianISO())
  return jalali || { year: 1404, month: 1, day: 1 }
}

const initial = initJalaliFromGregorian(props.modelValue)
const jalaliYear = ref(initial.year)
const jalaliMonth = ref(initial.month)
const jalaliDay = ref(initial.day)

const yearOptions = computed(() => {
  const center = jalaliYear.value || initial.year
  return Array.from({ length: 11 }, (_, i) => center - 5 + i)
})

const dayOptions = computed(() => {
  const count = jalaliDaysInMonth(jalaliYear.value, jalaliMonth.value)
  const safeCount = Number.isFinite(count) && count > 0 ? count : 31
  return Array.from({ length: safeCount }, (_, i) => i + 1)
})

function emitGregorian() {
  const year = jalaliYear.value || initial.year
  const month = jalaliMonth.value || initial.month
  let day = jalaliDay.value || initial.day

  const maxDay = jalaliDaysInMonth(year, month)
  if (day > maxDay) {
    day = maxDay
    jalaliDay.value = maxDay
  }

  emit('update:modelValue', jalaliToGregorianISO(year, month, day))
}

function onMonthChange() {
  emitGregorian()
}

function onYearChange() {
  emitGregorian()
}

watch(
  () => props.modelValue,
  (iso) => {
    if (!iso) return
    const jalali = gregorianISOToJalali(iso)
    if (!jalali) return
    jalaliYear.value = jalali.year
    jalaliMonth.value = jalali.month
    jalaliDay.value = jalali.day
  },
)

onMounted(() => {
  if (!props.modelValue) {
    emitGregorian()
  }
})
</script>
