package main

import (
	"fmt"
	"os"
	"strings"
)

// Lang — код языка интерфейса. Сейчас два: ru и en, всё остальное
// откатывается на en. Новый язык добавляется так: таблица MsgsXX в
// lang_xx.go и одна ветка в applyLang — компилятор сам подсветит
// незаполненные поля (структура одна на всех).
type Lang string

const (
	LangEN Lang = "en"
	LangRU Lang = "ru"
)

// Msgs — все строки интерфейса на одном языке. Форматные глаголы (%s, %d)
// обязаны совпадать в обеих таблицах.
type Msgs struct {
	Usage               string
	SessionUsage        string
	ConfigUsage         string
	ModelsHelp          string
	LangNameRU          string
	LangNameEN          string
	FailPrefix          string
	LangShow            string
	LangSaved           string
	LangBadArg          string
	HelpAsk             string
	HelpConfigUse       string
	HelpConfigList      string
	HelpConfigRm        string
	HelpConfigShow      string
	HelpSessionShow     string
	HelpSessionClear    string
	HelpSessionList     string
	HelpLanguage        string
	HelpUpdate          string
	HelpPurge           string
	HelpUninstall       string
	HelpNuke            string
	HelpUnknown         string
	HelpTopics          string
	CompletionUsage     string
	CompletionBadShell  string
	CompletionDone      string
	CompletionWriteFail string
	CompletionZshHint   string
	CompletionRemoved   string
	CompletionNone      string
	AskCompletion       string
	HelpCompletion      string
	StdinReadFail       string
	UnknownFlag         string
	NothingToAsk        string
	AskUsage            string
	ProfileDetail       string
	ReadingStdin        string
	StdinBytes          string
	ContextWrap         string
	ImageAttached       string
	SessionSaveErr      string
	Thinking            string
	StepDetail          string
	AnswerCut           string
	Interrupted         string
	OutcomeIntr         string
	OutcomeEmpty        string
	UnknownTool         string
	AskEmptyQ           string
	ShellEmpty          string
	OutputMark          string
	OutputCut           string
	RunStartFail        string
	ExecTooLong         string
	ConfigCorrupt       string
	ActiveNotFound      string
	ActiveSaveFail      string
	ActiveSwitch        string
	ProfileInvalid      string
	KeyMissing          string
	NeedProfileName     string
	NoSuchProfileList   string
	SaveFail            string
	ActiveProfileIs     string
	NoProfiles          string
	ActiveRemoved       string
	RemovedIs           string
	ShowFile            string
	ShowProfile         string
	ShowNotSet          string
	ShowTimeout         string
	ShowLanguage        string
	ShowLangAuto        string
	BadTimeout          string
	UnknownSubcommand   string
	ProfileNoModel      string
	FetchingModels      string
	FilterNoMatch       string
	ModelsEmptyAPI      string
	ErrSetup            string
	ErrNoConn           string
	ModelParen          string
	ErrShort            string
	NoConn              string
	HintAuth            string
	HintURL             string
	HintModel           string
	HintCtx             string
	HintRate            string
	HintNet             string
	ProxyParse          string
	ProxyBlind          string
	RespUnpars          string
	EmptyChoi           string
	ReqDetail           string
	RespDetail          string
	NoModels            string
	Answering           string
	ModelFail           string
	RetryIn             string
	NoAnswer            string
	ModelsUnp           string
	APIError            string
	ImageBig            string
	ImageType           string
	PurgeEmpty          string
	PurgeAsk            string
	Aborted             string
	PurgeFail           string
	PurgeDone           string
	BinPathFail         string
	UninstAsk           string
	UninstFail          string
	UninstDone          string
	NukeAsk             string
	NukePartFail        string
	NukeDataErr         string
	NukeBinErr          string
	NukeDone            string
	SessPathFail        string
	SessFresh           string
	SessReadFail        string
	SessCorrupt         string
	SessSwitch          string
	SessDetail          string
	SessEmpty           string
	SessReadErr         string
	SessFileBad         string
	SessHeader          string
	MarkDone            string
	MarkAnswer          string
	MarkImage           string
	MarkAsks            string
	MarkViews           string
	MarkRuns            string
	MarkAsst            string
	SessRemoved         string
	NoSessions          string
	SessClearDone       string
	SessClearFail       string
	SessClearOne        string
	StClosed            string
	StShellAlive        string
	StHeadless          string
	SessRow             string
	ConfirmHint         string
	NoTTYDecline        string
	ModelWantsRun       string
	RunPrompt           string
	NoTTYDefault        string
	NoTTYNoAnswer       string
	ChooseOrType        string
	ChooseOrDef         string
	AnswerOrDef         string
	AnswerIs            string
	EnterNum            string
	UpdBinFail          string
	UpdLinkFail         string
	UpdChecking         string
	UpdCheckErr         string
	UpdLatest           string
	UpdAvail            string
	UpdNotes            string
	UpdCached           string
	UpdAsk              string
	UpdAbort            string
	UpdDown             string
	UpdDownErr          string
	UpdUnpErr           string
	UpdKept             string
	UpdCacheErr         string
	UpdApplyErr         string
	UpdSudoHint         string
	UpdDone             string
	UpdNoAsset          string
	UpdNoExeZip         string
	UpdNoExeTar         string
	UpdNoSums           string
	UpdSumBad           string
	UpdSumOK            string
	ShellApprox         string
	ShellUnknown        string
	AllDisabled         string
	BadBool             string
	BadMode             string
	BadPos              string
	BadReasoning        string
	ColModel            string
	ColReasoning        string
	ColTools            string
	ColVision           string
	ConfigMigrated      string
	EditNothing         string
	ErrTimeout          string
	FlagNeedsValue      string
	FromProfile         string
	HelpConfigCreate    string
	HelpConfigEdit      string
	HelpMode            string
	ListModels          string
	MarkActive          string
	MarkDisabled        string
	ModeAskDesc         string
	ModeAutoDesc        string
	ModeIs              string
	ModeReadDesc        string
	ModelAdded          string
	ModelExists         string
	ModelFlagFirst      string
	ModelUsage          string
	ModelsHidden        string
	ModelsNeedURL       string
	ModelsOf            string
	MoveUsage           string
	NeedModelID         string
	NeedValueFor        string
	No                  string
	NoModelsYet         string
	NoSuchModel         string
	PreviewMore         string
	PreviewNew          string
	PreviewOverwrite    string
	PreviewReplaces     string
	ProfileCreated      string
	ProfileExists       string
	RmAsk               string
	RmNeedsYes          string
	RunAuto             string
	SavedProfile        string
	ShowActive          string
	ShowEnvProxy        string
	ShowKey             string
	ShowMode            string
	ShowModels          string
	ShowProvider        string
	ShowProxy           string
	ShowReasonDef       string
	ShowURL             string
	SilentFor           string
	StreamDetail        string
	ToolArgsBad         string
	ToolDoing           string
	ToolReadOnly        string
	ToolWants           string
	ToolWantsOut        string
	ToolWantsSens       string
	UnexpectedArg       string
	WhatEdit            string
	WhatGrep            string
	WhatList            string
	WhatRead            string
	WhatView            string
	WhatWrite           string
	Yes                 string
	HintForbidden       string
	BadContext          string
	ColContext          string
	ContentFiltered     string
	CtxNoBigger         string
	CtxUnknown          string
	EffortDropped       string
	ErrShortCtx         string
	HintQuota           string
	HintTooLarge        string
	ImagesDropped       string
	NoToolModels        string
	NoToolsMarked       string
	PickPrompt          string
	PickToolModel       string
}

// M — активная таблица строк. Переключается один раз на старте.
var M = &EN

// curLang — активный язык (нужен systemprompt.go).
var curLang = LangEN

// detectLang определяет язык по переменным окружения. Порядок — как
// принято: LC_ALL главнее всего, дальше LC_MESSAGES, дальше LANG.
// Первое непустое значение решает: начинается с ru — русский, иначе
// английский (включая C/POSIX и неизвестные локали).
func detectLang() Lang {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "ru") {
				return LangRU
			}
			return LangEN
		}
	}
	return LangEN
}

// resolveLang: явная настройка из конфига важнее автоопределения.
func resolveLang(setting string) Lang {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "ru":
		return LangRU
	case "en":
		return LangEN
	default:
		return detectLang()
	}
}

// applyLang включает таблицу нужного языка.
func applyLang(l Lang) {
	curLang = l
	if l == LangRU {
		M = &RU
	} else {
		M = &EN
	}
}

// showLanguage — значение для строки языка в config show.
func showLanguage(setting string) string {
	if strings.TrimSpace(setting) == "" {
		return M.ShowLangAuto + " (" + string(resolveLang("")) + ")"
	}
	return setting
}

// initLang читает настройку языка из конфига (без побочных эффектов —
// loadConfigFile ничего не пишет) и включает нужную таблицу.
func initLang() {
	cf, err := loadConfigFile()
	if err != nil {
		applyLang(detectLang())
		return
	}
	applyLang(resolveLang(cf.Language))
}

// cmdHelp — `clank help [команда [подкоманда]]`. Без аргументов — общий
// текст; с путём вроде `help config create` — справка по разделу.
func cmdHelp(args []string) int {
	if len(args) == 0 {
		usage()
		return exitOK
	}
	key := strings.ToLower(strings.Join(args, " "))
	topics := map[string]string{
		"ask":           M.HelpAsk,
		"models":        M.ModelsHelp,
		"config":        M.ConfigUsage,
		"config create": M.HelpConfigCreate,
		"config edit":   M.HelpConfigEdit,
		"config list":   M.HelpConfigList,
		"config show":   M.HelpConfigShow,
		"config use":    M.HelpConfigUse,
		"config rm":     M.HelpConfigRm,
		"config model":  M.ModelUsage,
		"session":       M.SessionUsage,
		"session show":  M.HelpSessionShow,
		"session clear": M.HelpSessionClear,
		"session list":  M.HelpSessionList,
		"mode":          M.HelpMode,
		"lang":          M.HelpLanguage,
		"language":      M.HelpLanguage,
		"completion":    M.HelpCompletion,
		"update":        M.HelpUpdate,
		"purge":         M.HelpPurge,
		"uninstall":     M.HelpUninstall,
		"nuke":          M.HelpNuke,
	}
	for _, verb := range []string{"list", "add", "edit", "rm", "activate", "move", "enable", "disable"} {
		topics["config model "+verb] = M.ModelUsage
	}
	if t, ok := topics[key]; ok {
		fmt.Println(t)
		return exitOK
	}
	fail(M.HelpUnknown, strings.Join(args, " "))
	fmt.Println(M.HelpTopics)
	return exitConfig
}

// cmdLanguage — `clank lang [en|ru|auto]` (старое имя language — алиас): без аргумента показывает
// текущий язык, с аргументом — сохраняет в конфиг.
func cmdLanguage(args []string) int {
	cf, err := loadConfigFile()
	if err != nil {
		fail("%v", err)
		return exitConfig
	}

	if len(args) == 0 {
		l := resolveLang(cf.Language)
		name := M.LangNameEN
		if l == LangRU {
			name = M.LangNameRU
		}
		fmt.Println(M.LangShow, name)
		return exitOK
	}

	switch strings.ToLower(args[0]) {
	case "ru", "русский":
		cf.Language = "ru"
	case "en", "английский", "english":
		cf.Language = "en"
	case "auto", "авто", "-", "default":
		cf.Language = ""
	default:
		fail(M.LangBadArg, args[0])
		return exitConfig
	}
	lang := cf.Language
	if _, err := updateConfig(func(c *ConfigFile) error {
		c.Language = lang
		return nil
	}); err != nil {
		fail("%v", err)
		return exitConfig
	}
	applyLang(resolveLang(cf.Language))
	reinstallCompletions(&cf)
	name := M.LangNameEN
	if curLang == LangRU {
		name = M.LangNameRU
	}
	fmt.Println(M.LangSaved, name)
	return exitOK
}
