// CSV the way the server writes it (pkg/models/csv_export.go): a byte order mark so Excel reads UTF-8, CRLF line ends,
// fields quoted where needed, and text that a spreadsheet would run as a formula prefixed with a single quote.

export const CSV_BOM = '﻿'

export type CsvCell = string | number | null | undefined

// Whitespace as unicode.IsSpace of Go sees it (the byte order mark aside), so both writers decide alike.
const startsWithSpace = /^\s/
const formulaAfterSpace = /^\s*[=+\-@]/

// A spreadsheet skips leading whitespace before it decides a cell is a formula.
export function csvSafeText(value: string): string {
	return /^[\t\r]/.test(value) || formulaAfterSpace.test(value) ? `'${value}` : value
}

// Like encoding/csv: fields with a separator, a quote, a line break or a leading space are quoted.
function quote(value: string): string {
	return /[",\r\n]/.test(value) || startsWithSpace.test(value) || value === '\\.' ? `"${value.replace(/"/g, '""')}"` : value
}

// Numbers are written by us and keep their sign, text is guarded.
export function csvCell(cell: CsvCell): string {
	if (cell === null || cell === undefined) {
		return ''
	}
	return typeof cell === 'number' ? quote(String(cell)) : quote(csvSafeText(cell))
}

export function toCsv(header: string[], rows: CsvCell[][]): string {
	return CSV_BOM + [header, ...rows].map(row => row.map(csvCell).join(',')).join('\r\n') + '\r\n'
}
