<template>
  <main class="mx-auto max-w-[1600px] px-4 py-6 sm:px-6 lg:px-8" dir="rtl">
    <div v-if="error" class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {{ error }}
    </div>

    <div class="mb-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
      <div class="flex flex-wrap items-end gap-4">
        <div class="min-w-[280px] flex-1">
          <label :for="dateInputId" class="mb-1.5 block text-sm font-medium text-slate-700">تاریخ (شمسی)</label>
          <PwtDatePicker :id="dateInputId" v-model="selectedDate" />
        </div>

        <div class="min-w-[320px] flex-1">
          <label :for="imeiSelectId" class="mb-1.5 block text-sm font-medium text-slate-700">دستگاه (IMEI)</label>
          <select
            :id="imeiSelectId"
            ref="imeiSelectEl"
            class="w-full"
            dir="ltr"
            :disabled="loadingDevices || !devices.length"
          >
            <option v-for="d in devices" :key="d.imei" :value="d.imei">
              {{ d.imei }} — {{ d.name }}
            </option>
          </select>
        </div>
      </div>
    </div>

    <div class="rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-4 py-3 sm:px-6">
        <h2 class="text-lg font-semibold text-slate-800">مسیر حرکت دستگاه</h2>
      </div>
      <div class="h-[72vh] w-full">
        <div ref="mapEl" class="h-full w-full rounded-b-lg" />
      </div>
    </div>
  </main>
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import PwtDatePicker from '../components/PwtDatePicker.vue'
import { todayGregorianISO, formatPersianDate } from '../utils/persianDate'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

const dateInputId = 'path-date'
const imeiSelectId = 'path-imei-select'

const selectedDate = ref(todayGregorianISO())
const selectedImei = ref('')
const devices = ref([])
const loadingDevices = ref(false)
const loading = ref(false)
const error = ref(null)

const mapEl = ref(null)
const imeiSelectEl = ref(null)

let map = null
let markersLayer = null
let lineLayer = null
let layersControl = null

function parseDateTimeLoose(value) {
  if (!value) return null
  if (typeof value === 'string') {
    const iso = value.includes('T') ? value : value.replace(' ', 'T')
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? null : d
  }
  try {
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? null : d
  } catch {
    return null
  }
}

function formatPersianDateTimeLoose(value) {
  if (!value) return '—'
  const d = parseDateTimeLoose(value)
  if (!d) return String(value)
  try {
    // Use Persian calendar formatting + keep time visible.
    const datePart = formatPersianDate(d.toISOString())
    const timePart = d.toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    return `${datePart} ${timePart}`
  } catch {
    return String(value)
  }
}

function toNumberOrNull(v) {
  if (v == null) return null
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : null
}

function normalizePathPoints(points) {
  const kept = []
  for (const p of points || []) {
    const lat = toNumberOrNull(p?.lat)
    const lng = toNumberOrNull(p?.lng)
    if (lat == null || lng == null) continue
    kept.push({
      id: p?.id ?? null,
      imei: p?.imei ?? '',
      lat,
      lng,
      speed: toNumberOrNull(p?.speed),
      status: toNumberOrNull(p?.status),
      date_time: p?.date_time ?? null,
    })
  }
  return kept
}

function initMap() {
  if (map || !mapEl.value) return
  map = L.map(mapEl.value, { zoomControl: true })

  const osm = L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors',
  })

  const cartoLight = L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
    maxZoom: 20,
    attribution: '&copy; OpenStreetMap contributors &copy; CARTO',
  })

  const cartoDark = L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
    maxZoom: 20,
    attribution: '&copy; OpenStreetMap contributors &copy; CARTO',
  })

  const openTopoMap = L.tileLayer('https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png', {
    maxZoom: 17,
    attribution: 'Map data: &copy; OpenStreetMap contributors, SRTM | Map style: &copy; OpenTopoMap',
  })

  // Default base layer
  osm.addTo(map)

  markersLayer = L.layerGroup().addTo(map)
  lineLayer = L.polyline([], { color: '#2563eb', weight: 4, opacity: 0.9 }).addTo(map)

  const baseLayers = {
    'OpenStreetMap': osm,
    'CARTO Light': cartoLight,
    'CARTO Dark': cartoDark,
    'OpenTopoMap': openTopoMap,
  }

  const overlays = {
    'نقاط': markersLayer,
    'مسیر': lineLayer,
  }

  layersControl = L.control.layers(baseLayers, overlays, { position: 'topleft', collapsed: true })
  layersControl.addTo(map)

  // Default view: Iran-ish; will be replaced by fitBounds when data arrives.
  map.setView([32.0, 53.0], 5)
}

function clearMap() {
  if (markersLayer) markersLayer.clearLayers()
  if (lineLayer) lineLayer.setLatLngs([])
}

function popupHtml(p) {
  const coord = `${p.lat.toFixed(6)}, ${p.lng.toFixed(6)}`
  const statusLabel = p.status == null ? '—' : String(p.status)
  const speedLabel = p.speed == null ? '—' : String(p.speed)
  const dtLabel = formatPersianDateTimeLoose(p.date_time)

  return `
    <div dir="rtl" style="min-width:220px;direction:rtl;text-align:right">
      <div style="font-weight:600;margin-bottom:6px">اطلاعات نقطه</div>
      <div><b>تاریخ/زمان:</b> ${dtLabel}</div>
      <div><b>سرعت:</b> ${speedLabel}</div>
      <div><b>وضعیت:</b> ${statusLabel}</div>
      <div><b>مختصات:</b> <span dir="ltr">${coord}</span></div>
    </div>
  `
}

function renderPoints(points, keptPoints) {
  clearMap()
  if (!map) return

  const latlngs = keptPoints.map((p) => [p.lat, p.lng])
  lineLayer.setLatLngs(latlngs)

  for (const p of keptPoints) {
    const isStoppage = p.speed === 0
    const marker = L.circleMarker([p.lat, p.lng], {
      radius: 6,
      weight: 2,
      color: isStoppage ? '#dc2626' : '#2563eb',
      fillColor: isStoppage ? '#fecaca' : '#bfdbfe',
      fillOpacity: 0.9,
    })
    marker.bindPopup(popupHtml(p), { maxWidth: 360 })
    marker.addTo(markersLayer)
  }

  if (latlngs.length) {
    const bounds = L.latLngBounds(latlngs)
    map.fitBounds(bounds.pad(0.15))
  }
}

async function fetchRecords() {
  if (!selectedDate.value || !selectedImei.value) return { records: [] }
  const params = new URLSearchParams({ date: selectedDate.value, imei: selectedImei.value })
  const response = await fetch(`/api/gps/path?${params.toString()}`)
  const data = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(data?.error || 'خطا در دریافت داده‌ها')
  }
  return data
}

async function refresh() {
  error.value = null
  loading.value = true
  try {
    const data = await fetchRecords()
    const kept = normalizePathPoints(data.points)
    // Server already de-duplicates consecutive stoppages and sorts.
    renderPoints(kept, kept)
  } catch (e) {
    clearMap()
    stats.value = { total: null, kept: null }
    error.value = e?.message || 'اتصال به سرور برقرار نشد.'
  } finally {
    loading.value = false
  }
}

async function loadDevices() {
  loadingDevices.value = true
  error.value = null
  try {
    const res = await fetch('/tractor_gps_devices.json')
    const data = await res.json()
    devices.value = Array.isArray(data) ? data : []
    if (!selectedImei.value && devices.value.length) {
      selectedImei.value = String(devices.value[0].imei || '')
    }
  } catch {
    devices.value = []
    error.value = 'خطا در بارگذاری لیست دستگاه‌ها'
  } finally {
    loadingDevices.value = false
  }
}

function initSelect2() {
  if (!imeiSelectEl.value) return
  const $ = window.jQuery || window.$
  if (!$) return
  const el = imeiSelectEl.value
  const $el = $(el)

  // Sync Vue state from DOM/select2 changes.
  $el.on('change', () => {
    selectedImei.value = String($el.val() || '')
  })

  // Select2 is loaded globally in main.js (ensures it actually works).
  if (typeof $el.select2 === 'function') {
    $el.select2({
      width: '100%',
      placeholder: 'انتخاب دستگاه…',
      allowClear: false,
    })
  }

  // Ensure the DOM matches our initial selected value.
  if (selectedImei.value) {
    $el.val(selectedImei.value)
    $el.trigger('change')
  }
}

function destroySelect2() {
  if (!imeiSelectEl.value) return
  const $ = window.jQuery || window.$
  if (!$) return
  const $el = $(imeiSelectEl.value)
  try {
    $el.off('change')
    if ($el.data('select2')) $el.select2('destroy')
  } catch {
    // ignore
  }
}

onMounted(async () => {
  initMap()
  await loadDevices()
  await nextTick()
  initSelect2()
  await refresh()
})

let refreshTimer = null
function scheduleRefresh() {
  if (!selectedDate.value || !selectedImei.value) return
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = null
    refresh()
  }, 150)
}

watch(selectedDate, () => scheduleRefresh())
watch(selectedImei, () => scheduleRefresh())

onBeforeUnmount(() => {
  destroySelect2()
  if (refreshTimer) clearTimeout(refreshTimer)
  if (map) {
    map.remove()
    map = null
  }
  layersControl = null
})
</script>

<style>
/* Make select2 match tailwind-ish input height */
.select2-container .select2-selection--single {
  height: 40px;
  border: 1px solid rgb(203 213 225);
  border-radius: 0.375rem;
}
.select2-container--default .select2-selection--single .select2-selection__rendered {
  line-height: 38px;
  padding-left: 10px;
  padding-right: 10px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 0.875rem;
}
.select2-container--default .select2-selection--single .select2-selection__arrow {
  height: 38px;
}

/* Ensure Leaflet popups render RTL */
.leaflet-popup-content {
  direction: rtl;
  text-align: right;
}

/* Make Leaflet layers control readable in RTL pages */
.leaflet-control-layers {
  direction: rtl;
  text-align: right;
}
</style>

