package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox — временный HOME и рабочий каталог внутри него; кэш cwd
// сбрасывается, чтобы resolvePath видел новый каталог.
func sandbox(t *testing.T) (home, work string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(base, "home")
	work = filepath.Join(base, "work")
	for _, d := range []string{home, work, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Chdir(work)
	cwdRealDone = false
	t.Cleanup(func() { cwdRealDone = false })
	applyLang(LangEN)
	return home, work
}

func call(t *testing.T, env toolEnv, name string, args any) (string, toolVerdict) {
	t.Helper()
	b, _ := json.Marshal(args)
	var tc toolCall
	tc.ID = "c1"
	tc.Function.Name = name
	tc.Function.Arguments = string(b)
	msg, _, v := runToolCall(env, tc)
	return msg.Text(), v
}

func TestWithin(t *testing.T) {
	cases := []struct {
		dir, p string
		want   bool
	}{
		{"/a/proj", "/a/proj", true},
		{"/a/proj", "/a/proj/x/y", true},
		{"/a/proj", "/a/proj-evil", false},
		{"/a/proj", "/a/projx/file", false},
		{"/a/proj", "/a", false},
		{"/a/proj", "/a/proj/../other", false},
		{"/a/proj", "/a/proj/..foo", true}, // имя файла, начинающееся с "..", — внутри
	}
	for _, c := range cases {
		if got := within(c.dir, filepath.Clean(c.p)); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.dir, c.p, got, c.want)
		}
	}
}

func TestResolvePathEscapes(t *testing.T) {
	home, work := sandbox(t)
	outside := filepath.Join(filepath.Dir(work), "outside.txt")
	os.WriteFile(outside, []byte("x"), 0600)
	os.WriteFile(filepath.Join(home, ".ssh", "id_test"), []byte("key"), 0600)
	// симлинки изнутри наружу
	os.Symlink(outside, filepath.Join(work, "link-out"))
	os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(work, "keys"))
	os.MkdirAll(filepath.Join(work, "sub"), 0755)

	check := func(p string, inside, sensitive bool) {
		t.Helper()
		pi, err := resolvePath(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if pi.inside != inside || pi.sensitive != sensitive {
			t.Errorf("%s → real=%s inside=%v sensitive=%v, want inside=%v sensitive=%v",
				p, pi.real, pi.inside, pi.sensitive, inside, sensitive)
		}
	}
	check(".", true, false)
	check("sub/new/file.txt", true, false) // несуществующий путь внутри
	check("../outside.txt", false, false)
	check("sub/../../outside.txt", false, false)
	check("link-out", false, false)
	check("keys/id_test", false, true)
	check("keys/nonexistent", false, true)
	check("~/.ssh/id_test", false, true)
	check(outside, false, false)
}

func TestReadToolsInsideAndDeclinedOutside(t *testing.T) {
	_, work := sandbox(t)
	os.MkdirAll(filepath.Join(work, "a", "b"), 0755)
	os.WriteFile(filepath.Join(work, "a", "b", "f.go"), []byte("package b\nfunc Secret() int { return 7 }\n"), 0644)
	os.WriteFile(filepath.Join(work, "top.txt"), []byte("one\ntwo\nthree\n"), 0644)
	os.WriteFile(filepath.Join(work, "bin.dat"), []byte{1, 0, 2}, 0644)
	os.WriteFile(filepath.Join(filepath.Dir(work), "outside.txt"), []byte("secret"), 0644)
	os.Symlink(filepath.Join(filepath.Dir(work), "outside.txt"), filepath.Join(work, "link-out"))

	env := toolEnv{mode: ModeAsk}

	out, v := call(t, env, listDirToolName, map[string]any{"path": ".", "recursive": true, "pattern": "*.go"})
	if v != verdictOK || !strings.Contains(out, "a/b/f.go") || strings.Contains(out, "top.txt") {
		t.Errorf("list_dir recursive+pattern: %v %q", v, out)
	}
	out, _ = call(t, env, listDirToolName, map[string]any{})
	if !strings.Contains(out, "a/") || !strings.Contains(out, "top.txt") {
		t.Errorf("list_dir default: %q", out)
	}

	out, v = call(t, env, readFileToolName, map[string]any{"path": "top.txt", "offset": 2, "limit": 1})
	if v != verdictOK || !strings.Contains(out, "2\ttwo") || strings.Contains(out, "three\n") || !strings.Contains(out, "of 3") {
		t.Errorf("read_file offset/limit: %q", out)
	}
	out, v = call(t, env, readFileToolName, map[string]any{"path": "bin.dat"})
	if v != verdictInvalid || !strings.Contains(out, "binary") {
		t.Errorf("read_file binary: %v %q", v, out)
	}

	out, v = call(t, env, grepToolName, map[string]any{"pattern": "secret", "ignore_case": true, "glob": "*.go"})
	if v != verdictOK || !strings.Contains(out, "a/b/f.go:2:") {
		t.Errorf("grep: %v %q", v, out)
	}

	// Без терминала подтвердить нельзя — чтение снаружи отклоняется,
	// в том числе через симлинк и через "..".
	if haveTTY() {
		t.Skip("есть управляющий терминал — проверка отказа без TTY неприменима")
	}
	for _, p := range []string{"../outside.txt", "link-out", "a/../../outside.txt"} {
		out, v = call(t, env, readFileToolName, map[string]any{"path": p})
		if v != verdictDeclined || strings.Contains(out, "secret") {
			t.Errorf("read_file %s outside cwd must be declined without tty: %v %q", p, v, out)
		}
	}
}

func TestWriteEditAuto(t *testing.T) {
	_, work := sandbox(t)
	env := toolEnv{mode: ModeAuto}

	out, v := call(t, env, writeFileToolName, map[string]any{"path": "deep/dir/new.txt", "content": "hello world\nhello again\n"})
	if v != verdictOK {
		t.Fatalf("write_file: %v %q", v, out)
	}
	data, _ := os.ReadFile(filepath.Join(work, "deep", "dir", "new.txt"))
	if string(data) != "hello world\nhello again\n" {
		t.Fatalf("content: %q", data)
	}

	out, v = call(t, env, editFileToolName, map[string]any{"path": "deep/dir/new.txt", "old_string": "hello", "new_string": "bye"})
	if v != verdictInvalid || !strings.Contains(out, "2 times") {
		t.Errorf("edit ambiguous must fail: %v %q", v, out)
	}
	out, v = call(t, env, editFileToolName, map[string]any{"path": "deep/dir/new.txt", "old_string": "nothere", "new_string": "x"})
	if v != verdictInvalid || !strings.Contains(out, "not found") {
		t.Errorf("edit missing must fail: %v %q", v, out)
	}
	out, v = call(t, env, editFileToolName, map[string]any{"path": "deep/dir/new.txt", "old_string": "hello world", "new_string": "bye world"})
	if v != verdictOK {
		t.Fatalf("edit unique: %v %q", v, out)
	}
	out, v = call(t, env, editFileToolName, map[string]any{"path": "deep/dir/new.txt", "old_string": "hello", "new_string": "ciao", "replace_all": true})
	if v != verdictOK {
		t.Fatalf("edit replace_all: %v %q", v, out)
	}
	data, _ = os.ReadFile(filepath.Join(work, "deep", "dir", "new.txt"))
	if string(data) != "bye world\nciao again\n" {
		t.Errorf("after edits: %q", data)
	}

	// Пустой content — пустой файл.
	if _, v := call(t, env, writeFileToolName, map[string]any{"path": "empty.txt", "content": ""}); v != verdictOK {
		t.Errorf("empty write: %v", v)
	}
	if st, err := os.Stat(filepath.Join(work, "empty.txt")); err != nil || st.Size() != 0 {
		t.Errorf("empty file: %v %v", st, err)
	}
}

func TestSensitiveAlwaysConfirmedEvenInAuto(t *testing.T) {
	home, _ := sandbox(t)
	if haveTTY() {
		t.Skip("есть управляющий терминал")
	}
	os.WriteFile(filepath.Join(home, ".ssh", "id_x"), []byte("PRIVATE"), 0600)
	env := toolEnv{mode: ModeAuto}
	out, v := call(t, env, readFileToolName, map[string]any{"path": "~/.ssh/id_x"})
	if v != verdictDeclined || strings.Contains(out, "PRIVATE") {
		t.Errorf("sensitive read in auto must need confirmation: %v %q", v, out)
	}
	out, v = call(t, env, writeFileToolName, map[string]any{"path": "~/.ssh/authorized_keys", "content": "evil"})
	if v != verdictDeclined {
		t.Errorf("sensitive write in auto must need confirmation: %v %q", v, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); err == nil {
		t.Errorf("file was written")
	}
}

func TestReadModeRefusesWrites(t *testing.T) {
	_, work := sandbox(t)
	env := toolEnv{mode: ModeRead}
	for _, name := range []string{writeFileToolName, editFileToolName, shellToolName} {
		_, v := call(t, env, name, map[string]any{"path": "x.txt", "content": "x", "command": "touch x.txt", "old_string": "a", "new_string": "b"})
		if v != verdictInvalid {
			t.Errorf("%s in read mode: verdict %v", name, v)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "x.txt")); err == nil {
		t.Errorf("file created in read mode")
	}
}

func TestToolsForMode(t *testing.T) {
	names := func(ts []toolDef) string {
		var n []string
		for _, t := range ts {
			n = append(n, t.Function.Name)
		}
		return strings.Join(n, ",")
	}
	if got := toolsFor(ModelEntry{Tools: false}, ModeAuto); got != nil {
		t.Errorf("no-tools model got tools: %s", names(got))
	}
	read := names(toolsFor(ModelEntry{Tools: true}, ModeRead))
	if strings.Contains(read, writeFileToolName) || strings.Contains(read, shellToolName) || strings.Contains(read, viewImageToolName) {
		t.Errorf("read mode tools: %s", read)
	}
	ask := names(toolsFor(ModelEntry{Tools: true, Vision: true}, ModeAsk))
	for _, n := range []string{listDirToolName, readFileToolName, grepToolName, writeFileToolName, editFileToolName, shellToolName, questionToolName, viewImageToolName} {
		if !strings.Contains(ask, n) {
			t.Errorf("ask mode misses %s: %s", n, ask)
		}
	}
	for _, td := range toolsFor(ModelEntry{Tools: true, Vision: true}, ModeAsk) {
		if !json.Valid(td.Function.Parameters) {
			t.Errorf("schema of %s is not valid JSON", td.Function.Name)
		}
	}
}
