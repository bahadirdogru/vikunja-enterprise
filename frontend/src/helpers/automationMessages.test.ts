import {afterAll, beforeAll, describe, expect, it} from 'vitest'

import {i18n, setLanguage} from '@/i18n'
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
			expect(automationCodeText('set_title', {title: 'Yeni'}, 'set title to "Yeni"')).toBe('Başlığı "Yeni" olarak ayarla')
		})

		it('frames the error of a script and keeps its text', () => {
			expect(automationCodeText('script_thrown', {message: 'ReferenceError: x is not defined'}, 'x'))
				.toBe('Betik hatası: ReferenceError: x is not defined')
		})

		it('looks a numeric code up as an API error', () => {
			expect(automationCodeText('4002', {}, 'The task does not exist.')).toBe('Görev bulunamadı.')
		})

		it('says the reason of a custom field error in Turkish too', () => {
			const text = automationCodeText('20005', {reason: 'expected a number', reason_code: 'expected_number'}, 'x')
			expect(text).toBe('Değer bu özel alan için geçerli değil: bir sayı bekleniyor')
		})

		it('keeps the English note when the failed condition has no sentence of its own', () => {
			expect(automationCodeText('note_conditions_fail', {code: 'a_code_from_a_newer_server'}, 'The conditions fail: boom')).toBe('The conditions fail: boom')
		})

		it('wraps the sentence of an action that had nothing to do', () => {
			expect(automationCodeText('nothing_to_do', {inner: 'mark_done'}, 'nothing to do: mark the task as done (already so)'))
				.toBe('Yapılacak bir şey yok, zaten öyle: Görevi tamamlandı olarak işaretle')
		})

		it('says the error of a failed condition inside the note', () => {
			expect(automationCodeText('note_conditions_fail', {code: 'script_thrown', message: 'boom'}, 'x'))
				.toBe('Koşullar başarısız oldu: Betik hatası: boom')
		})

		it('says what a step did in the imperative, which is right for a test and for the history', () => {
			expect(automationCodeText('set_title', {title: 'Yeni'})).toBe('Başlığı "Yeni" olarak ayarla')
			expect(automationCodeText('mark_done', {})).toBe('Görevi tamamlandı olarak işaretle')
			expect(automationCodeText('add_comment', {text: 'Görüldü'})).toBe('"Görüldü" yorumunu ekle')
			expect(automationCodeText('notify', {count: '2', message: 'Merhaba'})).toBe('2 kullanıcıya bildirim gönder: "Merhaba"')
		})

		it('never says a step in the future tense, because the history shows steps that ran', () => {
			const stepKeys = ['add_comment', 'add_label', 'assign_user', 'change_status', 'clear_custom_field', 'clear_due_date', 'clear_end_date',
				'clear_start_date', 'create_related_task', 'create_subtask', 'create_task', 'mark_done', 'mark_not_done', 'move_task', 'notify',
				'remove_label', 'request_approval', 'run_script', 'send_request', 'set_custom_field', 'set_description', 'set_due_date',
				'set_end_date', 'set_hex_color', 'set_percent_done', 'set_priority', 'set_start_date', 'set_title', 'unassign_user',
				'condition_fails', 'condition_holds']
			const future = stepKeys.filter(key => /(ecek|acak)(\s|\.|$)/u.test(i18n.global.t(`automation.messages.${key}`, {} as never)))
			expect(future).toEqual([])
		})

		it('shows a ratio of progress as a percentage and a limit in seconds as a duration', () => {
			expect(automationCodeText('set_percent_done', {value: '0.5'})).toBe('İlerlemeyi %50 olarak ayarla')
			expect(automationCodeText('wait_too_long', {max: '2592000'})).toBe('Bekleme, sunucunun izin verdiği 30 gün süresinden uzun.')
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
			expect(result.sentences).toEqual(['Başlığı "Yeni" olarak ayarla', 'Bilinmeyen yer tutucu x boş görünecek'])
			expect(result.rest).toBe('log:\nhello')
		})

		it('shows an old output, recorded without codes, as it is', () => {
			expect(automationOutput('set title to "Old"', [])).toEqual({sentences: [], rest: 'set title to "Old"'})
			expect(automationOutput('set title to "Old"', undefined)).toEqual({sentences: [], rest: 'set title to "Old"'})
		})
	})
})
