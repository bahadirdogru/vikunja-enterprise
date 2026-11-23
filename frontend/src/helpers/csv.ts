// CSV the way the server writes it (pkg/models/csv_export.go): a byte order mark so Excel reads UTF-8, CRLF line ends,
// fields quoted where needed, and text that a spreadsheet would run as a formula prefixed with a single quote.

export const CSV_BOM = '﻿'

export type CsvCell = string | number | null | undefined

export function csvSafeText(value: string): string {
	return /^[=+\-@\t\r]/.test(value) ? `'${value}` : value
}

function quote(value: string): string {
	return /[",\r\n]/.test(value) ? `"${value.replace(/"/g, '""')}"` : value
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
