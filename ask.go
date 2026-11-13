package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type outcome int

const (
	outcomeAnswer outcome = iota
	outcomeInterrupted
	outcomeEmpty
)

type toolVerdict int

const (
	verdictOK toolVerdict = iota
	verdictInvalid
	verdictDeclined
	verdictInterrupted
)

func readStdin() string {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		warn(M.StdinReadFail, err)
	}
	return strings.TrimRight(string(data), "\n")
}

// flagValue разбирает флаг со значением в двух формах: `--x v` и `--x=v`.
// ok=false — это не этот флаг; err — флаг есть, а значения нет.
func flagValue(args []string, i *int, short, long string) (val string, ok bool, err error) {
	a := args[*i]
	if strings.HasPrefix(a, long+"=") {
		return strings.TrimPrefix(a, long+"="), true, nil
	}
	if a != long && (short == "" || a != short) {
		return "", false, nil
	}
	if *i+1 >= len(args) {
		return "", true, fmt.Errorf(M.FlagNeedsValue, a)
	}
	*i++
	return args[*i], true, nil
}

func cmdAsk(args []string) int {
	var (
		wantContext bool
		resume      bool
		rest        []string
		literal     bool
		imagePaths  []string
		profileFlag string
		modelFlag   string
		modeFlag    string
		reasonFlag  string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if literal {
			rest = append(rest, a)
			continue
		}
		var handled bool
		for _, f := range []struct {
			short, long string
			dst         *string
			multi       *[]string
		}{
			{"-i", "--image", nil, &imagePaths},
			{"-p", "--profile", &profileFlag, nil},
			{"-m", "--model", &modelFlag, nil},
			{"", "--mode", &modeFlag, nil},
			{"", "--reasoning", &reasonFlag, nil},
		} {
			v, ok, err := flagValue(args, &i, f.short, f.long)
			if err != nil {
				fail("%v", err)
				return exitConfig
			}
			if ok {
				if f.multi != nil {
					*f.multi = append(*f.multi, v)
				} else {
					*f.dst = v
				}
				handled = true
				break
			}
		}
		if handled {
			continue
		}
		switch {
		case a == "--":
			literal = true
		case a == "-c" || a == "--context":
			wantContext = true
		case a == "-r" || a == "--resume":
			resume = true
		case a == "-v" || a == "--verbose":
			verboseLevel = vVerbose
		case a == "-q" || a == "--quiet":
			verboseLevel = vQuiet
		case a == "-h" || a == "--help":
			usage()
			return exitOK
		case strings.HasPrefix(a, "-") && len(a) > 1:
			fail(M.UnknownFlag, a)
			return exitConfig
		default:
			rest = append(rest, a)
		}
	}

	question := strings.Join(rest, " ")
	if strings.TrimSpace(question) == "" {
		fail(M.NothingToAsk)
		fmt.Fprintln(os.Stderr, M.AskUsage)
		return exitConfig
	}

	cf, err := loadConfigFile()
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	profileName, profile, err := resolveProfile(&cf, profileFlag)
	if err != nil {
		fail("%v", err)
		return exitConfig
	}
	if err := profile.validate(); err != nil {
		fail("%v", err)
		return exitConfig
	}

	mode := cf.mode()
	if modeFlag != "" {
		m, ok := parseMode(modeFlag)
		if !ok {
			fail(M.BadMode, modeFlag)
			return exitConfig
		}
		mode = m
	}
	if reasonFlag != "" {
		if err := checkReasoningValue(reasonFlag); err != nil {
			fail("%v", err)
			return exitConfig
		}
	}

	// Цепочка — включённые модели по порядку. -m ставит выбранную первой
	// (даже выключенную: явный выбор важнее), остальные остаются запасными.
	chain := profile.enabledModels()
	if modelFlag != "" {
		idx, err := profile.findModel(modelFlag)
		if err != nil {
			fail("%v", err)
			return exitConfig
		}
		chosen := profile.Models[idx]
		next := []ModelEntry{chosen}
		for _, m := range chain {
			if m.ID != chosen.ID {
				next = append(next, m)
			}
		}
		chain = next
	}
	ids := make([]string, len(chain))
	for i, m := range chain {
		ids[i] = m.ID
	}
	detail(M.ProfileDetail, profileName, strings.Join(ids, " → "), mode)

	gcSessions()

	var context string
	if wantContext && isTTY(os.Stdin) {
		// Иначе выглядит как зависание: программа молча ждёт EOF.
		info(M.ReadingStdin)
	}
	if wantContext || !isTTY(os.Stdin) {
		context = readStdin()
		detail(M.StdinBytes, len(context))
	}

	userContent := question
	if context != "" {
		userContent = fmt.Sprintf(M.ContextWrap, context, question)
	}

	var history []chatMessage
	if resume {
		history = loadSession(profileName)
	}

	var userMsg chatMessage
	if len(imagePaths) > 0 {
		parts := []contentPart{{Type: "text", Text: userContent}}
		for _, imgPath := range imagePaths {
			dataURL, err := encodeImageFile(imgPath)
			if err != nil {
				fail("%v", err)
				return exitConfig
			}
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: dataURL}})
			detail(M.ImageAttached, imgPath, len(dataURL))
		}
		userMsg = chatMessage{Role: "user", Content: parts}
	} else {
		userMsg = chatMessage{Role: "user", Content: userContent}
	}

	// convo — диалог без system-сообщения: и на диск (system всегда
	// пересобирается — cwd/дата могли измениться), и в запрос, где system
	// собирается под возможности конкретной модели цепочки.
	convo := make([]chatMessage, 0, len(history)+4)
	convo = append(convo, history...)
	convo = append(convo, userMsg)

	// Сессию сохраняем на любом выходе, в том числе аварийном: иначе
	// упавшая на пятом шаге сеть уносит с собой журнал уже выполненных
	// команд, и -r восстанавливает состояние «до начала».
	finish := func(code int) int {
		if err := saveSession(profileName, convo); err != nil {
			warn(M.SessionSaveErr, err)
		}
		return code
	}

	build := func(m ModelEntry) ([]chatMessage, reqOpts) {
		tools := toolsFor(m, mode)
		sys := chatMessage{Role: "system", Content: buildSystemPrompt(len(tools) > 0, m.Vision, mode)}
		msgs := make([]chatMessage, 0, len(convo)+1)
		msgs = append(msgs, sys)
		msgs = append(msgs, convo...)
		if !m.Vision && hasImages(msgs) {
			msgs = stripImages(msgs)
			noteImagesDropped()
		}
		return msgs, reqOpts{tools: tools, reasoning: profile.effectiveReasoning(m, reasonFlag)}
	}
	env := toolEnv{mode: mode, timeout: profile.execTimeout()}

	var (
		final        string
		result       = outcomeAnswer
		finishReason string
	)

loop:
	for i := 0; ; i++ {
		// Вызовы инструментов в истории модель без tools не поймёт, а
		// строгий сервер отвергнет — такие модели дальше не участвуют.
		if hasToolHistory(convo) {
			var kept []ModelEntry
			for _, m := range chain {
				if m.Tools {
					kept = append(kept, m)
				}
			}
			if len(kept) == 0 {
				fail(M.NoToolModels)
				return finish(exitConfig)
			}
			chain = kept
		}

		sp := startSpinner(M.Thinking)
		res, idx, err := chatWithFallback(profile, chain, build)
		sp.stopSpinner()
		if ae, ok := err.(*apiError); ok && ae.kind == errNoTools {
			next, ok := handleNoTools(profileName, chain, ae)
			if !ok {
				return finish(exitAPI)
			}
			chain = next
			continue
		}
		if err != nil {
			fail("%v", err)
			return finish(exitAPI)
		}
		// Фоллбэк липкий: ответила запасная — дальше в этом запуске
		// начинаем с неё, а не ждём снова таймаутов упавшей.
		chain = rotateChain(chain, idx)
		detail(M.StepDetail, i+1, res.model, valueOr(res.finishReason, "-"))

		convo = append(convo, res.msg)
		finishReason = res.finishReason

		if len(res.msg.ToolCalls) == 0 {
			final = res.msg.Text()
			if strings.TrimSpace(final) == "" {
				result = outcomeEmpty
			}
			break
		}

		for j, tc := range res.msg.ToolCalls {
			toolMsg, followUp, verdict := runToolCall(env, tc)
			convo = append(convo, toolMsg)
			if followUp != nil {
				convo = append(convo, *followUp)
			}
			if verdict == verdictInterrupted {
				// Каждому tool_call нужен ответ, иначе строгие серверы
				// отвергнут историю при -r: закрываем оставшиеся вызовы.
				for _, rest := range res.msg.ToolCalls[j+1:] {
					convo = append(convo, chatMessage{Role: "tool", ToolCallID: rest.ID,
						Content: "not run: the user interrupted the previous step"})
				}
				final = res.msg.Text()
				result = outcomeInterrupted
				break loop
			}
		}
	}

	switch finishReason {
	case "length":
		warn(M.AnswerCut)
	case "content_filter":
		warn(M.ContentFiltered)
	}

	code := exitOK
	switch result {
	case outcomeInterrupted:
		info(M.Interrupted)
		if strings.TrimSpace(final) == "" {
			final = M.OutcomeIntr
		}
		code = exitDeclined
	case outcomeEmpty:
		final = M.OutcomeEmpty
		code = exitNoAnswer
	}

	fmt.Println(final)
	return finish(code)
}

func hasToolHistory(convo []chatMessage) bool {
	for _, m := range convo {
		if m.Role == "tool" || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// handleNoTools: сервер сказал, что модель не умеет инструменты. Отмечаем
// это в конфиге и предлагаем другую модель цепочки с tools; без
// терминала или без вариантов — стоп.
func handleNoTools(profileName string, chain []ModelEntry, ae *apiError) ([]ModelEntry, bool) {
	if _, err := updateConfig(func(cf *ConfigFile) error {
		p, ok := cf.Profiles[profileName]
		if !ok {
			return nil
		}
		for i := range p.Models {
			if p.Models[i].ID == ae.model {
				p.Models[i].Tools = false
			}
		}
		cf.Profiles[profileName] = p
		return nil
	}); err != nil {
		warn("%v", err)
	} else {
		warn(M.NoToolsMarked, ae.model)
	}

	var alts []ModelEntry
	for _, m := range chain {
		if m.Tools && m.ID != ae.model {
			alts = append(alts, m)
		}
	}
	if len(alts) == 0 || !haveTTY() {
		fail("%v", ae)
		return nil, false
	}
	f, _, _ := ttyIO()
	fmt.Fprintln(f, M.PickToolModel)
	for i, m := range alts {
		fmt.Fprintf(f, "  %d) %s\n", i+1, sanitizeForDisplay(m.label()))
	}
	line, ok := askLine(M.PickPrompt)
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if !ok || err != nil || n < 1 || n > len(alts) {
		return nil, false
	}
	picked := alts[n-1]
	out := []ModelEntry{picked}
	for _, m := range chain {
		if m.ID != picked.ID && m.ID != ae.model {
			out = append(out, m)
		}
	}
	return out, true
}
