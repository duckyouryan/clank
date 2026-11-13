package main

// RU — строки интерфейса (сгенерировано из прежних таблиц, правится руками).
var RU = Msgs{
	Usage: `clank — минималистичный CLI к OpenAI-совместимому API

использование:
  clank <вопрос>                        задать вопрос (stdin читается сам, если это пайп)
  clank -c <вопрос>                     форс чтения stdin, если автодетект не сработал
  clank -r <вопрос>                     продолжить сессию этого терминала (--resume)
  clank -i <файл> <вопрос>              прикрепить изображение (PNG, JPG, WebP, GIF)
  clank -p <профиль> <вопрос>           другой профиль на один запуск
  clank -m <id|N> <вопрос>              другая модель на один запуск (остальные — запасные)
  clank --mode ask|read|auto <вопрос>   режим прав на один запуск
  clank --reasoning <уровень|off> ...   уровень reasoning на один запуск
  clank -v | -q <вопрос>                подробный лог / молча
  clank -- <вопрос>                     всё дальше — текст вопроса, даже если похоже на флаг

  clank ask <вопрос>                    то же явно (если вопрос начинается со слова-команды)

  clank config create <имя> --api-url <url> [--api-key <ключ>] [--model <id> ...]
  clank config edit [-p <имя>] [--api-url|--api-key|--proxy|--rename ...]
  clank config list | show [имя] | use <имя> | rm <имя> [-y]
  clank config model list|add|edit|rm|activate|move|enable|disable ...
  clank models [-p <имя>] [фильтр]      модели провайдера (подсказка для model add)

  clank mode [ask|read|auto]            режим прав (без аргумента — показать)
  clank lang [en|ru|auto]               язык интерфейса
  clank session show | clear [--all] | list
  clank completion [install|uninstall|print] [шелл]
  clank update [-y]                     проверить и установить новую версию с GitHub
  clank purge [-y]                      стереть все пользовательские данные (~/.config/clank/)
  clank uninstall [-y]                  удалить исполняемый файл clank
  clank nuke [-y]                       полностью удалить clank и все его данные

  clank help <команда>                  справка по разделу: help config model
  clank version

примеры:
  clank "почему grub не видит второй диск"
  dmesg | tail -50 | clank "разбери ошибку"
  clank -r "а если так: ..."

вопрос лучше брать в кавычки: без них ?, *, >, ! и обратные кавычки перехватит шелл.`,
	SessionUsage: `использование: clank session <команда>
  show              транскрипт сессии текущего терминала
  clear             стереть сессию текущего терминала
  clear --all       стереть сессии всех терминалов
  list              все сессии на машине

у каждой вкладки терминала своя история; -r продолжает историю своей вкладки`,
	ConfigUsage: `использование: clank config <команда>
  create <имя> [флаги]       создать профиль (clank help config create)
  edit [-p <имя>] [флаги]    изменить профиль: --api-url --api-key --proxy
                             --reasoning-default --exec-timeout --rename
  list                       список профилей, * — активный
  show [имя]                 профиль целиком (по умолчанию активный)
  use <имя>                  сделать профиль активным
  rm <имя> [-y]              удалить профиль
  model <команда>            модели профиля (clank help config model)

значение "-" сбрасывает поле: --proxy -, --api-key -`,
	ModelsHelp: `использование: clank models [-p <профиль>] [--all] [фильтр]
  модели провайдера — подсказка, что вписать в clank config model add.
  если провайдер отдаёт метаданные, рядом видно tools/vision/reasoning,
  а нетекстовые модели (эмбеддинги, речь) скрыты; --all — показать все.`,
	LangNameRU: "русский",
	LangNameEN: "английский",
	FailPrefix: "ошибка: ",
	LangShow:   "язык интерфейса:",
	LangSaved:  "язык сохранён:",
	LangBadArg: "неизвестный язык: %s (используй clank lang en|ru|auto)",
	HelpAsk: `clank <вопрос> — спросить модель. stdin из пайпа подхватывается сам.
Флаги: -c (форс чтения stdin), -r (продолжить сессию своей вкладки),
-i <файл> (прикрепить картинку), -p <профиль>, -m <id|N> (модель первой,
остальные запасные), --mode ask|read|auto, --reasoning <уровень|off>,
-v (подробно) / -q (молча), -- (дальше — текст вопроса).
Всё, что задано флагами, действует только на этот запуск.
Вопрос, начинающийся со слова-команды (models, config…), задавай явно:
clank ask models …`,
	HelpConfigUse: `clank config use <имя> — сделать профиль активным.
На один запуск другой профиль — флаг -p.`,
	HelpConfigList: `clank config list — профили, активный помечен *. Показывает url
и активную модель.`,
	HelpConfigRm: `clank config rm <имя> [-y] — удалить профиль (спросит подтверждение;
-y — без вопроса). Если удалён активный, clank переключится на самый
свежий из оставшихся.`,
	HelpConfigShow: `clank config show [имя] — профиль целиком: url, ключ (первые 3 символа),
прокси, reasoning, таймаут, модели с возможностями, режим, язык.`,
	HelpSessionShow: `clank session show — транскрипт сессии текущей вкладки:
вопросы, ответы, вызовы инструментов.`,
	HelpSessionClear: `clank session clear — стереть сессию текущей вкладки.
С --all — стереть сессии всех терминалов.`,
	HelpSessionList: `clank session list — все сессии на машине. * — текущая
вкладка; видно, чей шелл ещё жив, а чей терминал уже закрыт.`,
	HelpLanguage: `clank lang [en|ru|auto] — язык интерфейса (language — старое имя).
Без аргумента показывает текущий. auto — определять по LANG в терминале.`,
	HelpUpdate: `clank update [-y] — свериться с GitHub-релизами и поставить новую
версию поверх текущей. Скачанное кешируется: при ошибке прав повтор делают
через sudo clank update без перекачки.`,
	HelpPurge: `clank purge [-y] — стереть ~/.config/clank/ (профили, сессии, кэш).
Бинарник остаётся. Это заводской сброс настроек.`,
	HelpUninstall: `clank uninstall [-y] — удалить исполняемый файл clank.
Настройки и сессии остаются.`,
	HelpNuke: `clank nuke [-y] — удалить и бинарник, и все данные.
Точка невозврата; спросит подтверждение.`,
	HelpUnknown: "нет такой справки: %s",
	HelpTopics: `разделы: ask models config session mode lang completion update purge uninstall nuke
вложенные: например clank help config create, clank help config model`,
	CompletionUsage:     "использование: clank completion [install|uninstall|print] [шелл]",
	CompletionBadShell:  "неизвестный шелл: %s (умею: zsh, bash, fish)",
	CompletionDone:      "автодополнение установлено: %s",
	CompletionWriteFail: "не смог записать автодополнение: %v",
	CompletionZshHint:   "добавь в .zshrc, если ещё нет: fpath=(~/.zfunc $fpath)",
	CompletionRemoved:   "убрано автодополнение: %s",
	CompletionNone:      "автодополнение не ставилось — убирать нечего",
	AskCompletion:       "поставить автодополнение команд для %s (%s)? [Y/n]: ",
	HelpCompletion: `clank completion — поставить автодополнение для текущего шелла.
clank completion install [шелл] — поставить явно.
clank completion uninstall — убрать всё установленное.
clank completion print [шелл] — напечатать скрипт (свой путь, отладка).
Скрипт ставится на текущем языке интерфейса; смена языка через
clank language переустанавливает его автоматически.`,
	StdinReadFail:     "не смог дочитать stdin: %v — контекст может быть неполным",
	UnknownFlag:       "неизвестный флаг: %s (если это часть вопроса — поставь перед ним --)",
	NothingToAsk:      "нечего спрашивать",
	AskUsage:          "использование: clank [-c] [-r] [-p профиль] [-m модель] [--mode m] <вопрос>",
	ProfileDetail:     "профиль %s, модели: %s, режим: %s",
	ReadingStdin:      "читаю stdin, Ctrl-D когда закончишь",
	StdinBytes:        "контекст из stdin: %d байт",
	ContextWrap:       "Контекст:\n%s\n\nВопрос: %s",
	ImageAttached:     "прикреплено изображение %s (%d байт data-uri)",
	SessionSaveErr:    "не смог сохранить сессию: %v",
	Thinking:          "думаю",
	StepDetail:        "шаг %d: ответила %s, finish_reason=%s",
	AnswerCut:         "ответ обрезан сервером по лимиту токенов — переспроси короче или задай вопрос по частям",
	Interrupted:       "прервано (Ctrl-C)",
	OutcomeIntr:       "(выполнение прервано)",
	OutcomeEmpty:      "(модель вернула пустой ответ)",
	UnknownTool:       "модель просит неизвестный инструмент %q — не выполняю",
	AskEmptyQ:         "модель прислала пустой вопрос",
	ShellEmpty:        "модель прислала пустую команду — не выполняю",
	OutputMark:        "--- вывод ---",
	OutputCut:         "%s\n...(пропущено примерно %d байт середины вывода)...\n%s",
	RunStartFail:      "не удалось запустить: ",
	ExecTooLong:       "команда идёт дольше %s — снимаю",
	ConfigCorrupt:     "конфиг повреждён (%s): %w",
	ActiveNotFound:    "активный профиль %q не найден в конфиге",
	ActiveSaveFail:    "не смог сохранить выбор активного профиля: %v",
	ActiveSwitch:      "активный профиль не задан, переключаюсь на %s (сменить: clank config use <имя>)",
	ProfileInvalid:    "профиль не настроен, не хватает: %s — см. clank config show",
	KeyMissing:        "<не задан>",
	NeedProfileName:   "нужно имя профиля: clank config %s <имя>",
	NoSuchProfileList: "нет такого профиля: %s (есть: %s)",
	SaveFail:          "не смог сохранить: %v",
	ActiveProfileIs:   "активный профиль:",
	NoProfiles:        "профилей нет — создай: clank config create <имя> --api-url <url> --model <id>",
	ActiveRemoved:     "удалён активный профиль, переключаюсь на %s",
	RemovedIs:         "удалён:",
	ShowFile:          "файл:      ",
	ShowProfile:       "профиль:   ",
	ShowNotSet:        "<не задан>",
	ShowTimeout:       "таймаут:   ",
	ShowLanguage:      "язык:      ",
	ShowLangAuto:      "авто",
	BadTimeout:        "таймаут задаётся целым числом секунд (0 — дефолт)",
	UnknownSubcommand: "неизвестная подкоманда: %s",
	ProfileNoModel:    "в профиле %s нет моделей — добавь: clank config model add <id>",
	FetchingModels:    "получаю список моделей",
	FilterNoMatch:     "под фильтр %q ничего не подошло",
	ModelsEmptyAPI:    "API вернул пустой список моделей",
	ErrSetup:          "настройка профиля",
	ErrNoConn:         "не удалось соединиться",
	ModelParen:        " (модель %s)",
	ErrShort:          "ошибка настройки",
	NoConn:            "нет связи",
	HintAuth:          "ключ не принят — проверь: clank config show",
	HintURL:           "проверь base_url (clank ask сам дописывает /v1/chat/completions): clank config show",
	HintModel:         "такой модели у провайдера нет — проверь id: clank models",
	HintCtx:           "контекст переполнен — clank session clear или меньше данных в пайп",
	HintRate:          "лимит запросов у провайдера",
	HintNet:           "сеть, прокси или адрес — проверь: clank config show",
	ProxyParse:        "не разобрать proxy %q: %v — поправь: clank config edit --proxy <url|->",
	ProxyBlind:        "proxy %q без схемы или хоста (нужно вида http://host:port) — поправь: clank config edit --proxy <url|->",
	RespUnpars:        "ответ не разобрать: %v (первые байты: %s)",
	EmptyChoi:         "пустой список choices в ответе",
	ReqDetail:         "запрос: модель %s, сообщений %d, %d байт",
	RespDetail:        "ответ %d за %.1fс, %d байт",
	NoModels:          "в профиле нет включённых моделей — clank config model list",
	Answering:         "отвечает %s",
	ModelFail:         "%s не ответил (%s), пробую %s",
	RetryIn:           "%s: %s, повтор через %s (%d/%d)",
	NoAnswer:          "ни одна модель не ответила:\n  %s\nпоследняя ошибка: %v",
	ModelsUnp:         "список моделей не разобрать: %v",
	APIError:          "API вернул ошибку: %s",
	ImageBig:          "файл слишком большой (>20MB)",
	ImageType:         "файл %s не является поддерживаемым изображением (тип %s)",
	PurgeEmpty:        "конфигурационный каталог и данные уже пусты",
	PurgeAsk:          "очистить все настройки, сессии и данные (%s)? [y/N]: ",
	Aborted:           "отменено",
	PurgeFail:         "ошибка при очистке данных (%s): %v",
	PurgeDone:         "все пользовательские данные и настройки clank очищены",
	BinPathFail:       "не удалось определить путь к бинарнику: %v",
	UninstAsk:         "удалить исполняемый файл clank (%s)? [y/N]: ",
	UninstFail:        "ошибка при удалении %s: %v\n  (если нет прав, выполните: sudo rm %s)",
	UninstDone:        "исполняемый файл clank (%s) удалён с машины\n",
	NukeAsk:           "ВНИМАНИЕ: это полностью удалит бинарник (%s) и ВСЕ данные (%s). Продолжить? [y/N]: ",
	NukePartFail:      "при полном удалении возникли ошибки: %s",
	NukeDataErr:       "данные (%v)",
	NukeBinErr:        "бинарник (%v)",
	NukeDone:          "clank полностью удалён с машины (бинарник и все данные)",
	SessPathFail:      "не смог определить путь сессии: %v",
	SessFresh:         "сессии в этом терминале ещё нет — начинаю с чистого листа",
	SessReadFail:      "не смог прочитать сессию (%s): %v — начинаю с чистого листа",
	SessCorrupt:       "файл сессии повреждён (%s): %v — начинаю с чистого листа",
	SessSwitch:        "сессия в этом терминале от профиля %s, сейчас активен %s — начинаю заново",
	SessDetail:        "сессия: %d сообщений, обновлена %s",
	SessEmpty:         "сессия этого терминала пуста",
	SessReadErr:       "не смог прочитать сессию: %v",
	SessFileBad:       "файл сессии повреждён (%s): %v",
	SessHeader:        "профиль: %s, сообщений: %d, обновлена: %s\n\n",
	MarkDone:          "[выполнено]",
	MarkAnswer:        "[ответ пользователя]",
	MarkImage:         "[изображение загружено]",
	MarkAsks:          "[assistant спрашивает] %s\n",
	MarkViews:         "[assistant смотрит изображение] %s\n\n",
	MarkRuns:          "[assistant просит выполнить] %s(%s)\n\n",
	MarkAsst:          "[assistant] %s\n\n",
	SessRemoved:       "удалено сессий: %d\n",
	NoSessions:        "сессий нет",
	SessClearDone:     "сессия этого терминала очищена",
	SessClearFail:     "не смог удалить сессию: %v",
	SessClearOne:      "сессия этого терминала и так пуста",
	StClosed:          "терминал закрыт",
	StShellAlive:      "шелл %d жив",
	StHeadless:        "без терминала",
	SessRow:           "%s%-24s %s  %6d байт  %s\n",
	ConfirmHint:       "не понял: y — да, n — нет",
	NoTTYDecline:      "нет управляющего терминала — подтвердить действие невозможно, отказываю",
	ModelWantsRun:     "модель хочет выполнить:",
	RunPrompt:         "выполнить? [y/N]: ",
	NoTTYDefault:      "нет управляющего терминала — выбираю значение по умолчанию: %s",
	NoTTYNoAnswer:     "нет управляющего терминала — ответить на вопрос невозможно",
	ChooseOrType:      "выбери 1-%d или напиши свой ответ: ",
	ChooseOrDef:       "выбери 1-%d или напиши свой ответ [%s]: ",
	AnswerOrDef:       "ответ [%s]: ",
	AnswerIs:          "ответ: ",
	EnterNum:          "введи номер варианта или свой ответ",
	UpdBinFail:        "не удалось определить путь к текущему бинарнику: %v",
	UpdLinkFail:       "не удалось разрешить симлинк бинарника: %v",
	UpdChecking:       "проверяю обновления на GitHub",
	UpdCheckErr:       "не удалось проверить обновления: %v",
	UpdLatest:         "clank %s уже последней версии\n",
	UpdAvail:          "Доступна новая версия: %s (текущая: %s)\n",
	UpdNotes:          "\nИзменения в релизе:\n%s\n\n",
	UpdCached:         "найден ранее скачанный бинарник в кэше: %s",
	UpdAsk:            "обновить %s до %s? [Y/n]: ",
	UpdAbort:          "обновление отменено",
	UpdDown:           "скачиваю %s",
	UpdDownErr:        "не удалось скачать релиз: %v",
	UpdUnpErr:         "не удалось распаковать бинарник: %v",
	UpdKept:           "сохранён в кэш: %s",
	UpdCacheErr:       "не удалось прочитать кэш: %v, скачиваю заново",
	UpdApplyErr:       "ошибка при установке обновления в %s: %v",
	UpdSudoHint:       "\nБинарник сохранён в кэше. Если проблема в правах доступа, запустите:\n  sudo clank update\n",
	UpdDone:           "clank успешно обновлён до %s!\n",
	UpdNoAsset:        "не найден подходящий архив для платформы %s/%s в релизе %s",
	UpdNoExeZip:       "исполняемый файл clank.exe не найден внутри zip архива",
	UpdNoExeTar:       "исполняемый файл clank не найден внутри tar.gz архива",
	UpdNoSums:         "в релизе нет checksums.txt — ставлю без проверки",
	UpdSumBad:         "контрольная сумма архива %s не сошлась: ждали %s, посчитали %s",
	UpdSumOK:          "контрольная сумма %s сошлась",
	ShellApprox:       " (по $SHELL, не точно)",
	ShellUnknown:      "неизвестно",
	AllDisabled:       "все модели выключены — запросы не пойдут (clank config model enable <id|N>)",
	BadBool:           "%s: ожидается true или false, а не %q",
	BadMode:           "неизвестный режим: %s (ask, read или auto)",
	BadPos:            "позиция — целое число от 1, а не %q",
	BadReasoning:      "reasoning: нужен конкретный уровень (low, medium, high…) или off, а не %q",
	ColModel:          "модель",
	ColReasoning:      "reasoning",
	ColTools:          "tools",
	ColVision:         "vision",
	ConfigMigrated:    "конфиг переведён на новый формат, старый сохранён: %s",
	EditNothing:       "нечего менять — укажи хотя бы один флаг",
	ErrTimeout:        "провайдер молчит",
	FlagNeedsValue:    "флагу %s нужно значение",
	FromProfile:       "(профиль)",
	HelpConfigCreate: `clank config create <имя> --api-url <url> [--api-key <ключ>] [--proxy <url>]
                    [--reasoning-default <уровень|off>] [--exec-timeout <сек>]
                    [--model <id> [--name <название>] [--reasoning <уровень|off>]
                                  [--tools true|false] [--vision true|false]
                                  [--context <токены>]]...

Флаги после --model относятся к этой модели; --model можно повторять.
Порядок моделей — порядок фоллбэка, первая — активная. /v1 в конце url
не нужен, снимется сам. Ключ необязателен (локальным серверам не нужен).
Пример:
  clank config create work --api-url https://api.example.com --api-key sk-... \
    --model big-model --tools true --vision true --model small-model --tools true`,
	HelpConfigEdit: `clank config edit [-p <имя>] [--rename <новое>] [--api-url <url>] [--api-key <ключ|->]
                  [--proxy <url|->] [--reasoning-default <уровень|off|->]
                  [--exec-timeout <сек>]
Меняет активный профиль (или указанный через -p). "-" сбрасывает поле.
Модели правятся отдельно: clank help config model.`,
	HelpMode: `clank mode [ask|read|auto] — режим прав модели (глобальный).
  ask  — чтение само, запись файлов и команды с подтверждением (по умолчанию)
  read — только чтение: инструменты записи и shell модели не отдаются
  auto — всё без подтверждения
Чтение вне рабочего каталога спрашивается (кроме auto); ~/.ssh, ~/.gnupg и
конфиг clank — всегда с подтверждением. На один запуск — флаг --mode.`,
	ListModels:     "активная: %s, моделей: %d",
	MarkActive:     "(активная)",
	MarkDisabled:   "(выкл)",
	ModeAskDesc:    "ask — чтение само, запись файлов и команды с подтверждением",
	ModeAutoDesc:   "auto — выполнять всё без подтверждения",
	ModeIs:         "режим:",
	ModeReadDesc:   "read — только чтение, менять ничего нельзя",
	ModelAdded:     "модель %s добавлена в профиль %s",
	ModelExists:    "модель %s уже есть в профиле",
	ModelFlagFirst: "%s относится к модели — сначала --model <id>",
	ModelUsage: `использование: clank config model <команда> [-p <профиль>]
  list                          модели с номерами; номер годится вместо id
  add <id> [--pos N] [--name <название>] [--reasoning <уровень|off>]
           [--tools true|false] [--vision true|false] [--context <токены>]
  edit <id|N> --new-id/--name/--reasoning/--tools/--vision/--context
  rm <id|N>                     удалить модель
  activate <id|N>               сделать активной (поставить первой)
  move <id|N> <позиция>         переставить в цепочке
  enable|disable <id|N>         включить/выключить: выключенная остаётся
                                на месте, но фоллбэк её пропускает

порядок списка = порядок фоллбэка, активная — первая включённая.
--reasoning: уровень для reasoning_effort (low, medium, high…) или off;
"-" — брать reasoning-default профиля. --name - убирает название.
--context (128k, 1m) нужен, чтобы при переполнении идти к модели побольше.`,
	ModelsHidden:     "скрыто нетекстовых моделей: %d (показать все: --all)",
	ModelsNeedURL:    "у профиля %s не задан api-url",
	ModelsOf:         "модели профиля %s:",
	MoveUsage:        "использование: clank config model move <id|N> <позиция>",
	NeedModelID:      "нужен id или номер модели: clank config model %s <id|N>",
	NeedValueFor:     "нужно значение для %s",
	No:               "нет",
	NoModelsYet:      "моделей нет — clank config model add <id>",
	NoSuchModel:      "в профиле нет модели %s (id или номер из clank config model list)",
	PreviewMore:      "  … ещё строк: %d",
	PreviewNew:       "новый файл, %d байт",
	PreviewOverwrite: "перезапись: было %d байт, станет %d",
	PreviewReplaces:  "замен: %d",
	ProfileCreated:   "профиль создан:",
	ProfileExists:    "профиль %s уже есть — правь через clank config edit",
	RmAsk:            "удалить профиль %s вместе с ключом? [y/N]: ",
	RmNeedsYes:       "нет терминала для подтверждения — добавь -y",
	RunAuto:          "выполняю (режим auto): %s",
	SavedProfile:     "сохранено:",
	ShowActive:       "активный:  ",
	ShowEnvProxy:     "<не задан, из env>",
	ShowKey:          "API ключ:  ",
	ShowMode:         "режим:     ",
	ShowModels:       "модели (порядок = фоллбэк):",
	ShowProvider:     "<не отправляется, решает провайдер>",
	ShowProxy:        "прокси:    ",
	ShowReasonDef:    "reasoning: ",
	ShowURL:          "API URL:   ",
	SilentFor:        "нет данных уже %s",
	StreamDetail:     "поток: %d событий, %d байт за %.1fс",
	ToolArgsBad:      "аргументы %s не разобрать: %v",
	ToolDoing:        "· %s: %s",
	ToolReadOnly:     "модель просит %s в режиме только чтения — отказано",
	ToolWants:        "модель хочет %s:",
	ToolWantsOut:     "модель хочет %s ВНЕ рабочего каталога:",
	ToolWantsSens:    "модель хочет %s там, где лежат ключи или конфиг:",
	UnexpectedArg:    "лишний аргумент: %s",
	WhatEdit:         "изменить файл",
	WhatGrep:         "поискать по файлам",
	WhatList:         "посмотреть каталог",
	WhatRead:         "прочитать файл",
	WhatView:         "посмотреть картинку",
	WhatWrite:        "записать файл",
	Yes:              "да",
	HintForbidden:    "нет доступа: ключ не пускает к этой модели или провайдеру — проверь clank config show",
	BadContext:       "--context: число токенов, например 131072 или 128k, а не %q",
	ColContext:       "контекст",
	ContentFiltered:  "ответ отфильтрован провайдером",
	CtxNoBigger: `%v
  моделей с контекстом больше, чем у %s, в профиле нет`,
	CtxUnknown: `%v
  у %s не указан размер контекста, не с чем сравнить: clank config model edit %s --context <токены>`,
	EffortDropped: "%s не принимает reasoning %q — отправляю без него",
	ErrShortCtx:   "переполнен контекст",
	HintQuota:     "кончились кредиты или квота у ключа",
	HintTooLarge:  "запрос слишком большой — clank session clear или меньше данных и картинок",
	ImagesDropped: "Изображения не поддерживаются моделью, ответ генерируется без учёта изображения",
	NoToolModels:  "в истории есть вызовы инструментов, а моделей с tools в цепочке нет — clank session clear или --tools true у модели",
	NoToolsMarked: "%s не умеет инструменты — отметил в конфиге",
	PickPrompt:    "номер (Enter — стоп): ",
	PickToolModel: "модели с инструментами:",
}
