import { toGregorian, toJalaali, isLeapJalaaliYear, jalaaliMonthLength } from 'jalaali-js'

export const PERSIAN_MONTHS = [
  'فروردین',
  'اردیبهشت',
  'خرداد',
  'تیر',
  'مرداد',
  'شهریور',
  'مهر',
  'آبان',
  'آذر',
  'دی',
  'بهمن',
  'اسفند',
]

const persianDateFormatter = new Intl.DateTimeFormat('fa-IR-u-ca-persian', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

const persianDateTimeFormatter = new Intl.DateTimeFormat('fa-IR-u-ca-persian', {
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

export function todayGregorianISO() {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

export function gregorianISOToJalali(isoDate) {
  if (!isoDate) return null
  const [gy, gm, gd] = isoDate.split('-').map(Number)
  if (!Number.isFinite(gy) || !Number.isFinite(gm) || !Number.isFinite(gd)) return null
  try {
    const { jy, jm, jd } = toJalaali(gy, gm, gd)
    return { year: jy, month: jm, day: jd }
  } catch {
    return null
  }
}

export function jalaliToGregorianISO(year, month, day) {
  const { gy, gm, gd } = toGregorian(year, month, day)
  return `${gy}-${String(gm).padStart(2, '0')}-${String(gd).padStart(2, '0')}`
}

export function jalaliDaysInMonth(year, month) {
  if (!Number.isFinite(year) || !Number.isFinite(month)) return 31
  return jalaaliMonthLength(year, month)
}

export function formatPersianDate(value) {
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

export function formatPersianDateTime(value) {
  if (!value) return '—'
  try {
    const d = new Date(value)
    if (Number.isNaN(d.getTime())) return value
    return persianDateTimeFormatter.format(d)
  } catch {
    return value
  }
}

export function formatJalaliLabel(year, month, day) {
  const monthName = PERSIAN_MONTHS[month - 1] || month
  return `${day} ${monthName} ${year}`
}
