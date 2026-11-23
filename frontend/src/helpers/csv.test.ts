import {describe, expect, it} from 'vitest'

import {CSV_BOM, csvCell, csvSafeText, toCsv} from './csv'

describe('csv', () => {
	it('guards cells a spreadsheet would run as a formula', () => {
		for (const value of ['=1+1', '+1', '-1', '@SUM(A1)', '\tcmd', '\rcmd', ' =1+1', '  -1', '\u00a0@SUM(A1)']) {
			expect(csvSafeText(value)).toBe(`'${value}`)
		}
		for (const value of ['', 'plain', 'a=b', '1+1', ' ', ' plain', ' a=b']) {
			expect(csvSafeText(value)).toBe(value)
		}
	})

	it('leaves numbers alone, also negative ones', () => {
		expect(csvCell(-5)).toBe('-5')
		expect(csvCell(12.5)).toBe('12.5')
		expect(csvCell('-5')).toBe('\'-5')
		expect(csvCell(null)).toBe('')
	})

	it('quotes commas, quotes and line breaks', () => {
		expect(csvCell('a,b')).toBe('"a,b"')
		expect(csvCell('say "hi"')).toBe('"say ""hi"""')
		expect(csvCell('two\nlines')).toBe('"two\nlines"')
	})

	it('quotes a field with a leading space like the server does', () => {
		expect(csvCell(' lead')).toBe('" lead"')
		expect(csvCell(' =1+1')).toBe('\' =1+1')
		expect(csvCell('trail ')).toBe('trail ')
	})

	it('starts with a byte order mark and keeps Turkish characters', () => {
		const text = toCsv(['Başlık', 'Değer'], [['İğdır şğüöç', 3], ['=cmd', -1]])
		expect(text.startsWith(CSV_BOM)).toBe(true)
		expect(text).toBe(`${CSV_BOM}Başlık,Değer\r\nİğdır şğüöç,3\r\n'=cmd,-1\r\n`)
	})
})
