function triggerDownload(blob, filename) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

function escapeHtml(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

export function downloadJson(data, filename) {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json;charset=utf-8' })
  triggerDownload(blob, filename)
}

export function downloadExcel(rows, filename, sheetName = 'Data') {
  if (!rows.length) return

  const columns = Object.keys(rows[0])
  const headerCells = columns.map((col) => `<th>${escapeHtml(col)}</th>`).join('')
  const bodyRows = rows
    .map((row) => {
      const cells = columns.map((col) => `<td>${escapeHtml(row[col])}</td>`).join('')
      return `<tr>${cells}</tr>`
    })
    .join('')

  const html = `<!DOCTYPE html>
<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">
<head>
<meta charset="UTF-8">
<!--[if gte mso 9]><xml><x:ExcelWorkbook><x:ExcelWorksheets><x:ExcelWorksheet><x:Name>${escapeHtml(sheetName)}</x:Name><x:WorksheetOptions><x:DisplayGridlines/></x:WorksheetOptions></x:ExcelWorksheet></x:ExcelWorksheets></x:ExcelWorkbook></xml><![endif]-->
</head>
<body><table><thead><tr>${headerCells}</tr></thead><tbody>${bodyRows}</tbody></table></body></html>`

  const blob = new Blob(['\ufeff', html], { type: 'application/vnd.ms-excel;charset=utf-8' })
  const excelFilename = filename.endsWith('.xls') || filename.endsWith('.xlsx') ? filename : `${filename}.xls`
  triggerDownload(blob, excelFilename)
}

export function buildExportFilename(prefix, date, imei, extension) {
  const imeiPart = imei ? `-${imei}` : ''
  return `${prefix}-${date}${imeiPart}.${extension}`
}
