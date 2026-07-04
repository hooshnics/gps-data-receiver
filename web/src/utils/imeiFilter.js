export const IMEI_FILTER_PATTERN = /^\d{15}$/

export function imeiMatches(recordImei, filterImei) {
  if (!recordImei || !filterImei) return false
  return String(recordImei) === filterImei
}

export function normalizePayload(payload) {
  if (payload == null) return ''
  if (typeof payload === 'string') return payload
  try {
    return JSON.stringify(payload)
  } catch {
    return String(payload)
  }
}

export function extractImeisFromHooshnic(dataStr) {
  if (typeof dataStr !== 'string' || !dataStr) return []
  const lastComma = dataStr.lastIndexOf(',')
  if (lastComma === -1) return []
  const imei = dataStr.slice(lastComma + 1).trim()
  return /^\d{15}$/.test(imei) ? [imei] : []
}

function collectPayloadItems(parsed) {
  if (parsed == null) return []
  if (Array.isArray(parsed)) return parsed
  if (typeof parsed !== 'object') return []

  if (Array.isArray(parsed.data)) return parsed.data
  if (typeof parsed.data === 'string') return [{ data: parsed.data }]
  if (parsed.data && typeof parsed.data === 'object') return [parsed.data]

  return [parsed]
}

export function extractImeisFromPayload(payload) {
  const text = normalizePayload(payload)
  if (!text) return []

  const imeis = new Set()
  try {
    const parsed = JSON.parse(text)
    for (const item of collectPayloadItems(parsed)) {
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
    for (const imei of text.match(/\d{15}/g) ?? []) {
      imeis.add(imei)
    }
  }

  return [...imeis]
}

export function payloadMatchesImeiFilter(payload, filterImei) {
  if (!filterImei) return true
  const text = normalizePayload(payload)
  if (text.includes(filterImei)) return true
  return extractImeisFromPayload(payload).some((imei) => imeiMatches(imei, filterImei))
}
