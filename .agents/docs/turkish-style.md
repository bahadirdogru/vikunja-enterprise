# Turkish localisation standard

This is the standard for every Turkish string in this fork: `frontend/src/i18n/lang/tr-TR.json` and `pkg/i18n/lang/tr-TR.json`. It exists so that two translators given the same English string write the same Turkish one. Where a rule leaves a choice, the choice is made here, with the reason.

The mechanical part is checked by `python3 .agents/skills/fork-i18n/check_parity.py` (see [Checks](#16-checks)). The rest is a reviewer's job: read the Turkish next to the English, in context.

## 1. Sources

Read for this standard (November 2026). Where they disagree, section 2 says what was chosen.

| Source | What it settles |
|---|---|
| Türk Dil Kurumu, *Yazım Kuralları*: [büyük harfler](https://tdk.gov.tr/icerik/yazim-kurallari/buyuk-harflerin-kullanildigi-yerler/), [kesme işareti](https://tdk.gov.tr/icerik/yazim-kurallari/kesme-isareti/), [kısaltmalar](https://tdk.gov.tr/icerik/yazim-kurallari/kisaltmalar/), [sayıların yazılışı](https://tdk.gov.tr/icerik/yazim-kurallari/sayilarin-yazilisi/), [bağlaç *da/de*](https://tdk.gov.tr/icerik/yazim-kurallari/baglac-olan-da-denin-yazilisi/), [bağlaç *ki*](https://tdk.gov.tr/icerik/yazim-kurallari/baglac-olan-kinin-yazilisi/), [soru eki *mı*](https://tdk.gov.tr/icerik/yazim-kurallari/soru-eki-mi-mi-mu-munun-yazilisi/), [noktalama](https://tdk.gov.tr/icerik/yazim-kurallari/noktalama-isaretleri-aciklamalar/), [simgeler](https://tdk.gov.tr/icerik/yazim-kurallari/simgeler/) | Orthography. Normative. |
| TDK *Güncel Türkçe Sözlük* (queried through `https://sozluk.gov.tr/gts?ara=…`) | Contested spellings (section 13). Normative. |
| [Microsoft Turkish Localization Style Guide](https://download.microsoft.com/download/c/7/e/c7e1f405-1a5a-418d-a56d-3a8c183cabd9/tur-tur-StyleGuide.pdf) | Voice, form of address, error message patterns, placeholders, punctuation in software. Names TDK as its normative reference. |
| [Microsoft Terminology Collection, Turkish TBX](https://download.microsoft.com/download/b/2/d/b2db7a7c-8d33-47f3-b2c1-ee5e6445cf45/MicrosoftTermCollection.zip) (via [Microsoft Terminology](https://learn.microsoft.com/en-us/globalization/reference/microsoft-terminology)) | Reference terminology for the glossary. |
| [Mozilla Türkiye, *Türkçe Yerelleştirme Yönergeleri*](https://www.mozilla.org.tr/turkce-yerellestirme-yonergeleri/) (read through the Internet Archive; the site refused connections) and [Mozilla L10n team page](https://wiki.mozilla.org/L10n:Teams:tr) | Casing of commands, suffixes on variables, ellipsis, `&`, em dash. |
| Firefox Turkish strings, [`mozilla-l10n/firefox-l10n`](https://github.com/mozilla-l10n/firefox-l10n) (`tr/browser/browser/tabbrowser.ftl`, `preferences/preferences.ftl`, `newtab/onboarding.ftl`) | What the Mozilla team does in practice: *siz*, sentence case, `… kapatılsın mı?`, no plural after numbers. |
| GNOME Turkish team: [team page](https://l10n.gnome.org/teams/tr/), [Nautilus `tr.po`](https://l10n.gnome.org/POT/nautilus.main/nautilus.main.tr.po) | *siz* in messages, bare imperative on buttons, `nplurals=1`. GNOME has no written Turkish style guide; its practice is the evidence. |
| LibreOffice Turkish strings, [`translations/source/tr/sfx2/messages.po`](https://git.libreoffice.org/translations/+/refs/heads/master/source/tr/sfx2/messages.po) | Same practice as GNOME. |
| Upstream Vikunja's Crowdin Turkish, `git show upstream/main:frontend/src/i18n/lang/tr-TR.json` and commit `4e2a89939` (October 2026) | The register and terms of the 1611 upstream keys. That commit moved upstream's Turkish from *siz* to *sen*. |

No written Turkish guide was found for KDE (only the `kde-l10n-tr` list) or for LibreOffice; their files were used as evidence of practice.

## 2. Where the sources disagree

| Question | Sources | Decision and reason |
|---|---|---|
| *sen* or *siz* in sentences | Microsoft: *siz* in general, *sen* for young audiences and marketing. Firefox, GNOME, LibreOffice: *siz*. Upstream Vikunja: *sen* since October 2026, deliberately. | **The application uses *sen*; anything shown to people outside the instance uses *siz*.** See section 3. The upstream community chose *sen* for the 1611 upstream keys a month before this standard; switching the fork to *siz* means rewriting several hundred upstream strings and fighting every Crowdin sync. The owner may veto (section 3.4 states the cost). |
| Casing of window and dialog titles | Mozilla: Title Case for window titles, sentence case for commands. GNOME: follows English header capitalisation. Microsoft: only proper nouns and the first word are capitalised; follow the source but keep similar items consistent. | **Sentence case everywhere**, titles included. One rule, no list of exceptions; the English source in this repository is mostly sentence case already. |
| Apostrophe and quotation marks | TDK and Mozilla print the curly `’` and `“ ”`. Microsoft: straight quotation marks in software. | **Straight `'` and `"`**. Every existing string, upstream's included, uses them; mixing both would be the worst outcome. |
| Ellipsis | Microsoft: three points. Mozilla: the single character `…`. TDK: "üç nokta", never two. | **`…` (U+2026)**, as upstream's English and Turkish already do. |
| Dictionary | Mozilla: Dil Derneği. Microsoft: TDK first. | **TDK *Güncel Türkçe Sözlük***. |
| *password* | Microsoft: *parola*. TDK: both *şifre* and *parola*. Upstream: *şifre* in 65 strings. | ***şifre***. Correct, and established in the product. |
| *token* | Microsoft: *belirteç*. Upstream: *anahtar*. | ***anahtar*** (*API anahtarı*). Established upstream; users understand it. |
| *webhook* | Microsoft: *web kancası*. Upstream: *webhook*. | ***webhook***, untranslated. |
| *due date* | Microsoft: *son tarih*. Upstream: *bitiş tarihi* for due date, *sonlanma tarihi* for end date, consistently. | Keep upstream's pair. Changing it touches about 40 upstream strings. Owner to confirm. |

## 3. Form of address and mood

### 3.1 The scheme

| Element | Form | Example |
|---|---|---|
| Buttons, menu commands, links that do something, toolbar items | Bare imperative (2nd person singular), no person ending. Identical in every product read, *siz* products included. | *Kaydet*, *Sil*, *Görevi kapat*, *Bağlantıyı kopyala* |
| Checkbox and toggle labels that describe a setting | Noun phrase or imperative as in the English; first person when the user tells the app what to do (Microsoft 4.1.13). | *Görev hatırlatıcılarını e-postayla gönder*, *Gönderdiğim yorumları bana da gönder* |
| Field labels, column headers, section headings, page titles | Noun phrase. No verb, no person. | *Bitiş tarihi*, *Onaylayanlar*, *Yeni kural* |
| Instructions, hints, help text, placeholders | *sen* in the application, *siz* on outsider pages. | *Bir proje seç.* / *Formu doldurun.* |
| Questions in confirmations | Follow the English structure. *Are you sure you want to X?* → *X… istediğinden emin misin?* *Delete X?* → *X silinsin mi?* | *Bu kuralı silmek istediğinden emin misin?* / *Bu pano silinsin mi?* |
| Error messages | Impersonal: what failed, in the passive (Microsoft 5.5.2); what to do, in *sen* (or *siz* outside). | *Kural kaydedilemedi. Tekrar dene.* |
| Success and status messages | Impersonal passive, past tense. | *Kural kaydedildi.* |
| Progress | Impersonal passive, present continuous, `…`. | *Kaydediliyor…* |
| Empty states | Impersonal statement, then an optional instruction in *sen*. | *Henüz kural yok. İlk kuralını oluştur.* |
| E-mails and notifications to account holders | *sen*, as upstream's `pkg/i18n` strings. | *Merhaba %[1]s,* … *Karar vermen gerekmiyor.* |
| Anything shown to people outside the instance | *siz*: the public form page (`forms.public.*`), e-mails sent to form submitters, embed and help texts written for outsiders. | *Yanıtınız gönderildi.* *Bu alanı boş bırakın.* |

The instruction verb in *siz* is the plain plural imperative (*girin*, *seçin*), never the extra-polite *giriniz*, *seçiniz* (Microsoft 4.1.16: too authoritative).

Buttons stay bare imperative on outsider pages too: *Gönder*, not *Gönderin*.

### 3.2 *sen* forms to use

Imperative: *seç*, *gir*, *dene*, *ekle*. Possessive: *hesabın*, *şifreni*, *görevlerin*. Ability: *ekleyebilirsin*. Question: *emin misin?*, *devam etmek istiyor musun?* Obligation: *seçmelisin*, or impersonal *seçilmesi gerekir* when blaming would sound harsh.

### 3.3 What never to do

- Mix *sen* and *siz* in one string, or within one screen.
- Use *siz* forms in an application string (`-ınız`, `-in` imperatives, `misiniz`). The check flags them outside `forms.public.*`.
- Use *sen* forms in `forms.public.*`.
- Address the user in the third person (*kullanıcı şunu yapmalı*) when the English says *you*.

### 3.4 If the owner chooses *siz*

The switch would touch every instruction, question, error with a remedy, and second-person possessive, in upstream keys as well as fork keys. The audit of November 2026 counted 552 strings with *sen* forms: 394 upstream keys and 158 fork keys (out of 3103). It would also mean converting every Turkish string that upstream's Crowdin delivers after each merge. Buttons, labels, headings and status messages would not change.

## 4. Capitalisation

- **Sentence case** in every string: buttons, menu items, tabs, headings, page and dialog titles, labels, column headers, options. Only the first word and proper nouns are capitalised. *Yeni gizli anahtar*, not *Yeni Gizli Anahtar*. This holds even where the English source uses Title Case.
- Proper nouns and product names keep their own spelling: *Vikunja*, *Kanban*, *Gantt*, *CalDAV*, *Todoist*, *Microsoft To Do*, *TOTP*, *OpenID Connect*.
- Names of the app's own views and modules are common nouns in Turkish and are lower case mid-sentence: *liste görünümü*, *pano*, *iş akışı*, *özel alanlar*. Exception: *Kanban* and *Gantt* (proper names of methods).
- After a colon: capital only if a full sentence follows (TDK; Microsoft 4.1.14). *Gerekçe: kapsam dışı* / *Not: Bu işlem geri alınamaz.*
- Days and months are lower case in general (*her pazartesi*, *eylülde*) and capitalised in a specific date (*29 Ekim 2026 Perşembe*). The date formatter handles generated dates.
- Dotted and dotless i: upper case of *i* is *İ*, of *ı* is *I*. *İptal*, *İş akışı*, *Içerik* is wrong (*İçerik*). Never type a capitalised word by changing only the first byte of an ASCII word. Do not rely on CSS `text-transform: uppercase` for Turkish text unless the page sets `lang="tr"`.
- ALL CAPS only where the English uses it as a design element (Microsoft 4.1.5), and then typed correctly (*İPTAL*).

## 5. Punctuation

- **Labels, buttons, headings, options, table headers, short tooltips: no final period.** Full sentences end with a period, even when short (*Kural kaydedildi.*). Mirror the English: if the English string is a sentence with a period, the Turkish one is too; if the English has none, neither has the Turkish, unless the Turkish string became a full sentence where the English is a fragment (then it ends with a period).
- **Question mark** where the Turkish is a question, whatever the English does. *Needed by* → *Ne zamana kadar gerekli?* is a question.
- **Exclamation mark**: rarer than in English (Microsoft 4.1.14). Keep it in greetings and real warnings (*Bu işlem geri alınamaz!*); drop it in success messages (*Kaydedildi.*, not *Kaydedildi!*).
- **Ellipsis `…`**, no space before it. Use it where the English has it: on commands that open a further step (*Yeni gizli anahtar…*), on progress (*Yükleniyor…*), on placeholder prompts (*Ara…*).
- **Quotation marks**: straight `"`. Names the user gave to things are quoted when the English quotes them (*"{title}" kuralı*). Suffixes after a closing quotation mark take no apostrophe (TDK): *"Bit Palas"ını*. In this repository the rule is stricter: no suffix after a quoted placeholder at all (section 6).
- **Apostrophe**: straight `'`, only where TDK requires one (section 7). Never between a common noun and its suffix (*webhook'lar* is right because *webhook* is a foreign word read differently from its spelling; *görev'i* is wrong).
- **Colon**: before an example, a value or an explanation (*Bitiş: {at}*, *Gerekçe: {reason}*). A label that introduces a field keeps its colon only if the English has one.
- **Dashes**: an English em dash used for an aside becomes a colon, a comma or parentheses (Microsoft, Mozilla). The en dash and hyphen in ranges take no spaces: *09.30-10.30*, *3-5*. Keep the arrow `→` where the English uses it as a separator.
- **`&`** becomes *ve* in running text (Microsoft, Mozilla), except in code, keys and names.
- **Slash** for alternatives in short labels only (*aç/kapat*). In sentences write *veya* / *ya da*.
- **Parentheses**: no space inside. A suffix goes on the word before the parenthesis (*Gerekli sürümü (2.0 ve sonrası) yükle*). A full sentence inside parentheses has its period inside.
- **Lists**: bullet items that are sentences start with a capital and end with a period; fragments start with a capital and have no period (Microsoft).
- **Spaces**: one space after a sentence. No double spaces, no leading or trailing spaces unless the English has them.

## 6. Placeholders and suffixes

`{name}`, `{count}`, `%s`, `%[1]s` are replaced at run time with text the translator cannot see. A Turkish suffix attached to it is right for some values and wrong for others: vowel harmony (*Ali'ye*, *Ayşe'ye*, *Emre'ye*), consonant softening, buffer letters, and the reading of abbreviations all depend on the value.

**Rule: no suffix ever attaches to a placeholder.** Not with an apostrophe, not without one, not after a closing quotation mark. Restructure the sentence so the placeholder stands in the nominative, followed by a word that carries the suffix. This is Mozilla's rule (*%S tarayıcısından çık*, not *%S'tan çık*) and Microsoft's (*%s silinemiyor*: content unknown, no suffix allowed).

Patterns:

| English | Wrong | Right |
|---|---|---|
| Delete {name} | {name}'i sil | {name} öğesini sil · *"{name}" kuralını sil* |
| Shared by {user} | {user}'ın paylaştığı | Paylaşan: {user} |
| Assigned to {user} | {user}'a atandı | Atanan kişi: {user} · *{user} kullanıcısına atandı* |
| in {project} | {project}'de | {project} projesinde |
| from {service} | {service}'den | {service} hizmetinden · *{service} üzerinden* |
| You will be sent to {host} | {host}'a yönlendirileceksiniz | {host} adresine yönlendirileceksiniz |
| {name} is larger than {max} | {name}, {max}'tan büyük | {name} dosyası {max} sınırından büyük |
| until {date} | {date}'e kadar | {date} tarihine kadar |
| {n} minutes ago | — | {n} dakika önce (no suffix needed) |
| Action {n} | — | *Eylem {n}* or *{n}. eylem* (ordinal dot directly after the placeholder is allowed: it is not a suffix) |

Carrier words to use: *adlı* (named), *kullanıcısı/kullanıcısına* (user), *projesi/projesinde* (project), *görevi/görevinde* (task), *kuralı* (rule), *formu* (form), *dosyası* (file), *adresi/adresine* (address), *tarihi/tarihine* (date), *hizmeti/hizmetinden* (service), *öğesi/öğesini* (anything else). Or move the placeholder to after a colon: *Paylaşan: {user}*.

Placeholders are copied exactly: same names, same count, same `%[n]s` numbers. Their order may change when Turkish word order needs it; named and numbered placeholders allow that, plain `%s` does not (keep the order of plain `%s`).

## 7. Apostrophes on names, abbreviations, code

TDK:

- **Proper names** (people, places, products, brands, services) take an apostrophe before a suffix: *Vikunja'da*, *Vikunja'ya*, *Todoist'ten*, *Gravatar'ı*, *Microsoft To Do'dan*.
- **Institution and company names** used as names of institutions take no apostrophe (TDK 3). Rare in this product; treat a product name as a proper name.
- **Abbreviations written in capitals** take an apostrophe; the suffix follows the Turkish reading of the **last letter**, unless the abbreviation is read as a word (TDK *Kısaltmalar*). Microsoft adds: widely known foreign abbreviations may follow their English reading. Decision for this product (Turkish letter names, as TDK):

| Abbreviation | Reading | Forms |
|---|---|---|
| API | a-pe-i | API'si, API'ye, API'yi, API'den |
| URL | u-re-le | URL'si, URL'ye, URL'yi, URL'den |
| ID | i-de | ID'si, ID'ye, ID'yi — prefer *kimlik* (*kimliği*) in prose |
| CSV | ce-se-ve | CSV'si, CSV'ye, CSV'den |
| HTTP, HTTPS | ...-pe, ...-se | HTTP'ye, HTTPS'ye |
| JSON | read as a word, *ceysın* | JSON'u, JSON'a, JSON'da |
| TOTP | te-o-te-pe | TOTP'yi, TOTP'ye |
| MCP | me-ce-pe | MCP'ye, MCP'yi |
| SMTP | ...-pe | SMTP'ye |
| HTML | ...-le | HTML'si, HTML'ye |
| CalDAV | word, *kaldav* | CalDAV'a, CalDAV'ın |
| OIDC, OpenID | o-i-de-ce / word | OIDC'ye; OpenID'ye |
| PDF | pe-de-fe | PDF'si, PDF'ye |

- **Foreign common nouns kept in English** (*webhook*, *cron*, *Markdown*) take an apostrophe and follow the pronunciation: *webhook'u*, *webhook'a*, *webhook'lar*; *cron'un*. Prefer a carrier word where it reads better: *cron ifadesi*.
- **Lower-case unit abbreviations** follow the spoken word: *km'den*, *MB'lık* (*megabayt*) — prefer *MB boyutunda*.
- **Numbers** take an apostrophe: *2026'da*, *3'ten*, *%50'si*. Prefer a structure without suffix on a number where the number is a placeholder (section 6).
- **File names, code, config keys, header names, field paths** never take a suffix. Use a carrier word: *`config.yml` dosyasında*, *`X-Vikunja-Signature` başlığı*, *`event.payload` alanında*.

## 8. Numbers and plurals

- **No plural suffix after a number or a numeric placeholder** (TDK; Microsoft 5.5.2: *%d öğe silinemiyor*): *3 görev*, *{count} görev*, *birkaç görev*, *çok görev*. Also not after *her*, *kaç*, *birçok*.
- **vue-i18n `a | b` and `a | b | c`**: keep exactly as many slots as the English, because the code picks the slot by index (two slots: `count === 1 ? 0 : 1`; three slots: `0`, `1`, more). Turkish needs:
  - the number shown in the slot: the same singular word in every slot — `{count} yanıt | {count} yanıt`;
  - no number shown: singular in the first slot, plural in the second — `görevi sil | görevleri sil`, `kullanıcı | kullanıcılar`;
  - three-slot zero form: a natural zero phrase — `görev yok | {count} görev | {count} görev`.
- **Spelled-out numbers**: in running text Turkish spells out small numbers when the English does (*iki durum*). With a placeholder there is nothing to decide.
- **Ordinals**: *3.* or *3'üncü*, never *3.'üncü* (TDK). After a placeholder use the dot: *{n}. adım*.
- **Distributives** in words: *ikişer*, not *2'şer* (TDK).
- *1 tanesi* is translationese: *Bunlardan biri…*

## 9. Dates, times, numbers, units

Generated values are formatted by `Intl` and dayjs with the Turkish locale. Do not format inside a string. Know what the formatter produces, so the surrounding words fit:

- Decimal comma, thousands dot: *1.250,5* (TDK).
- Time with a dot or colon as the formatter outputs; in hand-written Turkish text use the TDK form *14.30*; a suffix needs an apostrophe: *14.30'da*.
- Date: *25 Kasım 2026*, or *25.11.2026*.
- **Percent sign before the number, no space**: *%50* (TDK; Microsoft 5.5.2). With a placeholder: `%{percent}`, not `{percent}%`. In `pkg` format strings a literal percent is `%%`; it goes before the verb: `%%%d`.
- Units after the number, with a space, lower case: *25 MB*, *10 dakika*. Duration words come from `time.units.*` and need no plural.
- Currency: *100 TL*, *₺100* as the formatter writes it. Currency names are lower case (*dolar*, *avro*).
- Relative time (*3 dakika önce*, *2 gün sonra*) comes from dayjs. A sentence around `{ago}` must accept a value like *3 dakika önce*: *Bu anahtarın süresi {ago} doldu.*

## 10. Sentence patterns

Use these, so the same English gives the same Turkish.

| English | Turkish |
|---|---|
| X was saved / created / deleted / updated (successfully). | X kaydedildi. / oluşturuldu. / silindi. / güncellendi. Drop *successfully* (*başarıyla*): the message already says it worked. Upstream keys that keep *başarıyla* are tolerated. |
| Could not / Cannot / Failed to X. | X yapılamadı. (*Kural kaydedilemedi.*) For a state that persists: *yapılamıyor*. (Microsoft 5.5.2) |
| X could not be found. / X does not exist. | X bulunamadı. / Bu X yok. |
| X is not valid. | X geçerli değil. |
| An error occurred. | Bir hata oluştu. Never *Bir hata meydana geldi.* Prefer saying what failed. |
| Something went wrong. | Bir sorun oluştu. |
| Try again (later). | Tekrar dene. / Daha sonra tekrar dene. |
| Please X. | X. Keep *Lütfen* only when the user is asked to wait or to do something inconvenient (Microsoft 2.1.2), at most once per string. |
| Click X to Y. / Click here to Y. | Y için X'e tıkla → prefer *Y için X'i seç*, or name the action: *Bitiş tarihi ekle*. *Tıkla* fails for keyboard and touch users (Microsoft 3.2). Upstream keys are tolerated. |
| Are you sure you want to X? | X… istediğinden emin misin? |
| X? (Delete this rule?) | Bu kural silinsin mi? |
| This cannot be undone. | Bu işlem geri alınamaz. |
| No X yet. | Henüz X yok. |
| Loading X… | X yükleniyor… |
| X (optional) / X (required) | X (isteğe bağlı) / X (zorunlu) |
| Enter a X. (validation) | Bir X gir. / Geçerli bir X gir. |
| X is required. | X zorunlu. / X gerekli. (*zorunlu* for fields, *gerekli* for prerequisites) |
| You do not have permission to X. | X için yetkin yok. |
| Only X can Y. | Y işlemini yalnızca X yapabilir. / *X'i yalnızca proje yöneticileri düzenleyebilir.* |
| N of M | {n} / {max} |
| X, Y and Z | X, Y ve Z (no comma before *ve*) |
| X or Y | X veya Y; between clauses *ya da*. Never *ve/veya*: write *X, Y ya da ikisi*. |

## 11. Glossary

One Turkish term per concept. Before inventing a term, search both `tr-TR.json` files for the English word. "Rejected" lists what not to use and why. "Upstream" means the term in upstream's Crowdin Turkish.

### Core (upstream concepts)

| English | Turkish | Rejected and why |
|---|---|---|
| task | görev | iş (ambiguous) |
| project / sub-project / parent project | proje / alt proje / üst proje | |
| label | etiket | |
| bucket (Kanban) | sütun | kova (calque), grup |
| view (noun) / View (command) | görünüm / Görüntüle | görüntü (= image) |
| list / table / Kanban / Gantt view | liste / tablo / Kanban / Gantt görünümü | |
| filter / saved filter | filtre / kayıtlı filtre | süzgeç (in TDK, unused in software) |
| assignee / assignees / assign | atanan kişi / atananlar / ata | sorumlu (different concept) |
| due date / start date / end date | bitiş tarihi / başlangıç tarihi / sonlanma tarihi | son tarih (Microsoft; upstream uses *bitiş tarihi*, see section 2). Never *bitiş tarihi* for *end date*. |
| reminder (on a task) / reminder (e-mail) | hatırlatıcı / hatırlatma | anımsatıcı (Microsoft; upstream uses *hatırlatıcı*) |
| share / sharing | paylaş / paylaşım | |
| link share | paylaşım bağlantısı | bağlantı paylaşımı |
| team | ekip | takım (upstream uses *ekip*) |
| member / owner / admin | üye / sahip / yönetici | |
| namespace | (does not exist any more; use *üst proje*) | ad alanı |
| user / person | kullanıcı / kişi | |
| bot | bot | robot |
| session | oturum | |
| sign in, log in / sign out, log out / sign up, register | giriş yap / çıkış yap / kayıt ol | oturum aç (Microsoft; upstream uses *giriş yap*) |
| password | şifre | parola (section 2) |
| token (API, CalDAV, reset) | anahtar (API anahtarı, CalDAV anahtarı) | belirteç (Microsoft), jeton, token |
| API | API | uygulama programlama arayüzü (too long) |
| webhook | webhook (*webhook'u*, *webhook'a*, *webhook'lar*) | web kancası (Microsoft; not used by upstream or the Turkish developer community) |
| e-mail | e-posta | mail, eposta |
| settings / preferences | ayarlar / tercihler | |
| comment / attachment | yorum / ek (*dosya eki* where *ek* is ambiguous) | |
| subtask | alt görev | |
| favourite | favori | sık kullanılan (Microsoft; upstream uses *favori*) |
| archive / unarchive | arşivle / arşivden çıkar | |
| duplicate (create a copy of a thing) | kopyala | çoğalt (Microsoft; upstream uses *kopyala* for projects and tasks, and the product must use one verb) |
| copy (to clipboard) | kopyala | |
| template / preset | şablon / hazır şablon | |
| timer (time tracking) | sayaç | zamanlayıcı (upstream uses *sayaç*); time entry → *zaman kaydı* |
| overview | genel bakış | |
| trash | çöp kutusu | |
| import / export | içe aktar / dışa aktar | içeri aktar (Microsoft) |
| upload / download | yükle / indir | karşıya yükle |
| preview | ön izleme | önizleme (not in TDK, see section 13) |
| done / mark as done | tamamlandı / tamamlandı olarak işaretle | bitti |
| required / optional | zorunlu / isteğe bağlı | opsiyonel, gerekli (for fields) |
| checkbox / option / select / multi-select | onay kutusu / seçenek / seçim / çoklu seçim | |
| retry / try again | yeniden dene / tekrar dene | |
| Done (button that ends an editing mode) | Tamam | Bitti |
| display (options for how something is shown) | gösterim | görünüm (= view) |
| Group by / Sort by / Split by (field labels) | Gruplama ölçütü / Sıralama ölçütü / Ayırma ölçütü | imperatives (*Grupla*, *Sırala*) on a label |

### Fork modules

| English | Turkish | Rejected and why |
|---|---|---|
| custom field / field | özel alan / alan | |
| workflow | iş akışı | |
| status (of a workflow) | durum | statü |
| transition | geçiş | aktarım |
| initial status | başlangıç durumu | |
| approval / approval request / request approval | onay / onay talebi / onay iste | |
| approver / requester | onaylayan / talep eden | onaylayıcı (not in TDK), onaycı |
| approve / reject / cancel (a request) | onayla / reddet / iptal et | |
| outcome / reason | sonuç / gerekçe | neden (use for *cause*, not for a stated reason) |
| ad-hoc approval | serbest onay | anlık onay |
| dashboard | pano | gösterge paneli (long), kontrol paneli (= control panel) — confirmed |
| widget | bileşen | pencere öğesi (Microsoft; desktop widgets), widget — confirmed |
| report | rapor | |
| chart | grafik | çizelge (= schedule) |
| form | form | |
| submission (a stored answer to a form) | yanıt | gönderim (Microsoft uses it for app store submissions), başvuru (implies an application) — confirmed |
| to submit / submitting | göndermek / gönderme | |
| automation | otomasyon | |
| rule | kural | |
| trigger | tetikleyici | tetik |
| condition | koşul | şart |
| action (step of a rule) | eylem | işlem (reserved for generic *operation* and the *Actions* menu of upstream), aksiyon — confirmed |
| Actions (generic menu) / operation | İşlemler / işlem | |
| script | betik | script, komut dosyası |
| run (one execution of a rule) / run history | çalıştırma / çalıştırma geçmişi | çalışma |
| test, dry run | deneme (*Kuralı dene*) | test, prova, kuru çalıştırma — confirmed |
| secret | gizli anahtar | gizli dizi (Microsoft, not understood by users), sır |
| token in a webhook address | erişim anahtarı | token, jeton (keeps it apart from *gizli anahtar* and *imza anahtarı*) |
| signing secret / signature | imza anahtarı / imza | |
| rate limit | istek sınırı; for rule runs *çalıştırma sınırı* | hız sınırı (calque) |
| payload | veri | yük (calque) |
| event | olay | etkinlik (= calendar event) |
| header (HTTP) | başlık; header names stay as they are | üst bilgi (document header) |
| schedule / cron expression | zamanlama / cron ifadesi | |
| queue / queued | kuyruk / kuyruğa alındı | |
| limit | sınır | limit |
| incoming webhook | gelen webhook | |
| branding | marka görünümü | markalama (calque) |

Words kept in English: *Vikunja*, *Kanban*, *Gantt*, *webhook*, *cron*, *Markdown*, *CalDAV*, *TOTP*, *OpenID Connect*, *JSON*, *API*, *URL*, *HTTP(S)*, *CSV*, product names of other services. They take suffixes as in section 7.

## 12. Common errors to avoid

- **Translationese.** Passive where Turkish is active (*X tarafından yapılır* when *X yapar* works); *için tıklayın*; *bir* for every English *a*; English word order for adjectives (*fotoğraflı aylık takvim*, Microsoft 4.1.3); possessives for every English *your* (*Hesabınızı ayarlayın* → *Hesabı ayarla* is often enough, Microsoft 4.1.3); *-mekte/-makta* for simple present.
- **Lütfen** in every instruction (section 10).
- **ve/veya** (section 10). **Doubled plurals**: *tüm görevlerler*, *birçok görevler*, *3 görevler*.
- **Inconsistent tense in status messages.** Done: *-ildi* (*kaydedildi*). In progress: *-iliyor…* (*kaydediliyor…*). A state: *-ili*/*-dir* (*arşivlenmiş*, *kapalı*). Do not mix *silindi* and *silinmiştir* for the same kind of message; prefer the shorter *silindi*.
- **"Hata oluştu" without content.** Say what failed: *Kural kaydedilemedi.*
- **Importing English pronouns.** Turkish has no grammatical gender; do not add *o*, *onun* where English needs *he/she/they*. A singular *they* is just the verb ending.
- **ASCII substitutes** for *ç ğ ı İ ö ş ü*, and the circumflex where TDK keeps it (*hâlâ*). The check finds words that also occur with diacritics elsewhere.
- **Dotted and dotless i** in capitals (section 4).
- **Suffix on a placeholder** (section 6). **Plural after a number** (section 8). **Percent sign after the number** (section 9).
- ***de/da* and *ki* written wrongly.** The conjunction *de/da* is written separately and never as *te/ta* (*Ayşe de geldi*, *gidip de*); the locative is attached (*evde*, *Vikunja'da*). The conjunction *ki* is separate (*demek ki*); fixed exceptions are attached: *belki, çünkü, hâlbuki, mademki, meğerki, oysaki, sanki*. The adjective suffix *-ki* is attached (*yandaki*, *dünkü*).
- **Question particle *mı*** is written separately and follows vowel harmony; endings attach to it: *Kaydedilsin mi?*, *emin misin?*, *Siliniyor mu?* (TDK).
- **Untranslated English** left in prose, and English capitalisation copied (*Bitiş Tarihi*).
- **Upstream-sounding but wrong terms** for fork concepts (*işlem* for an automation action, *bitiş tarihi* for *end date*).
- **Different Turkish for the same English** across keys without a reason.

## 13. Contested spellings (TDK *Güncel Türkçe Sözlük*, checked November 2026)

| Write | Not |
|---|---|
| e-posta | eposta, e-mail |
| ön izleme | önizleme |
| veri tabanı | veritabanı |
| iş birliği | işbirliği |
| çevrim içi, çevrim dışı | çevrimiçi, çevrimdışı |
| art arda | ard arda, arda arda |
| ön ek, son ek | önek, sonek |
| zaman aşımı | zamanaşımı |
| hâlâ (still) | hala (= aunt) |
| her şey, hiçbir, birkaç, birçok, hiçbiri | herşey, hiç bir, bir kaç, bir çok |
| yalnızca | yanlızca |
| yanlış | yalnış |
| devre dışı | devredışı |
| kayıt, kaydı, kaydet | kayıdı |
| ad, adı | isim is acceptable, prefer *ad* for *name* of a thing |
| dahi, dâhil | dahil (TDK: *dâhil*) |

Words not in TDK but established in software are allowed: *varsayılan*, *ana sayfa*, *alt görev*, *kullanıcı adı*, *zamanlayıcı*, *tetikleyici*, *iş akışı*, *dışa aktar*, *içe aktar*.

## 14. Never translate or alter

- Placeholders: `{name}`, `{count}`, `%s`, `%d`, `%[1]s`, `%%`.
- HTML and markup inside strings: `<a>`, `<strong>`, `<code>`, attributes, `{'@'}`-style escapes for vue-i18n, `\n`.
- Keyboard keys as the key cap reads: *Enter*, *Esc*, *Shift*, *Ctrl*, *Tab*. Key names in prose take suffixes by pronunciation: *Enter'a bas*, *Esc'ye bas*.
- HTTP header names, config keys, field paths, JSON keys, query syntax and operators (`&&`, `in`, `done = false`), cron syntax, code, file names, URLs, e-mail addresses.
- Product names.

Translate the words around them, and use a carrier word instead of a suffix (section 7).

## 15. Upstream keys

1611 frontend keys and 113 backend keys come from upstream Vikunja; their Turkish arrives through upstream's Crowdin sync. Every value the fork changes there is a line that can conflict at the next merge (see the `fork-merge-upstream` skill).

- For **fork keys** (absent from `upstream/main:…/en.json`), every rule in this standard applies.
- For **upstream keys**, change the Turkish only for an **error**: wrong or missing meaning, a term that contradicts the glossary in a way the user can see (*bitiş tarihi* for an end date), TDK misspellings, missing diacritics, wrong suffixes or grammar, a broken placeholder, untranslated text, or *siz* in the application. Do not change upstream keys for style alone: casing copied from English Title Case, *başarıyla*, *lütfen*, *tıkla*, a wording the reviewer merely prefers. Those are tolerated in upstream keys and forbidden in fork keys.
- Where the fork has already reworded an upstream key and upstream's Turkish is acceptable under the rule above, go back to upstream's Turkish.
- Good corrections of upstream keys belong in upstream's Crowdin as well; note them for the owner.

## 16. Checks

`python3 .agents/skills/fork-i18n/check_parity.py` runs the key and placeholder parity checks and these style checks on `tr-TR.json`:

- markup parity with the English;
- suffix attached to a placeholder;
- plural suffix after a number or numeric placeholder;
- percent sign after a number;
- three dots instead of `…`;
- *ve/veya*;
- double spaces, leading or trailing whitespace not in the English;
- ASCII substitutes (a word that appears elsewhere in the file with Turkish letters);
- capital *I* where the word exists with *İ*;
- rejected glossary terms and TDK misspellings (section 11, 13);
- *siz* forms in application strings, *sen* forms in `forms.public.*`;
- Title Case in fork keys.

Justified exceptions go into `.agents/skills/fork-i18n/tr_allow.txt`, one `check-name key` per line with a comment saying why.
