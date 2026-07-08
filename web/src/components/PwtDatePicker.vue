<template>
  <input
    :id="id"
    ref="inputEl"
    type="text"
    dir="ltr"
    autocomplete="off"
    class="w-full rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-sm text-slate-900 shadow-sm focus:border-slate-500 focus:outline-none focus:ring-1 focus:ring-slate-500"
    :placeholder="placeholder"
  />
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' }, // Gregorian ISO: YYYY-MM-DD
  id: { type: String, default: '' },
  placeholder: { type: String, default: 'انتخاب تاریخ…' },
})

const emit = defineEmits(['update:modelValue'])
const inputEl = ref(null)

let picker = null
let persianDateCtor = null

function setPickerUnix(unixMs) {
  if (!picker) return
  try {
    picker.setDate(unixMs)
  } catch {
    // ignore
  }
}

function gregorianIsoFromUnix(unixMs) {
  if (!persianDateCtor) return ''
  try {
    const d = new persianDateCtor(unixMs)
    // persian-date supports gregorian conversion
    return d.toCalendar('gregorian').toLocale('en').format('YYYY-MM-DD')
  } catch {
    return ''
  }
}

function unixFromGregorianIso(iso) {
  if (!iso) return null
  const d = new Date(`${iso}T00:00:00`)
  if (Number.isNaN(d.getTime())) return null
  return d.getTime()
}

onMounted(async () => {
  if (!inputEl.value) return

  // These are attached globally in main.js, but keep safe fallbacks.
  const $ = window.jQuery || window.$
  if (!$) return

  // persian-date attaches constructor to window in many builds
  persianDateCtor = window.persianDate || window.PersianDate || null

  try {
    picker = $(inputEl.value).pDatepicker({
      format: 'YYYY/MM/DD',
      initialValue: false,
      calendar: { persian: { locale: 'fa' } },
      onSelect: (unixMs) => {
        const iso = gregorianIsoFromUnix(unixMs)
        if (iso) emit('update:modelValue', iso)
      },
    })
  } catch {
    picker = null
  }

  const initialUnix = unixFromGregorianIso(props.modelValue)
  if (initialUnix) setPickerUnix(initialUnix)
})

watch(
  () => props.modelValue,
  (iso) => {
    const unixMs = unixFromGregorianIso(iso)
    if (unixMs) setPickerUnix(unixMs)
  },
)

onBeforeUnmount(() => {
  if (!inputEl.value) return
  const $ = window.jQuery || window.$
  if (!$) return
  try {
    $(inputEl.value).pDatepicker('destroy')
  } catch {
    // ignore
  }
})
</script>

