package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdCompletion — управление автодополнением:
//
//	clank completion              поставить для текущего шелла
//	clank completion install [sh] поставить (шелл по умолчанию — текущий)
//	clank completion uninstall    убрать всё, что ставили мы
//	clank completion print [sh]   напечатать скрипт (отладка, свой путь)
func cmdCompletion(args []string) int {
	if len(args) == 0 {
		return cmdCompletionInstall("")
	}
	switch strings.ToLower(args[0]) {
	case "install":
		shell := ""
		if len(args) > 1 {
			shell = strings.ToLower(args[1])
		}
		return cmdCompletionInstall(shell)
	case "uninstall":
		return cmdCompletionUninstall()
	case "print":
		shell := completionShell()
		if len(args) > 1 {
			shell = strings.ToLower(args[1])
		}
		return printCompletionScript(shell)
	default:
		fail(M.CompletionUsage)
		return exitConfig
	}
}

// cmdCompletionUninstall убирает все установленные нами скрипты
// и чистит запись в конфиге.
func cmdCompletionUninstall() int {
	cf, err := loadConfigFile()
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	if len(cf.Completions) == 0 {
		fmt.Println(M.CompletionNone)
		return exitOK
	}
	removeCompletions(cf)
	if _, err := updateConfig(func(c *ConfigFile) error {
		c.Completions = nil
		return nil
	}); err != nil {
		fail("%v", err)
		return exitConfig
	}
	return exitOK
}

func printCompletionScript(shell string) int {
	script, ok := completionScript(shell)
	if !ok {
		fail(M.CompletionBadShell, shell)
		return exitConfig
	}
	fmt.Println(script)
	return exitOK
}

// completionShell — текущий шелл одним словом. Сначала смотрим реального
// родителя (detectShell), если там мусор — откатываемся на basename $SHELL.
func completionShell() string {
	if s := normalizeShellToken(detectShell()); supportedShell(s) {
		return s
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		if s := normalizeShellToken(filepath.Base(sh)); supportedShell(s) {
			return s
		}
	}
	return "unknown"
}

// normalizeShellToken чистит имя процесса шелла: первое слово, нижний
// регистр, без дефиса логин-шелла ("-zsh" → "zsh"). detectShell может
// вернуть "zsh (via $SHELL, approximate)" — берём первое слово.
func normalizeShellToken(s string) string {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.TrimPrefix(strings.ToLower(parts[0]), "-")
}

func supportedShell(s string) bool {
	_, ok := completionScript(s)
	return ok
}

func completionScript(shell string) (string, bool) {
	ru := curLang == LangRU
	switch shell {
	case "zsh":
		if ru {
			return zshCompletionRU, true
		}
		return zshCompletionEN, true
	case "bash":
		return bashCompletion, true
	case "fish":
		if ru {
			return fishCompletionRU, true
		}
		return fishCompletionEN, true
	}
	return "", false
}

// reinstallCompletions перезаписывает установленные скрипты под текущий
// язык (зовётся из `clank language`). Шелл выводим из пути: других записей
// у нас не бывает.
func reinstallCompletions(cf *ConfigFile) {
	for _, path := range cf.Completions {
		shell := ""
		switch {
		case filepath.Base(path) == "_clank":
			shell = "zsh"
		case strings.HasSuffix(path, ".fish"):
			shell = "fish"
		case filepath.Base(path) == "clank":
			shell = "bash"
		default:
			continue
		}
		script, ok := completionScript(shell)
		if !ok {
			continue
		}
		if err := os.WriteFile(path, []byte(script+"\n"), 0644); err != nil {
			warn(M.CompletionWriteFail, err)
			continue
		}
		info(M.CompletionDone, path)
	}
}

// completionTarget — куда класть скрипт для шелла. Только пользовательские
// каталоги, никакого sudo.
func completionTarget(shell string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	switch shell {
	case "zsh":
		// Всегда ~/.zfunc: детерминировано работает и с oh-my-zsh, и без.
		// Гадать по подпапкам omz не стали — у него нет гарантированного
		// места под сторонние дополнения, а fpath юзер правит один раз.
		return filepath.Join(home, ".zfunc", "_clank"), true
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "clank"), true
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "clank.fish"), true
	}
	return "", false
}

// installCompletion пишет скрипт на место и запоминает путь в конфиге.
func installCompletion(cf *ConfigFile, shell string) (string, error) {
	script, ok := completionScript(shell)
	if !ok {
		return "", fmt.Errorf(M.CompletionBadShell, shell)
	}
	path, ok := completionTarget(shell)
	if !ok {
		return "", fmt.Errorf(M.CompletionBadShell, shell)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf(M.CompletionWriteFail, err)
	}
	if err := os.WriteFile(path, []byte(script+"\n"), 0644); err != nil {
		return "", fmt.Errorf(M.CompletionWriteFail, err)
	}
	fresh, err := updateConfig(func(c *ConfigFile) error {
		for _, p := range c.Completions {
			if p == path {
				return nil
			}
		}
		c.Completions = append(c.Completions, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	cf.Completions = fresh.Completions
	return path, nil
}

func cmdCompletionInstall(shell string) int {
	if shell == "" {
		shell = completionShell()
	}
	cf, err := loadConfigFile()
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	path, err := installCompletion(&cf, shell)
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	info(M.CompletionDone, path)
	if shell == "zsh" && strings.Contains(path, ".zfunc") {
		fmt.Println(M.CompletionZshHint)
	}
	return exitOK
}

// offerCompletion предлагает поставить автодополнение. Зовётся из
// `config create` — человек как раз всё настраивает.
// Молчит, если дополнения уже записаны в конфиге или файл на месте.
func offerCompletion(cf *ConfigFile) {
	if !haveTTY() || len(cf.Completions) > 0 {
		return
	}
	shell := completionShell()
	if _, ok := completionScript(shell); !ok {
		return
	}
	if path, ok := completionTarget(shell); ok {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	target, _ := completionTarget(shell)
	if !confirmYN(fmt.Sprintf(M.AskCompletion, shell, target), true) {
		return
	}
	path, err := installCompletion(cf, shell)
	if err != nil {
		warn("%v", err)
		return
	}
	info(M.CompletionDone, path)
	if shell == "zsh" && strings.Contains(path, ".zfunc") {
		fmt.Println(M.CompletionZshHint)
	}
}

// removeCompletions убирает установленные нами скрипты (uninstall/nuke).
func removeCompletions(cf ConfigFile) {
	for _, path := range cf.Completions {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err == nil {
			info(M.CompletionRemoved, path)
		}
	}
}

// Скрипты собираются из одного шаблона на шелл и таблицы описаний на
// язык: так RU и EN не расходятся по набору команд и флагов. Список
// проверяет completion_test.go.

type compDesc struct {
	cmds    map[string]string
	config  map[string]string
	model   map[string]string
	flags   map[string]string
	modes   map[string]string
	session map[string]string
	compl   map[string]string
	langs   map[string]string
}

var compEN = compDesc{
	cmds: map[string]string{
		"ask": "ask the model", "models": "list provider models", "config": "manage profiles and models",
		"session": "manage sessions", "mode": "permission mode", "lang": "interface language",
		"completion": "shell completion", "update": "update from GitHub", "purge": "wipe user data",
		"uninstall": "remove the binary", "nuke": "remove everything", "help": "show help", "version": "print version",
	},
	config: map[string]string{
		"create": "create a profile", "edit": "change a profile", "list": "list profiles",
		"show": "show a profile", "use": "switch profile", "rm": "remove a profile", "model": "profile models",
	},
	model: map[string]string{
		"list": "list models", "add": "add a model", "edit": "change a model", "rm": "remove a model",
		"activate": "make active", "move": "reorder", "enable": "enable", "disable": "disable",
	},
	flags: map[string]string{
		"context": "force stdin read", "resume": "resume this terminal session", "verbose": "verbose log",
		"quiet": "quiet mode", "image": "attach image", "profile": "profile for this run",
		"model": "model for this run", "mode": "permission mode for this run",
		"reasoning": "reasoning level for this run", "help": "show help",
	},
	modes:   map[string]string{"ask": "confirm writes and commands", "read": "read-only", "auto": "no confirmations", "show": "show mode"},
	session: map[string]string{"show": "show transcript", "clear": "clear session", "list": "list sessions"},
	compl:   map[string]string{"install": "install", "uninstall": "remove", "print": "print script"},
	langs:   map[string]string{"en": "English", "ru": "Russian", "auto": "autodetect"},
}

var compRU = compDesc{
	cmds: map[string]string{
		"ask": "спросить модель", "models": "модели провайдера", "config": "профили и модели",
		"session": "управление сессиями", "mode": "режим прав", "lang": "язык интерфейса",
		"completion": "автодополнение", "update": "обновление с GitHub", "purge": "стереть данные",
		"uninstall": "удалить бинарник", "nuke": "удалить всё", "help": "справка", "version": "версия",
	},
	config: map[string]string{
		"create": "создать профиль", "edit": "изменить профиль", "list": "список профилей",
		"show": "показать профиль", "use": "переключить профиль", "rm": "удалить профиль", "model": "модели профиля",
	},
	model: map[string]string{
		"list": "список моделей", "add": "добавить модель", "edit": "изменить модель", "rm": "удалить модель",
		"activate": "сделать активной", "move": "переставить", "enable": "включить", "disable": "выключить",
	},
	flags: map[string]string{
		"context": "принудительно читать stdin", "resume": "продолжить сессию вкладки", "verbose": "подробный лог",
		"quiet": "тихий режим", "image": "прикрепить картинку", "profile": "профиль на запуск",
		"model": "модель на запуск", "mode": "режим прав на запуск",
		"reasoning": "уровень reasoning на запуск", "help": "показать справку",
	},
	modes:   map[string]string{"ask": "подтверждать запись и команды", "read": "только чтение", "auto": "без подтверждений", "show": "показать режим"},
	session: map[string]string{"show": "транскрипт", "clear": "стереть сессию", "list": "список сессий"},
	compl:   map[string]string{"install": "установить", "uninstall": "убрать", "print": "напечатать"},
	langs:   map[string]string{"en": "английский", "ru": "русский", "auto": "авто"},
}

var (
	compCmds       = []string{"ask", "models", "config", "session", "mode", "lang", "completion", "update", "purge", "uninstall", "nuke", "help", "version"}
	compConfig     = []string{"create", "edit", "list", "show", "use", "rm", "model"}
	compModel      = []string{"list", "add", "edit", "rm", "activate", "move", "enable", "disable"}
	compProfileFl  = []string{"--api-url", "--api-key", "--proxy", "--reasoning-default", "--exec-timeout"}
	compModelFl    = []string{"--name", "--reasoning", "--tools", "--vision", "--context"}
	compSession    = []string{"show", "clear", "list"}
	compModes      = []string{"show", "ask", "read", "auto"}
	compComplVerbs = []string{"install", "uninstall", "print"}
	compLangs      = []string{"en", "ru", "auto"}
	compHelp       = []string{"ask", "models", "config", "session", "mode", "lang", "completion", "update", "purge", "uninstall", "nuke"}
	compLevels     = "off low medium high max"
	compProfilesSh = `clank config list 2>/dev/null | sed 's/^[* ] //;s/ .*//'`
)

// zshPairs — 'имя:описание' для _describe.
func zshPairs(names []string, desc map[string]string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "\n        '%s:%s'", n, desc[n])
	}
	return b.String()
}

func zshScript(d compDesc) string {
	f := d.flags
	return `#compdef clank
# zsh completion for clank — generated by: clank completion print zsh
_clank_profiles() {
  local -a profiles
  profiles=(${(f)"$(` + compProfilesSh + `)"})
  _describe 'profile' profiles
}

_clank() {
  local context state line
  typeset -A opt_args

  _arguments -C \
    '(-c --context)'{-c,--context}'[` + f["context"] + `]' \
    '(-r --resume)'{-r,--resume}'[` + f["resume"] + `]' \
    '(-v --verbose)'{-v,--verbose}'[` + f["verbose"] + `]' \
    '(-q --quiet)'{-q,--quiet}'[` + f["quiet"] + `]' \
    '*'{-i,--image}'[` + f["image"] + `]:file:_files' \
    '(-p --profile)'{-p,--profile}'[` + f["profile"] + `]:profile:_clank_profiles' \
    '(-m --model)'{-m,--model}'[` + f["model"] + `]:model:' \
    '--mode[` + f["mode"] + `]:mode:(ask read auto)' \
    '--reasoning[` + f["reasoning"] + `]:level:(` + compLevels + `)' \
    '(-h --help)'{-h,--help}'[` + f["help"] + `]' \
    '1: :->cmds' \
    '*:: :->args'

  case $state in
    cmds)
      local -a commands
      commands=(` + zshPairs(compCmds, d.cmds) + `
      )
      _describe 'command' commands
      ;;
    args)
      case $words[1] in
        config)
          if (( CURRENT == 2 )); then
            local -a subs
            subs=(` + zshPairs(compConfig, d.config) + `
            )
            _describe 'config subcommand' subs
            return
          fi
          case $words[2] in
            use|rm|show)
              (( CURRENT == 3 )) && _clank_profiles
              ;;
            create|edit)
              compadd -- -p --profile --rename --model ` + strings.Join(compProfileFl, " ") + ` ` + strings.Join(compModelFl, " ") + `
              ;;
            model)
              if (( CURRENT == 3 )); then
                local -a verbs
                verbs=(` + zshPairs(compModel, d.model) + `
                )
                _describe 'model subcommand' verbs
              else
                compadd -- -p --profile --pos --new-id ` + strings.Join(compModelFl, " ") + `
              fi
              ;;
          esac
          ;;
        models)
          compadd -- -p --profile --all
          ;;
        session)
          local -a ss
          ss=(` + zshPairs(compSession, d.session) + `
          )
          _describe 'session subcommand' ss
          ;;
        mode)
          local -a ms
          ms=(` + zshPairs(compModes, d.modes) + `
          )
          _describe 'mode' ms
          ;;
        lang|language)
          local -a ls
          ls=(` + zshPairs(compLangs, d.langs) + `
          )
          _describe 'language' ls
          ;;
        completion)
          if (( CURRENT == 2 )); then
            local -a cs
            cs=(` + zshPairs(compComplVerbs, d.compl) + `
            )
            _describe 'action' cs
          else
            _describe 'shell' 'zsh:zsh' 'bash:bash' 'fish:fish'
          fi
          ;;
        help)
          compadd -- ` + strings.Join(compHelp, " ") + `
          ;;
      esac
      ;;
  esac
}
_clank "$@"
`
}

func bashScript() string {
	return `# bash completion for clank — generated by: clank completion print bash
_clank() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  local cmds="` + strings.Join(compCmds, " ") + `"
  local askflags="-c -r -v -q -i -p -m -h --context --resume --verbose --quiet --image --profile --model --mode --reasoning --help"

  case "$prev" in
    -p|--profile)
      COMPREPLY=($(compgen -W "$(` + compProfilesSh + `)" -- "$cur")); return 0 ;;
    --mode)
      COMPREPLY=($(compgen -W "ask read auto" -- "$cur")); return 0 ;;
    --reasoning|--reasoning-default)
      COMPREPLY=($(compgen -W "` + compLevels + `" -- "$cur")); return 0 ;;
    --tools|--vision)
      COMPREPLY=($(compgen -W "true false" -- "$cur")); return 0 ;;
    -i|--image)
      COMPREPLY=($(compgen -f -- "$cur")); return 0 ;;
  esac

  if [[ $COMP_CWORD -eq 1 ]]; then
    if [[ "$cur" == -* ]]; then
      COMPREPLY=($(compgen -W "$askflags" -- "$cur"))
    else
      COMPREPLY=($(compgen -W "$cmds" -- "$cur"))
    fi
    return 0
  fi

  case "${COMP_WORDS[1]}" in
    config)
      if [[ $COMP_CWORD -eq 2 ]]; then
        COMPREPLY=($(compgen -W "` + strings.Join(compConfig, " ") + `" -- "$cur"))
        return 0
      fi
      case "${COMP_WORDS[2]}" in
        use|rm|show)
          [[ $COMP_CWORD -eq 3 ]] && COMPREPLY=($(compgen -W "$(` + compProfilesSh + `)" -- "$cur"))
          ;;
        create|edit)
          COMPREPLY=($(compgen -W "-p --profile --rename --model ` + strings.Join(compProfileFl, " ") + ` ` + strings.Join(compModelFl, " ") + `" -- "$cur"))
          ;;
        model)
          if [[ $COMP_CWORD -eq 3 ]]; then
            COMPREPLY=($(compgen -W "` + strings.Join(compModel, " ") + `" -- "$cur"))
          else
            COMPREPLY=($(compgen -W "-p --profile --pos --new-id ` + strings.Join(compModelFl, " ") + `" -- "$cur"))
          fi
          ;;
      esac
      ;;
    models)
      COMPREPLY=($(compgen -W "-p --profile --all" -- "$cur")) ;;
    session)
      [[ $COMP_CWORD -eq 2 ]] && COMPREPLY=($(compgen -W "` + strings.Join(compSession, " ") + `" -- "$cur")) ;;
    mode)
      [[ $COMP_CWORD -eq 2 ]] && COMPREPLY=($(compgen -W "` + strings.Join(compModes, " ") + `" -- "$cur")) ;;
    lang|language)
      [[ $COMP_CWORD -eq 2 ]] && COMPREPLY=($(compgen -W "` + strings.Join(compLangs, " ") + `" -- "$cur")) ;;
    completion)
      if [[ $COMP_CWORD -eq 2 ]]; then
        COMPREPLY=($(compgen -W "` + strings.Join(compComplVerbs, " ") + `" -- "$cur"))
      else
        COMPREPLY=($(compgen -W "zsh bash fish" -- "$cur"))
      fi
      ;;
    help)
      [[ $COMP_CWORD -eq 2 ]] && COMPREPLY=($(compgen -W "` + strings.Join(compHelp, " ") + `" -- "$cur")) ;;
  esac
}
complete -F _clank clank
`
}

func fishScript(d compDesc) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("# fish completion for clank — generated by: clank completion print fish")
	w("function __clank_profiles; %s; end", compProfilesSh)
	for _, c := range compCmds {
		w("complete -c clank -f -n __fish_use_subcommand -a %s -d '%s'", c, d.cmds[c])
	}
	for _, c := range compConfig {
		w("complete -c clank -f -n '__fish_seen_subcommand_from config; and not __fish_seen_subcommand_from %s' -a %s -d '%s'",
			strings.Join(compConfig, " "), c, d.config[c])
	}
	w("complete -c clank -f -n '__fish_seen_subcommand_from config; and __fish_seen_subcommand_from use rm show' -a '(__clank_profiles)'")
	for _, c := range compModel {
		w("complete -c clank -f -n '__fish_seen_subcommand_from model; and not __fish_seen_subcommand_from %s' -a %s -d '%s'",
			strings.Join(compModel, " "), c, d.model[c])
	}
	for _, fl := range append(append([]string{"--rename", "--model", "--pos", "--new-id"}, compProfileFl...), compModelFl...) {
		w("complete -c clank -n '__fish_seen_subcommand_from config' -l %s -r", strings.TrimPrefix(fl, "--"))
	}
	for _, c := range compSession {
		w("complete -c clank -f -n '__fish_seen_subcommand_from session' -a %s -d '%s'", c, d.session[c])
	}
	for _, c := range compModes {
		w("complete -c clank -f -n '__fish_seen_subcommand_from mode' -a %s -d '%s'", c, d.modes[c])
	}
	for _, c := range compLangs {
		w("complete -c clank -f -n '__fish_seen_subcommand_from lang language' -a %s -d '%s'", c, d.langs[c])
	}
	for _, c := range compComplVerbs {
		w("complete -c clank -f -n '__fish_seen_subcommand_from completion; and not __fish_seen_subcommand_from %s' -a %s -d '%s'",
			strings.Join(compComplVerbs, " "), c, d.compl[c])
	}
	w("complete -c clank -f -n '__fish_seen_subcommand_from install print; and __fish_seen_subcommand_from completion' -a 'zsh bash fish'")
	w("complete -c clank -f -n '__fish_seen_subcommand_from help' -a '%s'", strings.Join(compHelp, " "))
	w("complete -c clank -f -n '__fish_seen_subcommand_from models' -l all")
	f := d.flags
	w("complete -c clank -s c -l context -d '%s'", f["context"])
	w("complete -c clank -s r -l resume -d '%s'", f["resume"])
	w("complete -c clank -s v -l verbose -d '%s'", f["verbose"])
	w("complete -c clank -s q -l quiet -d '%s'", f["quiet"])
	w("complete -c clank -s i -l image -r -F -d '%s'", f["image"])
	w("complete -c clank -s p -l profile -x -a '(__clank_profiles)' -d '%s'", f["profile"])
	w("complete -c clank -s m -l model -x -d '%s'", f["model"])
	w("complete -c clank -l mode -x -a 'ask read auto' -d '%s'", f["mode"])
	w("complete -c clank -l reasoning -x -a '%s' -d '%s'", compLevels, f["reasoning"])
	w("complete -c clank -s h -l help -d '%s'", f["help"])
	return b.String()
}

var (
	zshCompletionEN  = zshScript(compEN)
	zshCompletionRU  = zshScript(compRU)
	fishCompletionEN = fishScript(compEN)
	fishCompletionRU = fishScript(compRU)
	bashCompletion   = bashScript()
)
