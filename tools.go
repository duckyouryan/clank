package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Схемы инструментов и ответы инструментов модели — на английском и не
// локализуются: мелкие модели надёжнее разбирают EN, а EN-схема с RU-промптом
// работает нормально. Пользователю всё показывается через M.

type toolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

const (
	shellToolName     = "run_shell_command"
	questionToolName  = "question"
	viewImageToolName = "view_image"
	listDirToolName   = "list_dir"
	readFileToolName  = "read_file"
	grepToolName      = "grep"
	writeFileToolName = "write_file"
	editFileToolName  = "edit_file"
)

// Потолки вывода: защищают контекст модели, а не фильтруют. Если упёрлись —
// модель получает пометку и сама сужает запрос.
const (
	maxListEntries  = 1000
	maxReadLines    = 2000
	maxLineRunes    = 2000
	maxReadBytes    = 256 << 10
	maxGrepMatches  = 500
	maxGrepFileSize = 10 << 20
	binarySniffSize = 8 << 10
)

func mkTool(name, desc, params string) toolDef {
	var t toolDef
	t.Type = "function"
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(params)
	return t
}

func shellTool() toolDef {
	return mkTool(shellToolName,
		"Run a shell command in the user's POSIX environment via sh -c and get its stdout/stderr/exit code. Prefer list_dir/read_file/grep for looking at files.",
		`{"type":"object","properties":{"command":{"type":"string","description":"Single-line command for sh -c"}},"required":["command"]}`)
}

func questionTool() toolDef {
	return mkTool(questionToolName,
		"Ask the user a question, offer a choice of options, or request clarification/input.",
		`{"type":"object","properties":{
			"question":{"type":"string","description":"Question text for the user"},
			"options":{"type":"array","items":{"type":"string"},"description":"List of options for the user to choose from (optional)"},
			"default":{"type":"string","description":"Default option or value on Enter (optional)"}},
		"required":["question"]}`)
}

func viewImageTool() toolDef {
	return mkTool(viewImageToolName,
		"Look at a local image file (PNG, JPEG, WebP, GIF) to analyze the picture, read text (OCR), or inspect graphics.",
		`{"type":"object","properties":{"path":{"type":"string","description":"Path to the image file (relative or absolute)"}},"required":["path"]}`)
}

func listDirTool() toolDef {
	return mkTool(listDirToolName,
		"List a directory. Directories end with '/'. With recursive=true walks the whole tree; pattern (glob on file names, e.g. '*.go') filters entries, so recursive+pattern works as find.",
		`{"type":"object","properties":{
			"path":{"type":"string","description":"Directory path, default '.'"},
			"recursive":{"type":"boolean","description":"Walk subdirectories (default false)"},
			"pattern":{"type":"string","description":"Glob on entry names, optional"}}}`)
}

func readFileTool() toolDef {
	return mkTool(readFileToolName,
		"Read a text file. Returns numbered lines. Use offset/limit for big files.",
		`{"type":"object","properties":{
			"path":{"type":"string","description":"File path"},
			"offset":{"type":"integer","description":"First line to return, 1-based (default 1)"},
			"limit":{"type":"integer","description":"Max lines to return (default 2000)"}},
		"required":["path"]}`)
}

func grepTool() toolDef {
	return mkTool(grepToolName,
		"Search file contents with a regular expression (Go RE2 syntax). Returns 'file:line:text'. Binary files are skipped.",
		`{"type":"object","properties":{
			"pattern":{"type":"string","description":"Regular expression"},
			"path":{"type":"string","description":"File or directory to search, default '.'"},
			"glob":{"type":"string","description":"Only files whose name matches this glob, e.g. '*.go'"},
			"ignore_case":{"type":"boolean","description":"Case-insensitive search"}},
		"required":["pattern"]}`)
}

func writeFileTool() toolDef {
	return mkTool(writeFileToolName,
		"Create or overwrite a file with the given content. Missing parent directories are created. Empty content creates an empty file.",
		`{"type":"object","properties":{
			"path":{"type":"string","description":"File path"},
			"content":{"type":"string","description":"Full file content"}},
		"required":["path","content"]}`)
}

func editFileTool() toolDef {
	return mkTool(editFileToolName,
		"Replace an exact string in a file. old_string must occur exactly once unless replace_all is true. Read the file first to copy old_string exactly, including whitespace.",
		`{"type":"object","properties":{
			"path":{"type":"string","description":"File path"},
			"old_string":{"type":"string","description":"Exact text to replace"},
			"new_string":{"type":"string","description":"Replacement text"},
			"replace_all":{"type":"boolean","description":"Replace every occurrence (default false)"}},
		"required":["path","old_string","new_string"]}`)
}

// toolsFor — набор инструментов для модели в данном режиме. Модель без
// tools получает пустой набор; в режиме read запись и shell не отдаются
// вовсе — модели нечего пытаться вызвать.
func toolsFor(m ModelEntry, mode ToolMode) []toolDef {
	if !m.Tools {
		return nil
	}
	tools := []toolDef{listDirTool(), readFileTool(), grepTool(), questionTool()}
	if m.Vision {
		tools = append(tools, viewImageTool())
	}
	if mode != ModeRead {
		tools = append(tools, writeFileTool(), editFileTool(), shellTool())
	}
	return tools
}

// --- пути ---

type pathInfo struct {
	shown     string // как просила модель — для показа
	real      string // абсолютный путь после Clean и разворота симлинков
	inside    bool   // внутри рабочего каталога
	sensitive bool   // ключи и конфиги — всегда с подтверждением
	exists    bool
	isDir     bool
}

var (
	cwdRealCache string
	cwdRealErr   error
	cwdRealDone  bool
)

func cwdReal() (string, error) {
	if !cwdRealDone {
		cwdRealDone = true
		wd, err := os.Getwd()
		if err != nil {
			cwdRealErr = err
		} else {
			cwdRealCache, cwdRealErr = filepath.EvalSymlinks(wd)
		}
	}
	return cwdRealCache, cwdRealErr
}

// realPath — Abs + Clean + разворот симлинков. Для несуществующего пути
// разворачивается ближайший существующий предок, остаток дописывается:
// после Clean в нём нет "..", а несуществующие компоненты симлинками
// быть не могут. Так `ln -s ~/.ssh x` и `../../` не обходят проверку.
func realPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		wd, err := cwdReal()
		if err != nil {
			return "", err
		}
		p = filepath.Join(wd, p)
	}
	p = filepath.Clean(p)

	rest := ""
	cur := p
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if rest != "" {
				r = filepath.Join(r, rest)
			}
			return r, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// within — лежит ли p внутри dir (или равен ему). Сравниваем через Rel,
// а не HasPrefix: иначе /home/a/proj-evil считался бы частью /home/a/proj.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func sensitiveDirs() []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, d := range []string{".ssh", ".gnupg"} {
			if r, err := realPath(filepath.Join(home, d)); err == nil {
				out = append(out, r)
			}
		}
	}
	if d, err := clankDir(); err == nil {
		if r, err := realPath(d); err == nil {
			out = append(out, r)
		}
	}
	return out
}

func resolvePath(p string) (pathInfo, error) {
	if strings.TrimSpace(p) == "" {
		p = "."
	}
	pi := pathInfo{shown: p}
	r, err := realPath(p)
	if err != nil {
		return pi, err
	}
	pi.real = r
	if wd, err := cwdReal(); err == nil {
		pi.inside = within(wd, r)
	}
	for _, d := range sensitiveDirs() {
		if within(d, r) {
			pi.sensitive = true
		}
	}
	if st, err := os.Stat(r); err == nil {
		pi.exists = true
		pi.isDir = st.IsDir()
	}
	return pi, nil
}

// openAt открывает os.Root на безопасной базе и возвращает путь внутри
// неё. Внутри cwd база — сам cwd: os.Root не даст выйти наружу ни через
// "..", ни через симлинк, подменённый между проверкой и операцией. Вне
// cwd (с подтверждения) база — ближайший существующий предок цели.
func openAt(pi pathInfo) (*os.Root, string, error) {
	base := ""
	if pi.inside {
		wd, err := cwdReal()
		if err != nil {
			return nil, "", err
		}
		base = wd
	} else {
		base = pi.real
		for {
			if st, err := os.Stat(base); err == nil && st.IsDir() {
				break
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	rel, err := filepath.Rel(base, pi.real)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	return root, rel, nil
}

// --- права ---

type toolEnv struct {
	mode    ToolMode
	timeout time.Duration
}

// confirmRead — чтение само внутри cwd; снаружи — с вопросом (кроме auto);
// ключи и конфиги — всегда с вопросом.
func confirmRead(env toolEnv, pi pathInfo, what string) bool {
	switch {
	case pi.sensitive:
		return confirmTool(fmt.Sprintf(M.ToolWantsSens, what), sanitizeForDisplay(pi.real))
	case !pi.inside && env.mode != ModeAuto:
		return confirmTool(fmt.Sprintf(M.ToolWantsOut, what), sanitizeForDisplay(pi.real))
	}
	info(M.ToolDoing, what, sanitizeForDisplay(pi.shown))
	return true
}

// confirmWrite — в ask всегда вопрос, в auto сам (кроме ключей и конфигов).
func confirmWrite(env toolEnv, pi pathInfo, what, preview string) bool {
	if env.mode == ModeAuto && !pi.sensitive {
		info(M.ToolDoing, what, sanitizeForDisplay(pi.shown))
		return true
	}
	title := fmt.Sprintf(M.ToolWants, what)
	if pi.sensitive {
		title = fmt.Sprintf(M.ToolWantsSens, what)
	} else if !pi.inside {
		title = fmt.Sprintf(M.ToolWantsOut, what)
	}
	body := sanitizeForDisplay(pi.real)
	if preview != "" {
		body += "\n" + indentBlock(sanitizeForDisplay(preview), "  ")
	}
	return confirmTool(title, body)
}

// --- исполнение ---

func runToolCall(env toolEnv, tc toolCall) (chatMessage, *chatMessage, toolVerdict) {
	mk := func(content string) chatMessage {
		return chatMessage{Role: "tool", ToolCallID: tc.ID, Content: content}
	}
	args := []byte(tc.Function.Arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = []byte("{}")
	}

	writeTool := tc.Function.Name == shellToolName || tc.Function.Name == writeFileToolName || tc.Function.Name == editFileToolName
	if writeTool && env.mode == ModeRead {
		warn(M.ToolReadOnly, sanitizeForDisplay(tc.Function.Name))
		return mk("error: " + tc.Function.Name + " is not available in read-only mode; tell the user to rerun with --mode ask"), nil, verdictInvalid
	}

	switch tc.Function.Name {
	case shellToolName:
		msg, v := runShellTool(env, args, mk)
		return msg, nil, v
	case questionToolName, "input":
		msg, v := runQuestionTool(args, mk)
		return msg, nil, v
	case viewImageToolName:
		return runViewImageTool(env, args, mk)
	case listDirToolName:
		msg, v := runListDir(env, args, mk)
		return msg, nil, v
	case readFileToolName:
		msg, v := runReadFile(env, args, mk)
		return msg, nil, v
	case grepToolName:
		msg, v := runGrep(env, args, mk)
		return msg, nil, v
	case writeFileToolName:
		msg, v := runWriteFile(env, args, mk)
		return msg, nil, v
	case editFileToolName:
		msg, v := runEditFile(env, args, mk)
		return msg, nil, v
	default:
		warn(M.UnknownTool, sanitizeForDisplay(tc.Function.Name))
		return mk(fmt.Sprintf("error: tool %q does not exist", tc.Function.Name)), nil, verdictInvalid
	}
}

func badArgs(name string, err error, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	warn(M.ToolArgsBad, name, err)
	return mk(fmt.Sprintf("error: arguments of %s must be a JSON object matching its schema: %v", name, err)), verdictInvalid
}

func declined(mk func(string) chatMessage) (chatMessage, toolVerdict) {
	// Формулировка жёсткая намеренно: мелкие модели иначе рапортуют
	// «файл создан», получив отказ.
	return mk("NOT DONE: the user declined this action, nothing was changed. Do not claim it was done; tell the user it was declined or ask what to do instead."), verdictDeclined
}

func toolErr(mk func(string) chatMessage, format string, a ...any) (chatMessage, toolVerdict) {
	return mk("error: " + fmt.Sprintf(format, a...)), verdictInvalid
}

func runShellTool(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(shellToolName, err, mk)
	}
	cmdStr := strings.TrimSpace(a.Command)
	if cmdStr == "" {
		warn(M.ShellEmpty)
		return toolErr(mk, "command is empty, nothing was run")
	}

	if env.mode == ModeAuto {
		info(M.RunAuto, sanitizeForDisplay(cmdStr))
	} else if !confirmTool(M.ModelWantsRun, indentBlock(sanitizeForDisplay(cmdStr), "    ")) {
		return declined(mk)
	}

	// Маркеры в stderr, сам вывод команды — в stdout: при `clank ... > файл`
	// вывод выполненных команд считается такой же частью результата, как
	// и ответ модели.
	fmt.Fprintln(os.Stderr, M.OutputMark)
	res := execShell(cmdStr, env.timeout)
	fmt.Fprintf(os.Stderr, "--- exit %d ---\n", res.exitCode)

	var b strings.Builder
	switch {
	case res.interrupted:
		b.WriteString("command interrupted by the user (Ctrl-C)\n")
	case res.timedOut:
		b.WriteString("command killed by timeout\n")
	}
	fmt.Fprintf(&b, "exit code: %d\n%s", res.exitCode, res.output)

	if res.interrupted {
		return mk(b.String()), verdictInterrupted
	}
	return mk(b.String()), verdictOK
}

func runQuestionTool(args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Question string   `json:"question"`
		Prompt   string   `json:"prompt"`
		Text     string   `json:"text"`
		Options  []string `json:"options"`
		Default  string   `json:"default"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(questionToolName, err, mk)
	}
	q := strings.TrimSpace(a.Question)
	if q == "" {
		q = strings.TrimSpace(a.Prompt)
	}
	if q == "" {
		q = strings.TrimSpace(a.Text)
	}
	if q == "" {
		warn(M.AskEmptyQ)
		return toolErr(mk, "question is empty")
	}
	ans, ok := promptQuestion(q, a.Options, strings.TrimSpace(a.Default))
	if !ok {
		if !haveTTY() && a.Default == "" {
			return toolErr(mk, "no terminal to ask the user")
		}
		return mk("the user cancelled the input (Ctrl-C / EOF)"), verdictInterrupted
	}
	return mk(ans), verdictOK
}

func runViewImageTool(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, *chatMessage, toolVerdict) {
	var a struct {
		Path string `json:"path"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		m, v := badArgs(viewImageToolName, err, mk)
		return m, nil, v
	}
	p := strings.TrimSpace(a.Path)
	if p == "" {
		p = strings.TrimSpace(a.File)
	}
	if p == "" {
		m, v := toolErr(mk, "path is empty")
		return m, nil, v
	}
	pi, err := resolvePath(p)
	if err != nil {
		m, v := toolErr(mk, "%v", err)
		return m, nil, v
	}
	if !confirmRead(env, pi, M.WhatView) {
		m, v := declined(mk)
		return m, nil, v
	}
	root, rel, err := openAt(pi)
	if err != nil {
		m, v := toolErr(mk, "%v", err)
		return m, nil, v
	}
	defer root.Close()
	data, err := root.ReadFile(rel)
	if err != nil {
		m, v := toolErr(mk, "cannot read %s: %v", p, err)
		return m, nil, v
	}
	dataURL, err := encodeImageData(p, data)
	if err != nil {
		m, v := toolErr(mk, "%v", err)
		return m, nil, v
	}
	followUp := &chatMessage{
		Role: "user",
		Content: []contentPart{
			{Type: "text", Text: "Image " + p + ":"},
			{Type: "image_url", ImageURL: &imageURL{URL: dataURL}},
		},
	}
	return mk("image " + p + " loaded, it follows in the next message"), followUp, verdictOK
}

func runListDir(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Pattern   string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(listDirToolName, err, mk)
	}
	if a.Pattern != "" {
		if _, err := path.Match(a.Pattern, ""); err != nil {
			return toolErr(mk, "bad pattern: %v", err)
		}
	}
	pi, err := resolvePath(a.Path)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if !pi.exists {
		return toolErr(mk, "%s does not exist", pi.shown)
	}
	if !pi.isDir {
		return toolErr(mk, "%s is not a directory", pi.shown)
	}
	if !confirmRead(env, pi, M.WhatList) {
		return declined(mk)
	}
	root, rel, err := openAt(pi)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer root.Close()
	fsys := root.FS()
	start := filepath.ToSlash(rel)

	var lines []string
	more := 0
	add := func(p string, d fs.DirEntry) {
		if a.Pattern != "" {
			if ok, _ := path.Match(a.Pattern, d.Name()); !ok {
				return
			}
		}
		if len(lines) >= maxListEntries {
			more++
			return
		}
		if d.IsDir() {
			p += "/"
		} else if d.Type()&fs.ModeSymlink != 0 {
			p += " -> (symlink)"
		} else if fi, err := d.Info(); err == nil {
			p = fmt.Sprintf("%s  %d", p, fi.Size())
		}
		lines = append(lines, p)
	}

	if a.Recursive {
		err = fs.WalkDir(fsys, start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s: error: %v", p, err))
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if p == start {
				return nil
			}
			r := strings.TrimPrefix(strings.TrimPrefix(p, start), "/")
			if start == "." {
				r = p
			}
			add(r, d)
			return nil
		})
	} else {
		var entries []fs.DirEntry
		entries, err = fs.ReadDir(fsys, start)
		for _, d := range entries {
			add(d.Name(), d)
		}
	}
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if len(lines) == 0 {
		return mk("(empty)"), verdictOK
	}
	out := strings.Join(lines, "\n")
	if more > 0 {
		out += fmt.Sprintf("\n... %d more entries not shown; narrow the path or use pattern", more)
	}
	return mk(out), verdictOK
}

func looksBinary(b []byte) bool {
	if len(b) > binarySniffSize {
		b = b[:binarySniffSize]
	}
	return bytes.IndexByte(b, 0) >= 0
}

func runReadFile(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(readFileToolName, err, mk)
	}
	if strings.TrimSpace(a.Path) == "" {
		return toolErr(mk, "path is empty")
	}
	if a.Offset < 1 {
		a.Offset = 1
	}
	if a.Limit <= 0 || a.Limit > maxReadLines {
		a.Limit = maxReadLines
	}
	pi, err := resolvePath(a.Path)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if !pi.exists {
		return toolErr(mk, "%s does not exist", pi.shown)
	}
	if pi.isDir {
		return toolErr(mk, "%s is a directory, use list_dir", pi.shown)
	}
	if !confirmRead(env, pi, M.WhatRead) {
		return declined(mk)
	}
	root, rel, err := openAt(pi)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer f.Close()

	rd := bufio.NewReader(f)
	head, _ := rd.Peek(binarySniffSize)
	if looksBinary(head) {
		return toolErr(mk, "%s looks like a binary file", pi.shown)
	}

	var b strings.Builder
	n, shown, truncated := 0, 0, false
	for {
		line, err := rd.ReadString('\n')
		if line == "" && err != nil {
			break
		}
		n++
		if n >= a.Offset && shown < a.Limit && !truncated {
			line = strings.TrimRight(line, "\r\n")
			if utf8.RuneCountInString(line) > maxLineRunes {
				line = truncateRunes(line, maxLineRunes)
			}
			fmt.Fprintf(&b, "%6d\t%s\n", n, line)
			shown++
			if b.Len() > maxReadBytes {
				truncated = true
			}
		}
		if err != nil {
			break
		}
	}
	if n == 0 {
		return mk("(empty file)"), verdictOK
	}
	last := a.Offset + shown - 1
	if shown == 0 {
		return mk(fmt.Sprintf("(file has %d lines, offset %d is past the end)", n, a.Offset)), verdictOK
	}
	if last < n {
		fmt.Fprintf(&b, "... (shown lines %d-%d of %d; use offset to read more)\n", a.Offset, last, n)
	}
	return mk(b.String()), verdictOK
}

func runGrep(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(grepToolName, err, mk)
	}
	if a.Pattern == "" {
		return toolErr(mk, "pattern is empty")
	}
	expr := a.Pattern
	if a.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return toolErr(mk, "bad regexp: %v", err)
	}
	if a.Glob != "" {
		if _, err := path.Match(a.Glob, ""); err != nil {
			return toolErr(mk, "bad glob: %v", err)
		}
	}
	pi, err := resolvePath(a.Path)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if !pi.exists {
		return toolErr(mk, "%s does not exist", pi.shown)
	}
	if !confirmRead(env, pi, M.WhatGrep) {
		return declined(mk)
	}
	root, rel, err := openAt(pi)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer root.Close()
	fsys := root.FS()
	start := filepath.ToSlash(rel)

	var out []string
	more := 0
	searchFile := func(p, shown string) {
		f, err := fsys.Open(p)
		if err != nil {
			return
		}
		defer f.Close()
		if st, err := f.Stat(); err != nil || st.Size() > maxGrepFileSize {
			return
		}
		rd := bufio.NewReader(f)
		if head, _ := rd.Peek(binarySniffSize); looksBinary(head) {
			return
		}
		n := 0
		for {
			line, err := rd.ReadString('\n')
			if line == "" && err != nil {
				return
			}
			n++
			line = strings.TrimRight(line, "\r\n")
			if re.MatchString(line) {
				if len(out) >= maxGrepMatches {
					more++
				} else {
					out = append(out, fmt.Sprintf("%s:%d:%s", shown, n, truncateRunes(line, 300)))
				}
			}
			if err != nil {
				return
			}
		}
	}

	if !pi.isDir {
		searchFile(start, pi.shown)
	} else {
		err = fs.WalkDir(fsys, start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			if a.Glob != "" {
				if ok, _ := path.Match(a.Glob, d.Name()); !ok {
					return nil
				}
			}
			shown := p
			if start != "." {
				shown = strings.TrimPrefix(strings.TrimPrefix(p, start), "/")
				shown = path.Join(filepath.ToSlash(pi.shown), shown)
			}
			searchFile(p, shown)
			return nil
		})
		if err != nil {
			return toolErr(mk, "%v", err)
		}
	}
	if len(out) == 0 {
		return mk("no matches"), verdictOK
	}
	res := strings.Join(out, "\n")
	if more > 0 {
		res += fmt.Sprintf("\n... %d more matches not shown; narrow the pattern, path or glob", more)
	}
	return mk(res), verdictOK
}

// previewLines — первые n строк текста для подтверждения.
func previewLines(s string, n int, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	extra := 0
	if len(lines) > n {
		extra = len(lines) - n
		lines = lines[:n]
	}
	for i, l := range lines {
		lines[i] = prefix + truncateRunes(l, 200)
	}
	out := strings.Join(lines, "\n")
	if extra > 0 {
		out += fmt.Sprintf("\n"+M.PreviewMore, extra)
	}
	return out
}

func runWriteFile(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(writeFileToolName, err, mk)
	}
	if strings.TrimSpace(a.Path) == "" {
		return toolErr(mk, "path is empty")
	}
	content := ""
	if a.Content != nil {
		content = *a.Content
	}
	pi, err := resolvePath(a.Path)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if pi.isDir {
		return toolErr(mk, "%s is a directory", pi.shown)
	}

	var preview string
	perm := os.FileMode(0644)
	if pi.exists {
		st, _ := os.Stat(pi.real)
		if st != nil {
			perm = st.Mode().Perm()
			preview = fmt.Sprintf(M.PreviewOverwrite, st.Size(), len(content))
		}
	} else {
		preview = fmt.Sprintf(M.PreviewNew, len(content))
	}
	if content != "" {
		preview += "\n" + previewLines(content, 20, "| ")
	}
	if !confirmWrite(env, pi, M.WhatWrite, preview) {
		return declined(mk)
	}

	root, rel, err := openAt(pi)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer root.Close()
	if dir := filepath.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0755); err != nil {
			return toolErr(mk, "%v", err)
		}
	}
	if err := root.WriteFile(rel, []byte(content), perm); err != nil {
		return toolErr(mk, "%v", err)
	}
	return mk(fmt.Sprintf("wrote %d bytes to %s", len(content), pi.shown)), verdictOK
}

func runEditFile(env toolEnv, args []byte, mk func(string) chatMessage) (chatMessage, toolVerdict) {
	var a struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return badArgs(editFileToolName, err, mk)
	}
	if strings.TrimSpace(a.Path) == "" {
		return toolErr(mk, "path is empty")
	}
	if a.OldString == "" {
		return toolErr(mk, "old_string is empty; use write_file to create or overwrite a file")
	}
	if a.OldString == a.NewString {
		return toolErr(mk, "old_string and new_string are identical")
	}
	pi, err := resolvePath(a.Path)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	if !pi.exists || pi.isDir {
		return toolErr(mk, "%s is not an existing file", pi.shown)
	}

	// Читаем до подтверждения: показать человеку дифф и не спрашивать
	// зря, если old_string не найден. Чтение идёт через тот же os.Root.
	root, rel, err := openAt(pi)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	defer root.Close()
	data, err := root.ReadFile(rel)
	if err != nil {
		return toolErr(mk, "%v", err)
	}
	text := string(data)
	count := strings.Count(text, a.OldString)
	switch {
	case count == 0:
		return toolErr(mk, "old_string not found in %s; read the file and copy the text exactly", pi.shown)
	case count > 1 && !a.ReplaceAll:
		return toolErr(mk, "old_string occurs %d times in %s; add surrounding context to make it unique or set replace_all", count, pi.shown)
	}

	preview := previewLines(a.OldString, 15, "- ") + "\n" + previewLines(a.NewString, 15, "+ ")
	if count > 1 {
		preview = fmt.Sprintf(M.PreviewReplaces, count) + "\n" + preview
	}
	if !confirmWrite(env, pi, M.WhatEdit, preview) {
		return declined(mk)
	}

	var updated string
	if a.ReplaceAll {
		updated = strings.ReplaceAll(text, a.OldString, a.NewString)
	} else {
		updated = strings.Replace(text, a.OldString, a.NewString, 1)
	}
	perm := os.FileMode(0644)
	if st, err := root.Stat(rel); err == nil {
		perm = st.Mode().Perm()
	}
	if err := root.WriteFile(rel, []byte(updated), perm); err != nil {
		return toolErr(mk, "%v", err)
	}
	return mk(fmt.Sprintf("replaced %d occurrence(s) in %s", count, pi.shown)), verdictOK
}
