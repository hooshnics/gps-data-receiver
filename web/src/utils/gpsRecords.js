const IMEI_PATTERN = /^\d{15}$/

export { IMEI_PATTERN }

export function parseParsedData(raw) {
  if (!raw) return {}
  if (typeof raw === 'object') return raw
  try {
    return JSON.parse(raw)
  } catch {
    return {}
  }
}

export function formatStatus(status) {
  if (status === 1 || status === '1') return 'on'
  if (status === 0 || status === '0') return 'off'
  return String(status ?? '—')
}

export function shortServerLabel(server) {
  if (!server) return '—'
  try {
    const url = new URL(server)
    return url.host || server
  } catch {
    return server
  }
}
