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
let lineLayerGroup = null
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

// --- Track smoothing / noise reduction ---
// Reduce visual zig-zag caused by GPS jitter:
// 1) drop micro-movements (distance gating)
// 2) simplify the resulting polyline (Douglas-Peucker)
const TRACK_FILTER = {
  minStillDistanceM: 2.0,
  minMoveDistanceM: 5.0,
  rdpToleranceM: 6.0,
  // Leaflet native polyline simplification (Douglas-Peucker) per zoom level.
  leafletSmoothFactor: 1.3,
}

// Break the path into segments to avoid "teleport" lines when data is missing.
// We split when consecutive points imply an unrealistic jump:
// - big time gap, or
// - big distance jump, or
// - implausible instantaneous speed.
const TRACK_GAP = {
  maxTimeGapSec: 120, // 2 minutes
  maxDistanceGapM: 300, // 300 meters
  maxSpeedMps: 35, // 126 km/h
  // If both time and distance are available, you can require both to exceed thresholds.
  // Keeping it false is stricter (splits on either condition).
  requireBothTimeAndDistance: true,
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

function deg2rad(deg) {
  return (deg * Math.PI) / 180
}

function haversineMeters(a, b) {
  // Distance in meters between two {lat,lng} points.
  const R = 6371000
  const lat1 = deg2rad(a.lat)
  const lat2 = deg2rad(b.lat)
  const dLat = lat2 - lat1
  const dLng = deg2rad(b.lng - a.lng)
  const sinDLat = Math.sin(dLat / 2)
  const sinDLng = Math.sin(dLng / 2)
  const h = sinDLat * sinDLat + Math.cos(lat1) * Math.cos(lat2) * sinDLng * sinDLng
  return 2 * R * Math.asin(Math.sqrt(h))
}

function filterByMinDistance(points) {
  if (!points?.length) return []
  const kept = [points[0]]

  for (let i = 1; i < points.length; i++) {
    const p = points[i]
    const last = kept[kept.length - 1]

    const distM = haversineMeters(last, p)
    const isStoppage = p.speed === 0
    const minDistM = isStoppage ? TRACK_FILTER.minStillDistanceM : TRACK_FILTER.minMoveDistanceM

    if (distM >= minDistM) kept.push(p)
  }

  return kept
}

function pointToSegmentDistanceMeters(p, start, end) {
  // Equirectangular projection around the segment start.
  const R = 6371000
  const lat0 = deg2rad(start.lat)
  const lon0 = deg2rad(start.lng)

  const toXY = (q) => {
    const lat = deg2rad(q.lat)
    const lon = deg2rad(q.lng)
    const x = (lon - lon0) * Math.cos(lat0) * R
    const y = (lat - lat0) * R
    return { x, y }
  }

  const s = toXY(start)
  const e = toXY(end)
  const pt = toXY(p)

  const vx = e.x - s.x
  const vy = e.y - s.y
  const len2 = vx * vx + vy * vy
  if (len2 === 0) {
    const dx = pt.x - s.x
    const dy = pt.y - s.y
    return Math.sqrt(dx * dx + dy * dy)
  }

  const t = Math.max(0, Math.min(1, ((pt.x - s.x) * vx + (pt.y - s.y) * vy) / len2))
  const projX = s.x + t * vx
  const projY = s.y + t * vy
  const dx = pt.x - projX
  const dy = pt.y - projY
  return Math.sqrt(dx * dx + dy * dy)
}

function simplifyRdp(points, toleranceM) {
  if (!points?.length) return []
  if (points.length <= 2) return points

  const start = points[0]
  const end = points[points.length - 1]

  let maxDist = -1
  let index = -1
  for (let i = 1; i < points.length - 1; i++) {
    const d = pointToSegmentDistanceMeters(points[i], start, end)
    if (d > maxDist) {
      maxDist = d
      index = i
    }
  }

  if (maxDist >= 0 && maxDist <= toleranceM) return [start, end]

  const left = simplifyRdp(points.slice(0, index + 1), toleranceM)
  const right = simplifyRdp(points.slice(index), toleranceM)
  return left.slice(0, -1).concat(right)
}

function smoothAndSimplifyTrack(points) {
  const distFiltered = filterByMinDistance(points)
  if (distFiltered.length <= 2) return distFiltered
  return simplifyRdp(distFiltered, TRACK_FILTER.rdpToleranceM)
}

function splitTrackByGaps(points) {
  if (!points?.length) return []

  const segments = []
  let current = [points[0]]

  const shouldSplit = (prev, next) => {
    const distM = haversineMeters(prev, next)

    const t1 = parseDateTimeLoose(prev?.date_time)?.getTime?.() ?? null
    const t2 = parseDateTimeLoose(next?.date_time)?.getTime?.() ?? null
    const dtSec = t1 != null && t2 != null ? Math.abs(t2 - t1) / 1000 : null

    const speedMps = dtSec && dtSec > 0 ? distM / dtSec : null

    const timeGap = dtSec != null && dtSec > TRACK_GAP.maxTimeGapSec
    const distGap = distM > TRACK_GAP.maxDistanceGapM
    const speedGap = speedMps != null && speedMps > TRACK_GAP.maxSpeedMps

    if (TRACK_GAP.requireBothTimeAndDistance && dtSec != null) {
      // Prefer the "GPSBabel-style" rule: split only if BOTH time and distance exceed thresholds.
      // Still split on obviously impossible speed.
      return speedGap || (timeGap && distGap)
    }

    // Split if any gap condition triggers.
    return speedGap || timeGap || distGap
  }

  for (let i = 1; i < points.length; i++) {
    const prev = points[i - 1]
    const next = points[i]

    if (shouldSplit(prev, next)) {
      if (current.length) segments.push(current)
      current = [next]
    } else {
      current.push(next)
    }
  }

  if (current.length) segments.push(current)
  return segments.filter((s) => s.length >= 2)
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

  const cartoVoyager = L.tileLayer('https://{s}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png', {
    maxZoom: 20,
    attribution: '&copy; OpenStreetMap contributors &copy; CARTO',
  })

  const openTopoMap = L.tileLayer('https://{s}.tile.opentopomap.org/{z}/{x}/{y}.png', {
    maxZoom: 17,
    attribution: 'Map data: &copy; OpenStreetMap contributors, SRTM | Map style: &copy; OpenTopoMap',
  })

  // Satellite imagery is very helpful for urban movement paths.
  const esriWorldImagery = L.tileLayer(
    'https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}',
    {
      maxZoom: 19,
      attribution: 'Tiles &copy; Esri — Source: Esri, Maxar, Earthstar Geographics, and the GIS User Community',
    }
  )

  // Default base layer
  osm.addTo(map)

  markersLayer = L.layerGroup().addTo(map)
  lineLayerGroup = L.layerGroup().addTo(map)

  const baseLayers = {
    'OpenStreetMap': osm,
    'CARTO Light': cartoLight,
    'CARTO Dark': cartoDark,
    'CARTO Voyager (Urban)': cartoVoyager,
    'Esri Satellite': esriWorldImagery,
    'OpenTopoMap': openTopoMap,
  }

  const overlays = {
    'نقاط': markersLayer,
    'مسیر': lineLayerGroup,
  }

  layersControl = L.control.layers(baseLayers, overlays, { position: 'topleft', collapsed: true })
  layersControl.addTo(map)

  // Default view: Iran-ish; will be replaced by fitBounds when data arrives.
  map.setView([32.0, 53.0], 5)
}

function clearMap() {
  if (markersLayer) markersLayer.clearLayers()
  if (lineLayerGroup) lineLayerGroup.clearLayers()
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

function bearingDeg(a, b) {
  if (!a || !b) return 0
  const lat1 = (a.lat * Math.PI) / 180
  const lat2 = (b.lat * Math.PI) / 180
  const dLon = ((b.lng - a.lng) * Math.PI) / 180
  const y = Math.sin(dLon) * Math.cos(lat2)
  const x = Math.cos(lat1) * Math.sin(lat2) - Math.sin(lat1) * Math.cos(lat2) * Math.cos(dLon)
  const brng = (Math.atan2(y, x) * 180) / Math.PI
  return (brng + 360) % 360
}

function arrowIcon({ color = '#2563eb', rotation = 0 } = {}) {
  // Simple SVG arrow that we rotate via CSS transform.
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24">
    <path d="M12 2l7 20-7-4-7 4 7-20z" fill="${color}" stroke="white" stroke-width="1.2" />
  </svg>`

  return L.divIcon({
    className: 'leaflet-arrow-marker',
    html: `<div class="arrow" style="transform: rotate(${rotation}deg)">${svg}</div>`,
    iconSize: [22, 22],
    iconAnchor: [11, 11],
    popupAnchor: [0, -11],
  })
}

function renderPoints(points, keptPoints) {
  clearMap()
  if (!map) return

  const segments = splitTrackByGaps(keptPoints)
  const allLatLngs = []

  for (const seg of segments) {
    const latlngs = seg.map((p) => [p.lat, p.lng])
    allLatLngs.push(...latlngs)

    L.polyline(latlngs, {
      color: '#2563eb',
      weight: 4,
      opacity: 0.9,
      smoothFactor: TRACK_FILTER.leafletSmoothFactor,
    }).addTo(lineLayerGroup)
  }

  for (let i = 0; i < keptPoints.length; i++) {
    const p = keptPoints[i]
    const isStoppage = p.speed === 0
    const prev = keptPoints[i - 1] || null
    const next = keptPoints[i + 1] || null
    const rotation = next ? bearingDeg(p, next) : prev ? bearingDeg(prev, p) : 0

    const marker = L.marker([p.lat, p.lng], {
      icon: arrowIcon({ color: isStoppage ? '#dc2626' : '#2563eb', rotation }),
      keyboard: false,
      riseOnHover: true,
    })
      .bindPopup(popupHtml(p), { maxWidth: 360 })
      .addTo(markersLayer)

    // If speed is exactly 0, visually de-emphasize direction.
    if (isStoppage) {
      marker.setZIndexOffset(100)
    }
  }

  if (allLatLngs.length) {
    const bounds = L.latLngBounds(allLatLngs)
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
    const normalized = normalizePathPoints(data.points)
    // Server already de-duplicates consecutive stoppages and sorts.
    const smoothed = smoothAndSimplifyTrack(normalized)
    renderPoints(normalized, smoothed)
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

.leaflet-arrow-marker {
  background: transparent;
  border: none;
}
.leaflet-arrow-marker .arrow {
  width: 22px;
  height: 22px;
  transform-origin: 50% 50%;
  filter: drop-shadow(0 1px 1px rgba(0, 0, 0, 0.35));
}
</style>

