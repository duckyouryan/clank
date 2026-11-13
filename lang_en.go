package main

// EN — строки интерфейса (сгенерировано из прежних таблиц, правится руками).
var EN = Msgs{
	Usage: `clank — a minimal CLI for OpenAI-compatible APIs

usage:
  clank <question>                      ask a question (stdin is read when it is a pipe)
  clank -c <question>                   force stdin read if autodetect failed
  clank -r <question>                   resume this terminal's session (--resume)
  clank -i <file> <question>            attach an image (PNG, JPG, WebP, GIF)
  clank -p <profile> <question>         another profile for this run
  clank -m <id|N> <question>            another model for this run (the rest as fallback)
  clank --mode ask|read|auto <question> permission mode for this run
  clank --reasoning <level|off> ...     reasoning level for this run
  clank -v | -q <question>              verbose log / quiet
  clank -- <question>                   the rest is question text even if it looks like a flag

  clank ask <question>                  the same explicitly (if the question starts with a command word)

  clank config create <name> --api-url <url> [--api-key <key>] [--model <id> ...]
  clank config edit [-p <name>] [--api-url|--api-key|--proxy|--rename ...]
  clank config list | show [name] | use <name> | rm <name> [-y]
  clank config model list|add|edit|rm|activate|move|enable|disable ...
  clank models [-p <name>] [filter]     the provider's models (a hint for model add)

  clank mode [ask|read|auto]            permission mode (no argument — show)
  clank lang [en|ru|auto]               interface language
  clank session show | clear [--all] | list
  clank completion [install|uninstall|print] [shell]
  clank update [-y]                     check and install a new version from GitHub
  clank purge [-y]                      wipe all user data (~/.config/clank/)
  clank uninstall [-y]                  remove the clank binary
  clank nuke [-y]                       remove clank and all its data completely

  clank help <command>                  help on a topic: help config model
  clank version

examples:
  clank "why doesn't grub see the second disk"
  dmesg | tail -50 | clank "explain the error"
  clank -r "and what if: ..."

quote the question: otherwise ?, *, >, ! and backticks are eaten by the shell.`,
	SessionUsage: `usage: clank session <command>
  show              transcript of the current terminal's session
  clear             wipe the current terminal's session
  clear --all       wipe every terminal's sessions
  list              all sessions on the machine

each terminal tab has its own history; -r continues your own tab's`,
	ConfigUsage: `usage: clank config <command>
  create <name> [flags]      create a profile (clank help config create)
  edit [-p <name>] [flags]   change a profile: --api-url --api-key --proxy
                             --reasoning-default --exec-timeout --rename
  list                       list profiles, * — active
  show [name]                the whole profile (active by default)
  use <name>                 make a profile active
  rm <name> [-y]             remove a profile
  model <command>            profile models (clank help config model)

the value "-" resets a field: --proxy -, --api-key -`,
	ModelsHelp: `usage: clank models [-p <profile>] [--all] [filter]
  the provider's models — a hint for clank config model add.
  if the provider returns metadata, tools/vision/reasoning are shown next
  to the id and non-text models (embeddings, speech) are hidden; --all shows all.`,
	LangNameRU: "Russian",
	LangNameEN: "English",
	FailPrefix: "error: ",
	LangShow:   "interface language:",
	LangSaved:  "language saved:",
	LangBadArg: "unknown language: %s (use clank lang en|ru|auto)",
	HelpAsk: `clank <question> — ask the model. stdin from a pipe is picked up automatically.
Flags: -c (force stdin read), -r (resume this tab's session),
-i <file> (attach an image), -p <profile>, -m <id|N> (that model first,
the rest as fallback), --mode ask|read|auto, --reasoning <level|off>,
-v (verbose) / -q (quiet), -- (the rest is question text).
Everything set by flags applies to this run only.
A question starting with a command word (models, config…) needs an explicit
clank ask models …`,
	HelpConfigUse: `clank config use <name> — make a profile active.
For a single run use the -p flag.`,
	HelpConfigList: `clank config list — profiles, the active one is marked *. Shows the url
and the active model.`,
	HelpConfigRm: `clank config rm <name> [-y] — remove a profile (asks for confirmation;
-y skips it). If the active one is removed, clank switches to the newest
remaining profile.`,
	HelpConfigShow: `clank config show [name] — the whole profile: url, key (first 3 chars),
proxy, reasoning, timeout, models with capabilities, mode, language.`,
	HelpSessionShow: `clank session show — transcript of the current tab's session:
questions, answers, tool calls.`,
	HelpSessionClear: `clank session clear — wipe the current tab's session.
With --all — wipe every terminal's sessions.`,
	HelpSessionList: `clank session list — all sessions on the machine. * is the
current tab; shows whose shell is still alive and whose terminal is closed.`,
	HelpLanguage: `clank lang [en|ru|auto] — interface language (language is the old name).
No argument shows the current one. auto — detect from LANG in the terminal.`,
	HelpUpdate: `clank update [-y] — check GitHub releases and install the new
version over the current one. Downloads are cached: on permission errors
retry with sudo clank update, no re-download.`,
	HelpPurge: `clank purge [-y] — wipe ~/.config/clank/ (profiles, sessions, cache).
The binary stays. It is a factory reset of the settings.`,
	HelpUninstall: `clank uninstall [-y] — remove the clank executable.
Settings and sessions stay.`,
	HelpNuke: `clank nuke [-y] — remove both the binary and all data.
Point of no return; asks for confirmation.`,
	HelpUnknown: "no such help topic: %s",
	HelpTopics: `topics: ask models config session mode lang completion update purge uninstall nuke
nested ones too: e.g. clank help config create, clank help config model`,
	CompletionUsage:     "usage: clank completion [install|uninstall|print] [shell]",
	CompletionBadShell:  "unknown shell: %s (supported: zsh, bash, fish)",
	CompletionDone:      "completion installed: %s",
	CompletionWriteFail: "could not write completion: %v",
	CompletionZshHint:   "add to .zshrc if missing: fpath=(~/.zfunc $fpath)",
	CompletionRemoved:   "completion removed: %s",
	CompletionNone:      "no completions installed — nothing to remove",
	AskCompletion:       "install command completion for %s (%s)? [Y/n]: ",
	HelpCompletion: `clank completion — install completion for the current shell.
clank completion install [shell] — install explicitly.
clank completion uninstall — remove everything installed.
clank completion print [shell] — print the script (custom path, debugging).
The script follows the interface language; changing it via
clank language reinstalls automatically.`,
	StdinReadFail:     "could not read stdin to the end: %v — context may be incomplete",
	UnknownFlag:       "unknown flag: %s (if it is part of the question, put -- before it)",
	NothingToAsk:      "nothing to ask",
	AskUsage:          "usage: clank [-c] [-r] [-p profile] [-m model] [--mode m] <question>",
	ProfileDetail:     "profile %s, models: %s, mode: %s",
	ReadingStdin:      "reading stdin, Ctrl-D when done",
	StdinBytes:        "stdin context: %d bytes",
	ContextWrap:       "Context:\n%s\n\nQuestion: %s",
	ImageAttached:     "attached image %s (%d bytes of data-uri)",
	SessionSaveErr:    "could not save session: %v",
	Thinking:          "thinking",
	StepDetail:        "step %d: answered by %s, finish_reason=%s",
	AnswerCut:         "answer was cut by the server token limit — ask shorter or split the question",
	Interrupted:       "interrupted (Ctrl-C)",
	OutcomeIntr:       "(execution interrupted)",
	OutcomeEmpty:      "(model returned an empty answer)",
	UnknownTool:       "model asks for unknown tool %q — not running it",
	AskEmptyQ:         "model sent an empty question",
	ShellEmpty:        "model sent an empty command — not running it",
	OutputMark:        "--- output ---",
	OutputCut:         "%s\n...(about %d bytes of middle output skipped)...\n%s",
	RunStartFail:      "could not start: ",
	ExecTooLong:       "command runs longer than %s — killing it",
	ConfigCorrupt:     "config is corrupt (%s): %w",
	ActiveNotFound:    "active profile %q not found in config",
	ActiveSaveFail:    "could not save the active profile choice: %v",
	ActiveSwitch:      "no active profile set, switching to %s (change: clank config use <name>)",
	ProfileInvalid:    "profile is not set up, missing: %s — see clank config show",
	KeyMissing:        "<not set>",
	NeedProfileName:   "need a profile name: clank config %s <name>",
	NoSuchProfileList: "no such profile: %s (have: %s)",
	SaveFail:          "could not save: %v",
	ActiveProfileIs:   "active profile:",
	NoProfiles:        "no profiles — create one: clank config create <name> --api-url <url> --model <id>",
	ActiveRemoved:     "removed the active profile, switching to %s",
	RemovedIs:         "removed:",
	ShowFile:          "file:      ",
	ShowProfile:       "profile:   ",
	ShowNotSet:        "<not set>",
	ShowTimeout:       "timeout:   ",
	ShowLanguage:      "language:  ",
	ShowLangAuto:      "auto",
	BadTimeout:        "timeout is a whole number of seconds (0 — default)",
	UnknownSubcommand: "unknown subcommand: %s",
	ProfileNoModel:    "profile %s has no models — add one: clank config model add <id>",
	FetchingModels:    "fetching the model list",
	FilterNoMatch:     "nothing matches filter %q",
	ModelsEmptyAPI:    "API returned an empty model list",
	ErrSetup:          "profile setup",
	ErrNoConn:         "could not connect",
	ModelParen:        " (model %s)",
	ErrShort:          "setup error",
	NoConn:            "no connection",
	HintAuth:          "key was rejected — check: clank config show",
	HintURL:           "check base_url (clank ask appends /v1/chat/completions itself): clank config show",
	HintModel:         "provider has no such model — check the id: clank models",
	HintCtx:           "context overflow — clank session clear or pipe less data",
	HintRate:          "provider request limit",
	HintNet:           "network, proxy or address — check: clank config show",
	ProxyParse:        "cannot parse proxy %q: %v — fix: clank config edit --proxy <url|->",
	ProxyBlind:        "proxy %q has no scheme or host (need http://host:port) — fix: clank config edit --proxy <url|->",
	RespUnpars:        "cannot parse answer: %v (first bytes: %s)",
	EmptyChoi:         "empty choices list in answer",
	ReqDetail:         "request: model %s, %d messages, %d bytes",
	RespDetail:        "response %d in %.1fs, %d bytes",
	NoModels:          "profile has no enabled models — clank config model list",
	Answering:         "answering: %s",
	ModelFail:         "%s failed (%s), trying %s",
	RetryIn:           "%s: %s, retry in %s (%d/%d)",
	NoAnswer:          "no model answered:\n  %s\nlast error: %v",
	ModelsUnp:         "cannot parse model list: %v",
	APIError:          "API returned an error: %s",
	ImageBig:          "file too big (>20MB)",
	ImageType:         "file %s is not a supported image (type %s)",
	PurgeEmpty:        "config directory and data are already empty",
	PurgeAsk:          "wipe all settings, sessions and data (%s)? [y/N]: ",
	Aborted:           "aborted",
	PurgeFail:         "error wiping data (%s): %v",
	PurgeDone:         "all clank user data and settings wiped",
	BinPathFail:       "could not locate the binary: %v",
	UninstAsk:         "remove the clank executable (%s)? [y/N]: ",
	UninstFail:        "error removing %s: %v\n  (if permission is missing, run: sudo rm %s)",
	UninstDone:        "clank executable (%s) removed from the machine\n",
	NukeAsk:           "WARNING: this fully removes the binary (%s) and ALL data (%s). Continue? [y/N]: ",
	NukePartFail:      "errors during full removal: %s",
	NukeDataErr:       "data (%v)",
	NukeBinErr:        "binary (%v)",
	NukeDone:          "clank fully removed from the machine (binary and all data)",
	SessPathFail:      "could not determine the session path: %v",
	SessFresh:         "no session in this terminal yet — starting fresh",
	SessReadFail:      "could not read session (%s): %v — starting fresh",
	SessCorrupt:       "session file is corrupt (%s): %v — starting fresh",
	SessSwitch:        "this terminal's session is from profile %s, active is %s — starting over",
	SessDetail:        "session: %d messages, updated %s",
	SessEmpty:         "this terminal's session is empty",
	SessReadErr:       "could not read session: %v",
	SessFileBad:       "session file is corrupt (%s): %v",
	SessHeader:        "profile: %s, messages: %d, updated: %s\n\n",
	MarkDone:          "[done]",
	MarkAnswer:        "[user answer]",
	MarkImage:         "[image loaded]",
	MarkAsks:          "[assistant asks] %s\n",
	MarkViews:         "[assistant views image] %s\n\n",
	MarkRuns:          "[assistant asks to run] %s(%s)\n\n",
	MarkAsst:          "[assistant] %s\n\n",
	SessRemoved:       "sessions removed: %d\n",
	NoSessions:        "no sessions",
	SessClearDone:     "this terminal's session cleared",
	SessClearFail:     "could not delete session: %v",
	SessClearOne:      "this terminal's session is already empty",
	StClosed:          "terminal closed",
	StShellAlive:      "shell %d alive",
	StHeadless:        "headless",
	SessRow:           "%s%-24s %s  %6d bytes  %s\n",
	ConfirmHint:       "didn't get it: y — yes, n — no",
	NoTTYDecline:      "no controlling terminal — cannot confirm the action, declining",
	ModelWantsRun:     "model wants to run:",
	RunPrompt:         "go ahead? [y/N]: ",
	NoTTYDefault:      "no controlling terminal — taking the default: %s",
	NoTTYNoAnswer:     "no controlling terminal — cannot answer the question",
	ChooseOrType:      "pick 1-%d or type your answer: ",
	ChooseOrDef:       "pick 1-%d or type your answer [%s]: ",
	AnswerOrDef:       "answer [%s]: ",
	AnswerIs:          "answer: ",
	EnterNum:          "enter an option number or your answer",
	UpdBinFail:        "could not locate the current binary: %v",
	UpdLinkFail:       "could not resolve the binary symlink: %v",
	UpdChecking:       "checking GitHub for updates",
	UpdCheckErr:       "could not check for updates: %v",
	UpdLatest:         "clank %s is already the latest\n",
	UpdAvail:          "New version available: %s (current: %s)\n",
	UpdNotes:          "\nRelease notes:\n%s\n\n",
	UpdCached:         "found a previously downloaded binary in cache: %s",
	UpdAsk:            "update %s to %s? [Y/n]: ",
	UpdAbort:          "update cancelled",
	UpdDown:           "downloading %s",
	UpdDownErr:        "could not download the release: %v",
	UpdUnpErr:         "could not unpack the binary: %v",
	UpdKept:           "kept in cache: %s",
	UpdCacheErr:       "could not read cache: %v, downloading again",
	UpdApplyErr:       "error installing the update into %s: %v",
	UpdSudoHint:       "\nBinary kept in cache. If it is a permissions issue, run:\n  sudo clank update\n",
	UpdDone:           "clank successfully updated to %s!\n",
	UpdNoAsset:        "no suitable archive for %s/%s in release %s",
	UpdNoExeZip:       "clank.exe not found inside the zip archive",
	UpdNoExeTar:       "clank executable not found inside the tar.gz archive",
	UpdNoSums:         "no checksums.txt in the release — installing unverified",
	UpdSumBad:         "archive %s checksum mismatch: expected %s, got %s",
	UpdSumOK:          "checksum of %s matches",
	ShellApprox:       " (via $SHELL, approximate)",
	ShellUnknown:      "unknown",
	AllDisabled:       "all models are disabled — no requests will go out (clank config model enable <id|N>)",
	BadBool:           "%s: expected true or false, got %q",
	BadMode:           "unknown mode: %s (ask, read or auto)",
	BadPos:            "position is a whole number from 1, not %q",
	BadReasoning:      "reasoning: give a concrete level (low, medium, high…) or off, not %q",
	ColModel:          "model",
	ColReasoning:      "reasoning",
	ColTools:          "tools",
	ColVision:         "vision",
	ConfigMigrated:    "config migrated to the new format, the old one is saved as %s",
	EditNothing:       "nothing to change — give at least one flag",
	ErrTimeout:        "provider went silent",
	FlagNeedsValue:    "flag %s needs a value",
	FromProfile:       "(profile)",
	HelpConfigCreate: `clank config create <name> --api-url <url> [--api-key <key>] [--proxy <url>]
                    [--reasoning-default <level|off>] [--exec-timeout <sec>]
                    [--model <id> [--name <title>] [--reasoning <level|off>]
                                  [--tools true|false] [--vision true|false]
                                  [--context <tokens>]]...

Flags after --model belong to that model; --model may repeat. Model order
is the fallback order, the first one is active. A trailing /v1 in the url
is not needed, it is stripped. The key is optional (local servers skip it).
Example:
  clank config create work --api-url https://api.example.com --api-key sk-... \
    --model big-model --tools true --vision true --model small-model --tools true`,
	HelpConfigEdit: `clank config edit [-p <name>] [--rename <new>] [--api-url <url>] [--api-key <key|->]
                  [--proxy <url|->] [--reasoning-default <level|off|->]
                  [--exec-timeout <sec>]
Changes the active profile (or the one given with -p). "-" resets a field.
Models are edited separately: clank help config model.`,
	HelpMode: `clank mode [ask|read|auto] — what the model may do (global).
  ask  — reads run freely, file writes and commands need confirmation (default)
  read — read-only: write tools and the shell are not given to the model
  auto — everything without confirmation
Reading outside the working directory asks first (except auto); ~/.ssh,
~/.gnupg and the clank config always ask. For a single run — the --mode flag.`,
	ListModels:     "active: %s, models: %d",
	MarkActive:     "(active)",
	MarkDisabled:   "(off)",
	ModeAskDesc:    "ask — reads run freely, file writes and commands need confirmation",
	ModeAutoDesc:   "auto — run everything without confirmation",
	ModeIs:         "mode:",
	ModeReadDesc:   "read — read-only, nothing can be changed",
	ModelAdded:     "model %s added to profile %s",
	ModelExists:    "model %s is already in the profile",
	ModelFlagFirst: "%s belongs to a model — put --model <id> before it",
	ModelUsage: `usage: clank config model <command> [-p <profile>]
  list                          models with numbers; a number works instead of an id
  add <id> [--pos N] [--name <title>] [--reasoning <level|off>]
           [--tools true|false] [--vision true|false] [--context <tokens>]
  edit <id|N> --new-id/--name/--reasoning/--tools/--vision/--context
  rm <id|N>                     remove a model
  activate <id|N>               make it active (move it first)
  move <id|N> <position>        reorder the chain
  enable|disable <id|N>         switch on/off: a disabled model keeps its
                                place but the fallback skips it

list order = fallback order, the active model is the first enabled one.
--reasoning: level for reasoning_effort (low, medium, high…) or off;
"-" — use the profile's reasoning-default. --name - removes the title.
--context (128k, 1m) lets an overflow move on to a bigger model.`,
	ModelsHidden:     "non-text models hidden: %d (show all: --all)",
	ModelsNeedURL:    "profile %s has no api-url",
	ModelsOf:         "models of profile %s:",
	MoveUsage:        "usage: clank config model move <id|N> <position>",
	NeedModelID:      "need a model id or number: clank config model %s <id|N>",
	NeedValueFor:     "%s needs a value",
	No:               "no",
	NoModelsYet:      "no models — clank config model add <id>",
	NoSuchModel:      "profile has no model %s (id or number from clank config model list)",
	PreviewMore:      "  … %d more lines",
	PreviewNew:       "new file, %d bytes",
	PreviewOverwrite: "overwrite: %d bytes now, %d after",
	PreviewReplaces:  "replacements: %d",
	ProfileCreated:   "profile created:",
	ProfileExists:    "profile %s already exists — change it with clank config edit",
	RmAsk:            "remove profile %s together with its key? [y/N]: ",
	RmNeedsYes:       "no terminal to confirm — add -y",
	RunAuto:          "running (auto mode): %s",
	SavedProfile:     "saved:",
	ShowActive:       "active:    ",
	ShowEnvProxy:     "<not set, from env>",
	ShowKey:          "API key:   ",
	ShowMode:         "mode:      ",
	ShowModels:       "models (order = fallback):",
	ShowProvider:     "<not sent, provider decides>",
	ShowProxy:        "proxy:     ",
	ShowReasonDef:    "reasoning: ",
	ShowURL:          "API URL:   ",
	SilentFor:        "no data for %s",
	StreamDetail:     "stream: %d events, %d bytes in %.1fs",
	ToolArgsBad:      "cannot parse %s arguments: %v",
	ToolDoing:        "· %s: %s",
	ToolReadOnly:     "model asks for %s in read-only mode — refused",
	ToolWants:        "model wants to %s:",
	ToolWantsOut:     "model wants to %s OUTSIDE the working directory:",
	ToolWantsSens:    "model wants to %s where keys or config live:",
	UnexpectedArg:    "unexpected argument: %s",
	WhatEdit:         "edit a file",
	WhatGrep:         "search files",
	WhatList:         "list a directory",
	WhatRead:         "read a file",
	WhatView:         "view an image",
	WhatWrite:        "write a file",
	Yes:              "yes",
	HintForbidden:    "forbidden: the key is not allowed to use this model or provider — check clank config show",
	BadContext:       "--context: a number of tokens like 131072 or 128k, not %q",
	ColContext:       "context",
	ContentFiltered:  "the provider filtered the answer",
	CtxNoBigger: `%v
  no model in the profile has a bigger context than %s`,
	CtxUnknown: `%v
  %s has no context size set, nothing to compare with: clank config model edit %s --context <tokens>`,
	EffortDropped: "%s does not accept reasoning %q — sending without it",
	ErrShortCtx:   "context overflow",
	HintQuota:     "the key is out of credits or quota",
	HintTooLarge:  "request too large — clank session clear or send less data and fewer images",
	ImagesDropped: "The model does not support images, answering without them",
	NoToolModels:  "the history has tool calls but no model in the chain has tools — clank session clear or --tools true on a model",
	NoToolsMarked: "%s does not support tools — marked in the config",
	PickPrompt:    "number (Enter — stop): ",
	PickToolModel: "models with tools:",
}
