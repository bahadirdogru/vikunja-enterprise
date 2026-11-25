import {afterAll, beforeAll, describe, expect, it} from 'vitest'

import {setLanguage} from '@/i18n'
import {automationCodeText, automationMessageText, automationOutput} from '@/helpers/automationMessages'

describe('the sentences of the server about an automation run', () => {
	describe('in English', () => {
		it('says a code with its values', () => {
			expect(automationCodeText('set_title', {title: 'Yeni'}, 'fallback')).toBe('set title to "Yeni"')
		})
	})

	describe('in Turkish', () => {
		beforeAll(async () => { await setLanguage('tr-TR') })
		afterAll(async () => { await setLanguage('en') })

		it('says a code with its values', () => {
			expect(automationCodeText('set_title', {title: 'Yeni'}, 'set title to "Yeni"')).toBe('Başlık "Yeni" yapılacak')
		})

		it('frames the error of a script and keeps its text', () => {
			expect(automationCodeText('script_thrown', {message: 'ReferenceError: x is not defined'}, 'x'))
				.toBe('Betik hatası: ReferenceError: x is not defined')
		})

		it('looks a numeric code up as an API error', () => {
			expect(automationCodeText('4002', {}, 'The task does not exist.')).toBe('Görev bulunamadı.')
		})

		it('wraps the sentence of an action that had nothing to do', () => {
			expect(automationCodeText('nothing_to_do', {inner: 'mark_done'}, 'nothing to do: mark the task as done (already so)'))
				.toBe('Yapılacak bir şey yok, zaten öyle: Görev tamamlandı olarak işaretlenecek')
		})

		it('says the error of a failed condition inside the note', () => {
			expect(automationCodeText('note_conditions_fail', {code: 'script_thrown', message: 'boom'}, 'x'))
				.toBe('Koşullar başarısız oldu: Betik hatası: boom')
		})

		it('shows the English text for a code it does not know', () => {
			expect(automationCodeText('a_code_from_a_newer_server', {}, 'the English text')).toBe('the English text')
			expect(automationCodeText('999999', {}, 'the English text')).toBe('the English text')
		})

		it('shows the English text when the server sent no code', () => {
			expect(automationCodeText('', {}, 'the English text')).toBe('the English text')
			expect(automationCodeText(undefined, undefined, 'the English text')).toBe('the English text')
			expect(automationMessageText(undefined, 'the English text')).toBe('the English text')
		})

		it('splits the output into the sentences of the server and the data after them', () => {
			const output = 'set title to "Yeni"\nwarning: unknown placeholder {{x}} renders empty\nlog:\nhello'
			const result = automationOutput(output, [
				{code: 'set_title', params: {title: 'Yeni'}},
				{code: 'unknown_placeholder', params: {name: 'x'}},
			])
			expect(result.sentences).toEqual(['Başlık "Yeni" yapılacak', 'Bilinmeyen yer tutucu x boş görünecek'])
			expect(result.rest).toBe('log:\nhello')
		})

		it('shows an old output, recorded without codes, as it is', () => {
			expect(automationOutput('set title to "Old"', [])).toEqual({sentences: [], rest: 'set title to "Old"'})
			expect(automationOutput('set title to "Old"', undefined)).toEqual({sentences: [], rest: 'set title to "Old"'})
		})
	})
})
