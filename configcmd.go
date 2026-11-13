package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// --- разбор флагов ---

type kv struct{ k, v string }

// scanFlags раскладывает аргументы на позиционные и флаги, сохраняя
// порядок флагов: в `config create` флаги модели относятся к последнему
// --model. Флаги со значением принимаются как `--x v` и `--x=v`.
func scanFlags(args []string, valueFlags, boolFlags []string) (pos []string, flags []kv, err error) {
	isValue := map[string]bool{}
	for _, f := range valueFlags {
		isValue[f] = true
	}
	isBool := map[string]bool{}
	for _, f := range boolFlags {
		isBool[f] = true
	}
	literal := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if literal || !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		if a == "--" {
			literal = true
			continue
		}
		name, val, hasEq := strings.Cut(a, "=")
		switch {
		case isValue[name]:
			if !hasEq {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf(M.FlagNeedsValue, name)
				}
				i++
				val = args[i]
			}
			flags = append(flags, kv{name, val})
		case isBool[name] && !hasEq:
			flags = append(flags, kv{name, ""})
		default:
			return nil, nil, fmt.Errorf(M.UnknownFlag, a)
		}
	}
	return pos, flags, nil
}

func flagSet(flags []kv, name string) (string, bool) {
	val, ok := "", false
	for _, f := range flags {
		if f.k == name {
			val, ok = f.v, true
		}
	}
	return val, ok
}

func parseBool(name, v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1", "y":
		return true, nil
	case "false", "no", "off", "0", "n":
		return false, nil
	}
	return false, fmt.Errorf(M.BadBool, name, v)
}

// checkReasoningValue — уровень или off. on/true отвергаем: уровень
// нужно назвать явно, провайдеры понимают только конкретные значения.
func checkReasoningValue(v string) error {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "on", "true", "yes", "1":
		return fmt.Errorf(M.BadReasoning, v)
	}
	if strings.ContainsAny(v, " \t,") {
		return fmt.Errorf(M.BadReasoning, v)
	}
	return nil
}

func normReasoning(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "none" {
		return "off"
	}
	return v
}

func parseTimeout(v string) (int, error) {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0, errors.New(M.BadTimeout)
	}
	return secs, nil
}

// --- clank config ---

func printConfigUsage() {
	fmt.Fprintln(os.Stderr, M.ConfigUsage)
}

func cmdConfig(args []string) int {
	if len(args) == 0 {
		printConfigUsage()
		return exitConfig
	}
	rest := args[1:]
	var err error
	switch args[0] {
	case "-h", "--help", "help":
		printConfigUsage()
		return exitOK
	case "create":
		err = configCreate(rest)
	case "edit":
		err = configEdit(rest)
	case "list":
		err = configList(rest)
	case "show":
		err = configShow(rest)
	case "use":
		err = configUse(rest)
	case "rm":
		err = configRm(rest)
	case "model":
		return cmdConfigModel(rest)
	default:
		fail(M.UnknownSubcommand, args[0])
		printConfigUsage()
		return exitConfig
	}
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	return exitOK
}

var (
	profileValueFlags = []string{"--api-url", "--api-key", "--proxy", "--reasoning-default", "--exec-timeout"}
	modelValueFlags   = []string{"--name", "--reasoning", "--tools", "--vision", "--context"}
)

// applyProfileFlag — общее для create и edit. "-" сбрасывает значение.
func applyProfileFlag(p *Profile, f kv) error {
	clear := f.v == "-"
	switch f.k {
	case "--api-url":
		if clear || strings.TrimSpace(f.v) == "" {
			return fmt.Errorf(M.NeedValueFor, f.k)
		}
		p.BaseURL = normalizeBaseURL(f.v)
	case "--api-key":
		if clear {
			p.APIKey = ""
		} else {
			p.APIKey = strings.TrimSpace(f.v)
		}
	case "--proxy":
		if clear {
			p.Proxy = ""
		} else {
			p.Proxy = strings.TrimSpace(f.v)
		}
	case "--reasoning-default":
		if clear {
			p.ReasoningDefault = ""
			return nil
		}
		if err := checkReasoningValue(f.v); err != nil {
			return err
		}
		p.ReasoningDefault = normReasoning(f.v)
	case "--exec-timeout":
		secs, err := parseTimeout(f.v)
		if err != nil {
			return err
		}
		p.ExecTimeoutSec = secs
	}
	return nil
}

// applyModelFlag — общее для create, model add и model edit.
func applyModelFlag(m *ModelEntry, f kv) error {
	switch f.k {
	case "--name":
		if f.v == "-" {
			m.Name = ""
		} else {
			m.Name = strings.TrimSpace(f.v)
		}
	case "--reasoning":
		if f.v == "-" {
			m.Reasoning = ""
			return nil
		}
		if err := checkReasoningValue(f.v); err != nil {
			return err
		}
		m.Reasoning = normReasoning(f.v)
	case "--tools":
		v, err := parseBool(f.k, f.v)
		if err != nil {
			return err
		}
		m.Tools = v
	case "--vision":
		v, err := parseBool(f.k, f.v)
		if err != nil {
			return err
		}
		m.Vision = v
	case "--context":
		if f.v == "-" {
			m.Context = 0
			return nil
		}
		n, err := parseTokens(f.v)
		if err != nil {
			return err
		}
		m.Context = n
	}
	return nil
}

// parseTokens — число токенов, можно с k/m: 131072, 128k, 1m.
func parseTokens(v string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(v))
	mult := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1024, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1024*1024, strings.TrimSuffix(s, "m")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf(M.BadContext, v)
	}
	return n * mult, nil
}

// fmtTokens — обратно в короткую форму для таблицы.
func fmtTokens(n int) string {
	switch {
	case n == 0:
		return "-"
	case n%(1024*1024) == 0:
		return strconv.Itoa(n/(1024*1024)) + "m"
	case n%1024 == 0:
		return strconv.Itoa(n/1024) + "k"
	case n >= 1000:
		return strconv.Itoa(n/1000) + "k"
	}
	return strconv.Itoa(n)
}

func isOneOf(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func configCreate(args []string) error {
	valueFlags := append(append([]string{"--model"}, profileValueFlags...), modelValueFlags...)
	pos, flags, err := scanFlags(args, valueFlags, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return fmt.Errorf(M.NeedProfileName, "create")
	}
	name := pos[0]

	p := Profile{Created: time.Now().Format(time.RFC3339)}
	cur := -1
	for _, f := range flags {
		switch {
		case f.k == "--model":
			id := strings.TrimSpace(f.v)
			if id == "" {
				return fmt.Errorf(M.NeedValueFor, f.k)
			}
			for _, m := range p.Models {
				if m.ID == id {
					return fmt.Errorf(M.ModelExists, id)
				}
			}
			p.Models = append(p.Models, ModelEntry{ID: id})
			cur = len(p.Models) - 1
		case isOneOf(f.k, modelValueFlags):
			if cur < 0 {
				return fmt.Errorf(M.ModelFlagFirst, f.k)
			}
			if err := applyModelFlag(&p.Models[cur], f); err != nil {
				return err
			}
		default:
			if err := applyProfileFlag(&p, f); err != nil {
				return err
			}
		}
	}
	if p.BaseURL == "" {
		return fmt.Errorf(M.NeedValueFor, "--api-url")
	}

	cf, err := updateConfig(func(cf *ConfigFile) error {
		if _, ok := cf.Profiles[name]; ok {
			return fmt.Errorf(M.ProfileExists, name)
		}
		cf.Profiles[name] = p
		if _, ok := cf.Profiles[cf.Active]; !ok {
			cf.Active = name
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Println(M.ProfileCreated, name)
	if cf.Active == name {
		fmt.Println(M.ActiveProfileIs, name)
	}
	if len(p.Models) == 0 {
		info(M.ProfileNoModel, name)
	}
	offerCompletion(&cf)
	return nil
}

func configEdit(args []string) error {
	pos, flags, err := scanFlags(args, append([]string{"-p", "--profile", "--rename"}, profileValueFlags...), nil)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf(M.UnexpectedArg, pos[0])
	}
	target, _ := flagSet(flags, "-p")
	if v, ok := flagSet(flags, "--profile"); ok {
		target = v
	}
	changes := 0
	for _, f := range flags {
		if f.k != "-p" && f.k != "--profile" {
			changes++
		}
	}
	if changes == 0 {
		return errors.New(M.EditNothing)
	}

	var finalName string
	_, err = updateConfig(func(cf *ConfigFile) error {
		name, p, err := resolveProfile(cf, target)
		if err != nil {
			return err
		}
		newName := name
		for _, f := range flags {
			switch f.k {
			case "-p", "--profile":
			case "--rename":
				nn := strings.TrimSpace(f.v)
				if nn == "" {
					return fmt.Errorf(M.NeedValueFor, f.k)
				}
				if _, ok := cf.Profiles[nn]; ok && nn != name {
					return fmt.Errorf(M.ProfileExists, nn)
				}
				newName = nn
			default:
				if err := applyProfileFlag(&p, f); err != nil {
					return err
				}
			}
		}
		delete(cf.Profiles, name)
		cf.Profiles[newName] = p
		if cf.Active == name {
			cf.Active = newName
		}
		finalName = newName
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Println(M.SavedProfile, finalName)
	return nil
}

func configList(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf(M.UnexpectedArg, args[0])
	}
	cf, err := loadConfigFile()
	if err != nil {
		return err
	}
	if len(cf.Profiles) == 0 {
		fmt.Println(M.NoProfiles)
		return nil
	}
	names := profileNames(cf)
	w := 0
	for _, n := range names {
		w = max(w, utf8.RuneCountInString(n))
	}
	for _, n := range names {
		mark := "  "
		if n == cf.Active {
			mark = "* "
		}
		p := cf.Profiles[n]
		active := "-"
		if i := p.activeIndex(); i >= 0 {
			active = p.Models[i].ID
		}
		// Имя профиля — второе поле строки: по нему режут скрипты автодополнения.
		fmt.Printf("%s%s  %s  %s\n", mark, padRight(n, w), valueOr(p.BaseURL, "-"),
			fmt.Sprintf(M.ListModels, active, len(p.Models)))
	}
	return nil
}

func padRight(s string, w int) string {
	if n := utf8.RuneCountInString(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func configShow(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf(M.UnexpectedArg, args[1])
	}
	cf, err := loadConfigFile()
	if err != nil {
		return err
	}
	target := ""
	if len(args) == 1 {
		target = args[0]
	}
	name, p, err := resolveProfile(&cf, target)
	if err != nil {
		return err
	}
	path, _ := configPath()
	yesNo := func(b bool) string {
		if b {
			return M.Yes
		}
		return M.No
	}
	fmt.Println(M.ShowFile, path)
	fmt.Println(M.ShowProfile, name)
	fmt.Println(M.ShowActive, yesNo(cf.Active == name))
	fmt.Println(M.ShowURL, valueOr(p.BaseURL, M.ShowNotSet))
	fmt.Println(M.ShowKey, maskKey(p.APIKey))
	fmt.Println(M.ShowProxy, valueOr(p.Proxy, M.ShowEnvProxy))
	fmt.Println(M.ShowReasonDef, valueOr(p.ReasoningDefault, M.ShowProvider))
	fmt.Println(M.ShowTimeout, p.execTimeout())
	fmt.Println(M.ShowMode, modeLabel(cf.mode()))
	fmt.Println(M.ShowLanguage, showLanguage(cf.Language))
	fmt.Println(M.ShowModels)
	printModelTable(p, "  ")
	return nil
}

func configUse(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf(M.NeedProfileName, "use")
	}
	name := args[0]
	_, err := updateConfig(func(cf *ConfigFile) error {
		if _, ok := cf.Profiles[name]; !ok {
			return fmt.Errorf(M.NoSuchProfileList, name, strings.Join(profileNames(*cf), ", "))
		}
		cf.Active = name
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Println(M.ActiveProfileIs, name)
	return nil
}

func configRm(args []string) error {
	pos, flags, err := scanFlags(args, nil, []string{"-y", "--yes"})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf(M.NeedProfileName, "rm")
	}
	name := pos[0]
	cf, err := loadConfigFile()
	if err != nil {
		return err
	}
	if _, ok := cf.Profiles[name]; !ok {
		return fmt.Errorf(M.NoSuchProfileList, name, strings.Join(profileNames(cf), ", "))
	}
	_, y1 := flagSet(flags, "-y")
	_, y2 := flagSet(flags, "--yes")
	if !y1 && !y2 {
		if !haveTTY() {
			return errors.New(M.RmNeedsYes)
		}
		if !confirmYN(fmt.Sprintf(M.RmAsk, name), false) {
			fmt.Println(M.Aborted)
			return nil
		}
	}
	var newActive string
	_, err = updateConfig(func(cf *ConfigFile) error {
		delete(cf.Profiles, name)
		if cf.Active == name {
			cf.Active = ""
			if len(cf.Profiles) > 0 {
				cf.Active = newestProfileName(*cf)
				newActive = cf.Active
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Println(M.RemovedIs, name)
	if newActive != "" {
		info(M.ActiveRemoved, newActive)
	}
	return nil
}

// --- clank config model ---

func cmdConfigModel(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, M.ModelUsage)
		return exitConfig
	}
	verb, rest := args[0], args[1:]
	var err error
	switch verb {
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, M.ModelUsage)
		return exitOK
	case "list":
		err = modelList(rest)
	case "add":
		err = modelAdd(rest)
	case "edit":
		err = modelEdit(rest)
	case "rm":
		err = modelSimple(verb, rest)
	case "activate":
		err = modelSimple(verb, rest)
	case "enable":
		err = modelSimple(verb, rest)
	case "disable":
		err = modelSimple(verb, rest)
	case "move":
		err = modelMove(rest)
	default:
		fail(M.UnknownSubcommand, verb)
		fmt.Fprintln(os.Stderr, M.ModelUsage)
		return exitConfig
	}
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	return exitOK
}

func profileFlag(flags []kv) string {
	name, _ := flagSet(flags, "-p")
	if v, ok := flagSet(flags, "--profile"); ok {
		name = v
	}
	return name
}

// editModels — общий каркас правки моделей: свежий конфиг, профиль из
// -p или активный, изменение, сохранение. Возвращает итоговый профиль
// для печати.
func editModels(target string, fn func(p *Profile) error) (string, Profile, error) {
	var (
		name string
		out  Profile
	)
	_, err := updateConfig(func(cf *ConfigFile) error {
		n, p, err := resolveProfile(cf, target)
		if err != nil {
			return err
		}
		if err := fn(&p); err != nil {
			return err
		}
		cf.Profiles[n] = p
		name, out = n, p
		return nil
	})
	return name, out, err
}

func modelList(args []string) error {
	pos, flags, err := scanFlags(args, []string{"-p", "--profile"}, nil)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf(M.UnexpectedArg, pos[0])
	}
	cf, err := loadConfigFile()
	if err != nil {
		return err
	}
	name, p, err := resolveProfile(&cf, profileFlag(flags))
	if err != nil {
		return err
	}
	fmt.Printf(M.ModelsOf+"\n", name)
	printModelTable(p, "")
	return nil
}

// printModelTable — нумерованный список моделей: номер годится вместо id
// во всех командах `config model`.
func printModelTable(p Profile, indent string) {
	if len(p.Models) == 0 {
		fmt.Println(indent + M.NoModelsYet)
		return
	}
	active := p.activeIndex()
	rows := [][]string{{"#", M.ColModel, M.ColTools, M.ColVision, M.ColReasoning, M.ColContext, ""}}
	for i, m := range p.Models {
		r := m.Reasoning
		if r == "" {
			if p.ReasoningDefault != "" {
				r = p.ReasoningDefault + " " + M.FromProfile
			} else {
				r = "-"
			}
		}
		note := ""
		switch {
		case m.Disabled:
			note = M.MarkDisabled
		case i == active:
			note = M.MarkActive
		}
		rows = append(rows, []string{strconv.Itoa(i + 1), m.label(), check(m.Tools), check(m.Vision), r, fmtTokens(m.Context), note})
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for j, c := range row {
			widths[j] = max(widths[j], utf8.RuneCountInString(c))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		b.WriteString(indent)
		for j, c := range row {
			if j > 0 {
				b.WriteString("  ")
			}
			if j == 0 {
				b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(c)) + c)
			} else {
				b.WriteString(padRight(c, widths[j]))
			}
		}
		fmt.Println(strings.TrimRight(b.String(), " "))
	}
}

func check(b bool) string {
	if b {
		return "✓"
	}
	return "·"
}

func modelAdd(args []string) error {
	pos, flags, err := scanFlags(args, append([]string{"-p", "--profile", "--pos"}, modelValueFlags...), nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return fmt.Errorf(M.NeedModelID, "add")
	}
	m := ModelEntry{ID: strings.TrimSpace(pos[0])}
	at := -1
	for _, f := range flags {
		switch f.k {
		case "-p", "--profile":
		case "--pos":
			n, err := strconv.Atoi(f.v)
			if err != nil || n < 1 {
				return fmt.Errorf(M.BadPos, f.v)
			}
			at = n - 1
		default:
			if err := applyModelFlag(&m, f); err != nil {
				return err
			}
		}
	}
	name, p, err := editModels(profileFlag(flags), func(p *Profile) error {
		for _, e := range p.Models {
			if e.ID == m.ID {
				return fmt.Errorf(M.ModelExists, m.ID)
			}
		}
		if at < 0 || at > len(p.Models) {
			at = len(p.Models)
		}
		p.Models = append(p.Models[:at], append([]ModelEntry{m}, p.Models[at:]...)...)
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf(M.ModelAdded+"\n", m.ID, name)
	printModelTable(p, "")
	return nil
}

func modelEdit(args []string) error {
	pos, flags, err := scanFlags(args, append([]string{"-p", "--profile", "--new-id"}, modelValueFlags...), nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf(M.NeedModelID, "edit")
	}
	changes := 0
	for _, f := range flags {
		if f.k != "-p" && f.k != "--profile" {
			changes++
		}
	}
	if changes == 0 {
		return errors.New(M.EditNothing)
	}
	_, p, err := editModels(profileFlag(flags), func(p *Profile) error {
		i, err := p.findModel(pos[0])
		if err != nil {
			return err
		}
		m := p.Models[i]
		for _, f := range flags {
			switch f.k {
			case "-p", "--profile":
			case "--new-id":
				id := strings.TrimSpace(f.v)
				if id == "" {
					return fmt.Errorf(M.NeedValueFor, f.k)
				}
				for j, e := range p.Models {
					if j != i && e.ID == id {
						return fmt.Errorf(M.ModelExists, id)
					}
				}
				m.ID = id
			default:
				if err := applyModelFlag(&m, f); err != nil {
					return err
				}
			}
		}
		p.Models[i] = m
		return nil
	})
	if err != nil {
		return err
	}
	printModelTable(p, "")
	return nil
}

// modelSimple — rm / activate / enable / disable: одна модель, без флагов
// кроме -p.
func modelSimple(verb string, args []string) error {
	pos, flags, err := scanFlags(args, []string{"-p", "--profile"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf(M.NeedModelID, verb)
	}
	_, p, err := editModels(profileFlag(flags), func(p *Profile) error {
		i, err := p.findModel(pos[0])
		if err != nil {
			return err
		}
		switch verb {
		case "rm":
			p.Models = append(p.Models[:i], p.Models[i+1:]...)
		case "activate":
			// Активная = первая в списке. Выключенную при этом включаем:
			// раз её явно сделали активной, пропускать её странно.
			m := p.Models[i]
			m.Disabled = false
			p.Models = append(p.Models[:i], p.Models[i+1:]...)
			p.Models = append([]ModelEntry{m}, p.Models...)
		case "enable":
			p.Models[i].Disabled = false
		case "disable":
			p.Models[i].Disabled = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	printModelTable(p, "")
	if len(p.Models) > 0 && p.activeIndex() < 0 {
		warn(M.AllDisabled)
	}
	return nil
}

func modelMove(args []string) error {
	pos, flags, err := scanFlags(args, []string{"-p", "--profile"}, nil)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errors.New(M.MoveUsage)
	}
	to, err := strconv.Atoi(pos[1])
	if err != nil || to < 1 {
		return fmt.Errorf(M.BadPos, pos[1])
	}
	_, p, err := editModels(profileFlag(flags), func(p *Profile) error {
		i, err := p.findModel(pos[0])
		if err != nil {
			return err
		}
		m := p.Models[i]
		p.Models = append(p.Models[:i], p.Models[i+1:]...)
		at := min(to-1, len(p.Models))
		p.Models = append(p.Models[:at], append([]ModelEntry{m}, p.Models[at:]...)...)
		return nil
	})
	if err != nil {
		return err
	}
	printModelTable(p, "")
	return nil
}

// --- clank models ---

// cmdModels печатает модели провайдера — подсказка, что вписывать в
// `config model add`. Нетекстовые (эмбеддинги, речь, картинки на выходе)
// скрыты, если провайдер отдаёт метаданные; возможности показаны тоже
// только из метаданных — проверять их нечем.
func cmdModels(args []string) int {
	pos, flags, err := scanFlags(args, []string{"-p", "--profile"}, []string{"-v", "--verbose", "-q", "--quiet", "-h", "--help", "--all"})
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	showAll := false
	for _, f := range flags {
		switch f.k {
		case "-v", "--verbose":
			verboseLevel = vVerbose
		case "-q", "--quiet":
			verboseLevel = vQuiet
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, M.ModelsHelp)
			return exitOK
		case "--all":
			showAll = true
		}
	}
	filter := strings.ToLower(strings.Join(pos, " "))

	cf, err := loadConfigFile()
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	name, p, err := resolveProfile(&cf, profileFlag(flags))
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	if p.BaseURL == "" {
		fail(M.ModelsNeedURL, name)
		return exitConfig
	}

	sp := startSpinner(M.FetchingModels)
	list, err := listModels(p)
	sp.stopSpinner()
	if err != nil {
		fail("%v", err)
		return exitAPI
	}

	var kept []remoteModel
	hidden := 0
	for _, m := range list {
		if filter != "" && !strings.Contains(strings.ToLower(m.ID+" "+m.Name), filter) {
			continue
		}
		if !showAll && !m.textOutput() {
			hidden++
			continue
		}
		kept = append(kept, m)
	}
	if len(kept) == 0 {
		if filter != "" {
			fail(M.FilterNoMatch, filter)
		} else {
			fail(M.ModelsEmptyAPI)
		}
		return exitAPI
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })

	w := 0
	for _, m := range kept {
		w = max(w, utf8.RuneCountInString(m.ID))
	}
	for _, m := range kept {
		var tags []string
		if containsString(m.SupportedParameters, "tools") {
			tags = append(tags, "tools")
		}
		if containsString(m.Architecture.InputModalities, "image") {
			tags = append(tags, "vision")
		}
		if m.Reasoning != nil && len(m.Reasoning.SupportedEfforts) > 0 {
			tags = append(tags, "reasoning:"+strings.Join(m.Reasoning.SupportedEfforts, ","))
		}
		if m.ContextLength > 0 {
			tags = append(tags, "context:"+fmtTokens(m.ContextLength))
		}
		line := m.ID
		if len(tags) > 0 {
			line = padRight(m.ID, w) + "  " + strings.Join(tags, " ")
		}
		fmt.Println(line)
	}
	if hidden > 0 {
		info(M.ModelsHidden, hidden)
	}
	return exitOK
}

// --- clank mode ---

func modeLabel(m ToolMode) string {
	switch m {
	case ModeAuto:
		return M.ModeAutoDesc
	case ModeRead:
		return M.ModeReadDesc
	}
	return M.ModeAskDesc
}

func cmdMode(args []string) int {
	if len(args) == 0 || args[0] == "show" {
		cf, err := loadConfigFile()
		if err != nil {
			fail("%v", err)
			return exitConfig
		}
		fmt.Println(M.ModeIs, modeLabel(cf.mode()))
		return exitOK
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Println(M.HelpMode)
		return exitOK
	}
	m, ok := parseMode(args[0])
	if !ok {
		fail(M.BadMode, args[0])
		return exitConfig
	}
	if _, err := updateConfig(func(cf *ConfigFile) error {
		cf.Mode = m
		return nil
	}); err != nil {
		fail("%v", err)
		return exitConfig
	}
	fmt.Println(M.ModeIs, modeLabel(m))
	return exitOK
}
