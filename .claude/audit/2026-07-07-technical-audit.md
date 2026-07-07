# Технический аудит GameSaver — 2026-07-07

> Аудит по запросу: аудио-переключение, Bluetooth (поиск/подключение), бэкапы/восстановление,
> импорт библиотеки EA App. Код на теге **v0.11.0** (`9ca28cd`) — это последний релиз и текущий HEAD
> (доки в `CLAUDE.md` устарели: там «current release v0.10.8»). Всё, что ниже, проверено по коду
> v0.11.0. Правки НЕ вносились — это только находки и варианты решения.

Легенда: 🔴 риск потери данных · 🟠 функция врёт/не работает · 🟡 хрупко/UX · ✅ работает.

---

## 1. Аудио: переключение устройства в AudioPicker

**Ваш симптом:** в AudioPicker наушники (JBL Live / колонка Soundcore) уже подключены, показываются,
звук идёт — но переключить дефолт не выходит. Из Windows те же устройства подключаются одним кликом.

**Как устроено:**
- Список: `internal/audio/audio_windows.go:151-200` `List()` — MMDevice API, только **ACTIVE** endpoint'ы,
  дефолт берётся по роли `eMultimedia` (`getDefaultID:269-278`).
- Переключение: `SetDefault(deviceID)` `audio_windows.go:211-237` — недокументированный **IPolicyConfig**
  COM, `SetDefaultEndpoint(id, role)` на слоте 12 (Vista IID) или 13 (Win7+ IID), выбор в
  `acquirePolicyConfig:248-265` через string-sniffing `hr=0x80004002`.
- COM инициализируется **на каждый вызов** (`coInit:323-332` — `LockOSThread`+`CoInitializeEx`).
- UI: `frontend/src/components/shell/AudioPicker.tsx`, `applyDevice:81-93`.

**Находки (ранжированы):**

🟠 **F1.1 — цикл ролей возвращает ошибку после частичного успеха.** `SetDefault:230-236` ставит
`eConsole`, затем `eMultimedia`, и **возвращает ошибку на первом же сбое, хотя первая роль уже
применилась** (звук физически переключился). Для BT-endpoint, который только что стал активным,
второй `SetDefaultEndpoint` часто отдаёт транзиентный HRESULT. Наружу: красный тост «Сменить
устройство: … hr=0x…», хотя дефолт уже сменился. Признак в тексте тоста: `role 1`.

🟠 **F1.2 — catch не обновляет список.** `AudioPicker.tsx:90-92`: в ветке ошибки нет `refresh()`,
поэтому бейдж «по умолчанию» показывает старое состояние → усиливает ощущение «не переключилось».

🟡 **F1.3 — ранний выход на `isDefault`.** `applyDevice:83`: если устройство уже дефолт (Windows сам
назначил наушники дефолтом при подключении), клик по ним → `playBack()+onDone()`, модалка молча
закрывается без действия. Выглядит как «не переключается».

🟡 **F1.4 — COM на каждый вызов.** `coInit:323-332` на случайной горутине/потоке. Если поток уже в
STA → `RPC_E_CHANGED_MODE (0x80010106)`: первое нажатие падает, второе (чистый поток) работает.
В процессе других инициализаторов COM не найдено, поэтому маловероятно, но возможно.

**Что определит лог однозначно:** строка `SetDefaultEndpoint (role N) hr=0x…` → это F1.1 (и какой role/hr).

**Варианты решения (по приоритету):**
1. `SetDefault:230-236` — применять обе роли; считать успехом, если применилась ≥1; неудавшуюся роль
   повторить один раз через ~500 мс; `slog.Warn` на остаток. Совпадает с поведением диалога «Звук» Windows.
2. `AudioPicker.tsx:90` — в catch всё равно звать `refresh()`; если устройство фактически стало
   дефолтным — заменить ошибку на успех.
3. Убрать/смягчить ранний выход F1.3 или явно показывать «уже используется».
4. Долгосрочно: выделенный COM-worker (один поток, один `CoInitialize`) вместо инициализации на вызов —
   убирает класс F1.4 и стоимость init.

---

## 2. Bluetooth: поиск и подключение

**Ваш симптом:** `BluetoothSetServiceState 0x110B → «error 0, the operation completed successfully»`;
иногда «устройство недоступно», причём даже не пытается искать (никаких лоадеров). Из Windows —
подключение одним кликом.

**Как устроено (v0.11.0):**
- Кнопка Connect (A) → `app.go:1276 ConnectBluetoothDevice` → `bluetooth.ConnectEx`
  (`bluetooth_windows.go:408-464`).
- Discover: `app.go:1235 ScanBluetoothDevices` → `bluetooth.Discover` — один синхронный inquiry.
- Профили: включаются 6 GUID'ов (`:87-90`), A2DP sink `0x110B` первым.
- Радио-хендл: `firstRadio()` (fallback в NULL при отсутствии).

**Находки (ранжированы):**

🟠 **F2.1 — «Connect» это включение сервиса, а не подключение, и даёт ложный успех.**
`BluetoothSetServiceState(..., enable)` настраивает драйвер профиля. Для сопряжённого устройства, у
которого сервисы **уже включены** (нормальное состояние), вызов возвращает `ERROR_SUCCESS` мгновенно,
**не трогая радио**. `ConnectEx:445-458` → `anyOK=true` → status `connected` → зелёный тост
«Подключено» (`BluetoothPicker.tsx:149`), **даже если наушники выключены** или уже играют через
другой источник. Реальный ACL/A2DP-линк не инициируется. У классического Win32 API **нет** публичного
«подключи A2DP сейчас» — флип реально подключает только на свежем включении (сразу после pairing или
после нашего Disconnect). **Это ядро «переключить не выходит».**

🟠 **F2.2 — «устройство недоступно» без ретраев и лоадеров.** `ConnectEx:450-452`: на
`errDeviceNotConnected (1167)` **мгновенно** возвращает `unreachable`, без поллинга `fConnected` и без
повторов (подключение асинхронное, ему нужно время). UI показывает янтарный хинт «device not in range
or powered off» без спиннера. Совпадает с «даже не пытается искать, не показывает лоадеров».

🟠 **F2.3 — «error 0 / operation completed successfully» — латентный баг форматирования.**
`setServicesLocked:528` всё ещё форматирует `syscall.Errno(r)` (используется в `Disconnect`/`Pair`).
В коде `r==0` перехватывается раньше (`continue`), поэтому в v0.11.0 из этого места «code 0» появиться
не должно — **это прямо противоречит вашему наблюдению**. Возможные объяснения: (а) текст, который
вы помните, из версии до фикса v0.10.6; (б) сообщение приходит из другого пути (Discover/scan-error).
Точную текущую строку покажет лог — см. ниже. Само форматирование сырого `Errno(r)` — хрупко и в духе
инцидента v0.10.6, стоит заменить на явную расшифровку без `Errno`.

🟠 **F2.4 — только классический BR/EDR, BLE-устройства невидимы** (`:22-25`). Часть современной
периферии и геймпады сопрягаются по BLE и в списке не появятся никогда.

🟡 **F2.5 — discovery = один синхронный inquiry ~6.4 с** (`cTimeoutMultiplier=5`), без повтора,
результаты одним пакетом в конце. Windows сканирует непрерывно → «почти ничего не находит».

🟡 **F2.6 — первые результаты inquiry без имён** → «(unnamed)»; нет проверки состояния радио (BT off →
пустой скан без подсказки «включи Bluetooth»); `ConnectBluetoothDevice` — блокирующий Wails-промис до
~6×30 с без таймаута.

**Почему из Windows «одним кликом»:** оболочка Windows использует WinRT
(`Windows.Devices.Bluetooth`/`DeviceInformationPairing`) и KS-property реальный reconnect, а не
классический service-flip. Классический API просто не умеет то, что делает диалог Windows.

**Варианты решения (по приоритету):**
1. **Реальный connect/disconnect через `IKsControl` + `KSPROPSETID_BtAudio`
   (`KSPROPERTY_ONESHOT_RECONNECT` / `ONESHOT_DISCONNECT`)** на MMDevice-endpoint наушников — подход
   рабочих утилит (ToothTray). Вся COM/vtable-обвязка и enumeration **уже есть** в
   `internal/audio/audio_windows.go`; MAC↔endpoint сопоставляется по device path (содержит BT-адрес).
   Быстро, реально подключает, без churn драйвера. Минус: полу-документированный KS property set.
2. **BLE + живые имена + надёжный pairing: WinRT `DeviceWatcher` + `DeviceInformationPairing`.** Вы
   подтвердили готовность тянуть WinRT. Реализация без cgo: `github.com/saltosystems/winrt-go` либо
   ручной `RoActivateInstance`/`IInspectable`/HSTRING. Закрывает F2.4/F2.5/F2.6 и часть pairing разом.
   Минус: самый большой объём (WinRT async/HSTRING-плумбинг).
3. Пока WinRT нет — дёшево: не тостить «Подключено» по результату флипа, а поллить `fConnected`
   несколько секунд и рапортовать факт (чинит F2.1/F2.2); поднять `cTimeoutMultiplier` до ~8 и
   зациклить inquiry пока picker открыт (F2.5); заменить `Errno(r)` на явную расшифровку (F2.3);
   проверять `BluetoothFindFirstRadio` и показывать «радио выключено» (F2.6).

**Дрейф доков:** под перелопаченный BT в v0.11.0 нет decision-файла; `windows-syscalls.md` и decision
0028 описывают до-релизное состояние (кнопка «через Windows» теперь `ms-settings:bluetooth`,
`app.go:1290-1312`). По вашему же протоколу self-maintenance это надо задокументировать.

---

## 3. Бэкапы и восстановление

Базовый контракт соблюдён, но есть **два P1 риска потери данных** и **ноль тестов** в критичном пакете.

**✅ Работает корректно (проверено):** zip крэш-атомарен (`.tmp`→`os.Rename`, `engine.go:385,429`);
retention обрезает только после успешного бэкапа (`:265`); restore делает preRestore-снапшот
(`:314-317`), UI всегда шлёт `overwrite=true` (`GameDrawer.tsx:188`); zip-slip защита (`:449-455`);
дедуп по SHA-256 (`:374-381,199-202`); reconcile реимпортит orphan-zip и чистит мёртвые строки; restore
— extract-over, файлы из цели не удаляет.

**Находки:**

🔴 **F3.1 (P1) — single-file save-локации: нет preRestore-бэкапа И сломанный restore.** `engine.go:315`
preRestore под условием `util.IsDir(loc.Path)` — одиночный файл перезаписывается **без страховки**
(нарушение красной линии). Далее `unzipInto:445` делает `os.MkdirAll(dest)`, где `dest` — путь файла:
либо падает, либо создаёт директорию с именем файла и распаковывает в `save.dat\save.dat`.
Восстановление одиночных файлов, вероятно, никогда не работало.

🔴 **F3.2 (P1) — падение preRestore молча проглатывается.** `engine.go:316`: `_, _ = e.snapshotLocation(...)`.
Диск полон / нет прав / файл залочен → страховка тихо не создаётся, а restore идёт перезаписывать
живой сейв. Ровно тот сценарий, ради которого красная линия существует.

🟠 **F3.3 (P2) — залоченные/меняющиеся файлы молча выпадают из бэкапа** (`scanFiles:344-352`,
`writeZip:397-399` → `continue`). Манифест может перечислять файлы, которых нет в zip (scan и zip —
два раздельных прохода, TOCTOU); дедуп-хеш метит урезанное состояние как «последнее» и подавляет
следующий корректный бэкап. Ручной `BackupGame` (`app.go:518`), в отличие от watcher, **не имеет**
проверки «игра запущена».

🟠 **F3.4 (P2) — restore не атомарен и без проверки запущенной игры.** Распаковка файл-за-файлом
поверх живой папки; крэш/залоченный файл посередине → половина старого/половина нового.

🟠 **F3.5 (P2) — retention может удалить восстанавливаемый снапшот.** `Restore`→preRestore→
`applyRetention:265` может снести точку, чей `ArchivePath` взят на `:302`.

🟡 **F3.6 (P3):** junction/symlink-корни сейвов, вероятно, молча не бэкапятся (`WalkDir` не следует по
симлинкам, а миграции документированы как «copy + NTFS junction»); падение записи манифеста →
вечно-orphan zip (reconcile импортит только zip с манифестом); `AppVersion` в манифесте захардкожен
`"0.1.0"` (`engine.go:228`).

**Тесты:** во всём `internal/backup` — **ноль** `*_test.go`. Единственный тест репозитория —
`internal/bluetooth/bluetooth_windows_test.go`.

**Варианты решения (по риску):**
1. Прерывать restore при провале preRestore (`engine.go:314-317`) + снять `IsDir`-гейт.
2. Починить single-file restore в `unzipInto` (детектить file-typed dest, писать файл напрямую).
3. Исключить preRestore/preMigrate (или in-flight цель) из retention.
4. Считать пропущенные файлы в `scanFiles`/`writeZip` и падать/предупреждать при >0; добавить проверку
   запущенной игры в ручной `BackupGame`.
5. Restore через temp-папку + swap (квази-атомарно); блокировать restore при запущенной игре.
6. **Юнит-тесты** (вы подтвердили, что нужны) на чистые хелперы — highest-value: zip-slip кейсы,
   single-file корни, стабильность хеша, порядок retention, `computeContentHashFromManifest`.

---

## 4. Импорт библиотеки EA App — ПОЛНЫЙ список владения

**Ваша цель:** полный список владения (owned), не только установленные.

**Как сейчас (установленные):** `internal/scan/launchers/ea.go` — скрейп реестровых Uninstall-ключей,
фильтр по `Publisher`/`UninstallString`. Пробелы: пропускает игры со студийным издателем и без
уверенно найденного exe. LaunchURI/SourceAppID не ставятся.

**Онлайн-провайдер уже написан, но выключен:** `internal/stores/ea.go`. Комментарий `:40-48` гласит
«браузерного пути к облачной библиотеке нет». **Этот вывод устарел — причина провала найдена.**

🟠 **F4.1 — неправильный `client_id`.** Код хардкодит `EADOTCOM-WEB-SERVER` (`ea.go:28`) — веб-серверный
клиент, который gateway энтайтлментов отвергает («auth token not allowed for unknown client»). Три
живых проекта (Lutris `ea_app.py`, FriendsOfGalaxy `galaxy-integration-origin`, форк
`BellezaEmporium/galaxy-integration-ead`) используют **`ORIGIN_JS_SDK`** — публичный first-party
JS-SDK клиент, которому те же endpoint'ы доверяют, и он **захватывается из браузера** (в отличие от
`qrc://`-редиректа десктопного `JUNOPCCLIENT`).

**Рабочий флоу (высокая уверенность — подтверждён в трёх инструментах):**
1. Браузер/WebView2 на страницу логина EA; пользователь входит сам (2FA/captcha на нём).
2. Захватить cookies **`remid`** (долгоживущий) и **`sid`** с домена `accounts.ea.com`.
3. Тихий GET `accounts.ea.com/connect/auth?client_id=ORIGIN_JS_SDK&response_type=token&redirect_uri=nucleus:rest&prompt=none`
   с этими cookies → возвращает **JSON** `{access_token,...}` напрямую (без редиректа — вот почему
   `nucleus:rest` обходит проблему `qrc://`).
4. Обновление токена — повтор того же тихого запроса с `remid`.
5. Библиотека — один из двух путей:
   - Legacy REST: `ecommerce2/consolidatedentitlements/{pid}?machine_hash=1` + заголовок
     `Accept: application/vnd.origin.v3+json; x-cache/force-write` (у вас сейчас путь
     `entitlements/{pid}` — другой, `ea.go:32`).
   - **Актуальный (EA App): Juno GraphQL** `service-aggregation-layer.juno.ea.com/graphql`, query
     `ownedGameProducts` (storefronts:[EA], type:[DIGITAL_FULL_GAME,PACKAGED_FULL_GAME], platforms:[PC])
     — возвращает владение И названия одним запросом (замена per-offer каталог-лукапам `offerNames`,
     на которых вы тоже спотыкались).
6. Заголовки: `Authorization` + `AuthToken` + **`X-AuthToken`** — последнего у вас нет (`ea.go:157-159`).

**Правки в `internal/stores/ea.go` для оживления:**
- `eaClientID`: `EADOTCOM-WEB-SERVER` → `ORIGIN_JS_SDK`; `response_type=token`, `redirect_uri=nucleus:rest`,
  `prompt=none`.
- Добавить шаг захвата cookies `remid`/`sid`; хранить `remid` в DPAPI-vault как секрет (см. `secrets.md`).
- Заменить `FetchOwned` на `consolidatedentitlements` + Accept-заголовок ИЛИ (предпочтительно) Juno GraphQL.
- Добавить заголовок `X-AuthToken`.
- `LoginURL()` (`ea.go:40-48`) — вернуть рабочий URL и hint (текущий вывод «нет пути» неверен).
- Caveat: `accounts.ea.com` требует legacy TLS renegotiation (может понадобиться `tls.Config`).

**Осторожно (не проверено дословно):** WebFetch суммировал исходники, а не отдал raw-текст — точные
имена переменных/пути стоит сверить, прочитав три файла напрямую перед реализацией. Связка
`ORIGIN_JS_SDK` + `nucleus:rest` + `prompt=none` и Juno-endpoint подтверждаются во всех трёх — высокая
уверенность.

**Бонус — дешёвые апгрейды детекта установленных** (проверено по erri120/GameFinder wiki):
- `HKLM\SOFTWARE\WOW6432Node\Origin Games` — подключ на игру, имя = contentID, значение = путь.
- `__Installer\installerdata.xml` внутри папки игры — `<contentID>` + ключ реестра.
- Запуск-fallback `origin2://game/launch/?offerIds=<contentIDs>` (не `link2ea://`, оно не открывает app).
- (высокий риск сопровождения) расшифровка `%ProgramData%\EA Desktop\<hash>\IS` — эталон в GameFinder.

**Источники:** `github.com/erri120/GameFinder/wiki/EA-Desktop`; `lutris/lutris` (`ea_app.py`, issues
#4996/#5591); `FriendsOfGalaxy/galaxy-integration-origin`; `BellezaEmporium/galaxy-integration-ead`.

---

## Приложение — где взять лог

`slog` пишет JSON в `%LOCALAPPDATA%\GameSaver\logs\gamesaver.log` (`internal/logging/logging.go:19`,
`config.LogsDir()`). Нужны строки за момент воспроизведения:
- аудио: ищи `SetDefaultEndpoint (role N) hr=0x…` и `CoInitializeEx hr=…`;
- BT: ищи `SetServiceState … code …`, `bluetooth pair`, `bluetooth:scan-error`.
Они однозначно разведут гипотезы F1.1/F1.4 и подтвердят/опровергнут F2.3.
