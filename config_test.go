package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "clank")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	legacyConfigRaw = nil
	return path
}

func TestMigrateLegacyConfig(t *testing.T) {
	applyLang(LangEN)
	path := writeConfig(t, `{
  "active": "home",
  "yolo": true,
  "allowed_commands": {"ls": "simple"},
  "profiles": {
    "home": {"base_url": "http://h", "api_key": "k", "models": ["a", "b"], "model": "a",
             "use_tools": true, "reasoning": true, "reasoning_effort": "high"},
    "old":  {"base_url": "http://o", "api_key": "k", "model": "solo", "use_tools": false, "vision": false}
  }
}`)
	cf, err := loadConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if cf.mode() != ModeAuto {
		t.Errorf("yolo:true must become mode auto, got %q", cf.Mode)
	}
	h := cf.Profiles["home"]
	if len(h.Models) != 2 || !h.Models[0].Tools || !h.Models[1].Tools || !h.Models[0].Vision {
		t.Errorf("home models: %+v", h.Models)
	}
	if h.ReasoningDefault != "high" {
		t.Errorf("reasoning default: %q", h.ReasoningDefault)
	}
	o := cf.Profiles["old"]
	if len(o.Models) != 1 || o.Models[0].ID != "solo" || o.Models[0].Tools || o.Models[0].Vision {
		t.Errorf("old models: %+v", o.Models)
	}

	if err := saveConfigFile(cf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("backup not written: %v", err)
	}
	data, _ := os.ReadFile(path)
	for _, k := range []string{"yolo", "allowed_commands", "use_tools", `"model"`} {
		if strings.Contains(string(data), k) {
			t.Errorf("migrated config still has %s:\n%s", k, data)
		}
	}
	// Повторная загрузка нового формата — уже не миграция.
	if _, err := loadConfigFile(); err != nil {
		t.Fatal(err)
	}
	if legacyConfigRaw != nil {
		t.Errorf("new format detected as legacy")
	}
}

func TestUpdateConfigKeepsConcurrentChanges(t *testing.T) {
	applyLang(LangEN)
	writeConfig(t, `{"active":"a","profiles":{"a":{"base_url":"http://a","models":[{"id":"m","tools":true,"vision":false}]}}}`)
	stale, _ := loadConfigFile()
	// «Другая вкладка» меняет конфиг.
	if _, err := updateConfig(func(cf *ConfigFile) error {
		cf.Mode = ModeRead
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Наша правка через updateConfig не откатывает чужую.
	_ = stale
	cf, err := updateConfig(func(cf *ConfigFile) error {
		cf.Language = "ru"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cf.Mode != ModeRead || cf.Language != "ru" {
		t.Errorf("lost update: mode=%q lang=%q", cf.Mode, cf.Language)
	}
}

func TestFindModelAndMask(t *testing.T) {
	p := Profile{Models: []ModelEntry{{ID: "x"}, {ID: "2"}, {ID: "z", Disabled: true}}}
	if i, _ := p.findModel("2"); i != 1 {
		t.Errorf("exact id must win over number, got %d", i)
	}
	if i, _ := p.findModel("1"); i != 0 {
		t.Errorf("number 1 → %d", i)
	}
	if _, err := p.findModel("9"); err == nil {
		t.Errorf("out of range must fail")
	}
	if len(p.enabledModels()) != 2 || p.activeIndex() != 0 {
		t.Errorf("enabled/active wrong")
	}
	if maskKey("abcdefgh") != "abc***" || maskKey("ab") != "***" {
		t.Errorf("mask: %s %s", maskKey("abcdefgh"), maskKey("ab"))
	}
}

func TestEffectiveReasoning(t *testing.T) {
	p := Profile{ReasoningDefault: "low"}
	if r := p.effectiveReasoning(ModelEntry{}, ""); r != "low" {
		t.Errorf("profile default: %q", r)
	}
	if r := p.effectiveReasoning(ModelEntry{Reasoning: "high"}, ""); r != "high" {
		t.Errorf("model: %q", r)
	}
	if r := p.effectiveReasoning(ModelEntry{Reasoning: "high"}, "off"); r != "off" {
		t.Errorf("flag: %q", r)
	}
}

func TestScanFlags(t *testing.T) {
	applyLang(LangEN)
	pos, flags, err := scanFlags([]string{"name", "--api-url=http://x", "--model", "a", "--tools", "true", "-y", "--proxy", "-"},
		[]string{"--api-url", "--model", "--tools", "--proxy"}, []string{"-y"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pos, []string{"name"}) {
		t.Errorf("pos: %v", pos)
	}
	want := []kv{{"--api-url", "http://x"}, {"--model", "a"}, {"--tools", "true"}, {"-y", ""}, {"--proxy", "-"}}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("flags: %v", flags)
	}
	if _, _, err := scanFlags([]string{"--model"}, []string{"--model"}, nil); err == nil {
		t.Errorf("missing value must fail")
	}
	if _, _, err := scanFlags([]string{"--bogus"}, nil, nil); err == nil {
		t.Errorf("unknown flag must fail")
	}
}

func TestStreamAccumulatesToolCalls(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"content":"Hi "}}]}`,
		`{"choices":[{"delta":{"content":"there"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"x\"}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"grep","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	var acc streamAcc
	for _, c := range chunks {
		var ch streamChunk
		if err := json.Unmarshal([]byte(c), &ch); err != nil {
			t.Fatal(err)
		}
		acc.add(ch)
	}
	res := acc.result("m")
	if res.msg.Text() != "Hi there" || res.finishReason != "tool_calls" {
		t.Errorf("content/finish: %q %q", res.msg.Text(), res.finishReason)
	}
	if len(res.msg.ToolCalls) != 2 {
		t.Fatalf("calls: %+v", res.msg.ToolCalls)
	}
	if tc := res.msg.ToolCalls[0]; tc.ID != "c1" || tc.Function.Name != "read_file" || tc.Function.Arguments != `{"path":"x"}` {
		t.Errorf("call 0: %+v", tc)
	}
	if tc := res.msg.ToolCalls[1]; tc.ID != "c2" || tc.Type != "function" {
		t.Errorf("call 1: %+v", tc)
	}
}

func TestRotateChain(t *testing.T) {
	c := []ModelEntry{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := rotateChain(c, 1)
	if got[0].ID != "b" || got[1].ID != "c" || got[2].ID != "a" {
		t.Errorf("rotate: %+v", got)
	}
	if rotateChain(c, 0)[0].ID != "a" {
		t.Errorf("rotate 0 must keep order")
	}
}

func TestClassifyActions(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		kind     errKind
		next     bool
		attempts int
	}{
		{401, "bad key", errAuth, false, 1},
		{402, "payment required", errQuota, true, 1},
		{403, "provider not allowed", errForbidden, true, 1},
		{408, "", errServer, true, 3},
		{413, "too large", errTooLarge, false, 1},
		{429, "slow down", errRateLimit, true, 2},
		{429, "insufficient_quota", errQuota, true, 1},
		{500, "", errServer, true, 2},
		{503, "overloaded", errServer, true, 3},
		{400, "This model's maximum context length is 8192 tokens", errContextOverflow, false, 1},
		{400, "input not supported: model x does not accept image input", errNoImages, false, 1},
		{400, "this model does not support tools", errNoTools, false, 1},
		{400, "reasoning_effort must be one of low, medium, high", errBadEffort, false, 1},
		{404, "model not found", errModelMissing, true, 1},
		{404, "no route", errNotFound, false, 1},
		{400, "messages[3]: bad role", errBadRequest, false, 1},
	}
	for _, c := range cases {
		e := &apiError{kind: classify(c.status, c.body), status: c.status}
		if e.kind != c.kind || e.tryNextModel() != c.next || e.attempts() != c.attempts {
			t.Errorf("%d %q → kind=%d next=%v attempts=%d, want kind=%d next=%v attempts=%d",
				c.status, c.body, e.kind, e.tryNextModel(), e.attempts(), c.kind, c.next, c.attempts)
		}
	}
	for _, k := range []errKind{errConnect, errTimeout} {
		e := &apiError{kind: k}
		if k == errConnect && (e.tryNextModel() || e.attempts() != 2) {
			t.Errorf("connect: next=%v attempts=%d", e.tryNextModel(), e.attempts())
		}
		if k == errTimeout && (!e.tryNextModel() || e.attempts() != 1) {
			t.Errorf("timeout: next=%v attempts=%d", e.tryNextModel(), e.attempts())
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	if parseRetryAfter("3") != 3*time.Second || parseRetryAfter("") != 0 || parseRetryAfter("junk") != 0 {
		t.Errorf("seconds form")
	}
	if d := parseRetryAfter(time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)); d < 3*time.Second || d > 6*time.Second {
		t.Errorf("date form: %v", d)
	}
}

func TestStripImages(t *testing.T) {
	msgs := []chatMessage{
		{Role: "user", Content: []contentPart{{Type: "text", Text: "what"}, {Type: "image_url", ImageURL: &imageURL{URL: "data:"}}}},
		{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "x"}}}},
		{Role: "user", Content: "plain"},
	}
	if !hasImages(msgs) {
		t.Fatal("hasImages")
	}
	out := stripImages(msgs)
	if hasImages(out) || out[0].Content != "what" || !strings.Contains(out[1].Text(), "image omitted") || out[2].Content != "plain" {
		t.Errorf("strip: %+v", out)
	}
	if !hasImages(msgs) {
		t.Errorf("original must stay intact")
	}
}

// fakeProvider — сервер, отвечающий по id модели. Ответ — SSE с одним
// текстовым куском либо ошибка с заданным статусом и телом.
func fakeProvider(t *testing.T, handle func(model string, req map[string]any) (int, string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		model, _ := req["model"].(string)
		status, body := handle(model, req)
		if status != 200 {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"message":%q}}`, body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", body)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func plainBuild(m ModelEntry) ([]chatMessage, reqOpts) {
	return []chatMessage{{Role: "user", Content: "hi"}}, reqOpts{reasoning: m.Reasoning}
}

func TestFallbackContextPicksBigger(t *testing.T) {
	applyLang(LangEN)
	var tried []string
	srv := fakeProvider(t, func(model string, _ map[string]any) (int, string) {
		tried = append(tried, model)
		if model == "big" {
			return 200, "ok from big"
		}
		return 400, "maximum context length exceeded"
	})
	p := Profile{BaseURL: srv.URL}
	chain := []ModelEntry{{ID: "small", Context: 8000}, {ID: "tiny", Context: 4000}, {ID: "big", Context: 128000}}
	res, idx, err := chatWithFallback(p, chain, plainBuild)
	if err != nil || idx != 2 || res.msg.Text() != "ok from big" {
		t.Fatalf("res=%q idx=%d err=%v", res.msg.Text(), idx, err)
	}
	if strings.Join(tried, ",") != "small,big" {
		t.Errorf("tiny must be skipped: %v", tried)
	}

	_, _, err = chatWithFallback(p, []ModelEntry{{ID: "small"}, {ID: "big", Context: 128000}}, plainBuild)
	if err == nil || !strings.Contains(err.Error(), "--context") {
		t.Errorf("unknown context must stop with a hint: %v", err)
	}
}

func TestFallbackFixesImagesAndEffort(t *testing.T) {
	applyLang(LangEN)
	imagesNoted = false
	srv := fakeProvider(t, func(model string, req map[string]any) (int, string) {
		if _, ok := req["reasoning_effort"]; ok {
			return 400, "invalid reasoning_effort: must be one of low, high"
		}
		b, _ := json.Marshal(req["messages"])
		if strings.Contains(string(b), "image_url") {
			return 400, "model does not accept image input"
		}
		return 200, "fixed"
	})
	p := Profile{BaseURL: srv.URL}
	build := func(m ModelEntry) ([]chatMessage, reqOpts) {
		return []chatMessage{{Role: "user", Content: []contentPart{{Type: "text", Text: "x"}, {Type: "image_url", ImageURL: &imageURL{URL: "data:"}}}}},
			reqOpts{reasoning: "ultra"}
	}
	res, _, err := chatWithFallback(p, []ModelEntry{{ID: "m", Vision: true}}, build)
	if err != nil || res.msg.Text() != "fixed" || !imagesNoted {
		t.Fatalf("res=%q err=%v noted=%v", res.msg.Text(), err, imagesNoted)
	}
}

func TestFallbackStopsOnConnectAndAdvancesOn402(t *testing.T) {
	applyLang(LangEN)
	srv := fakeProvider(t, func(model string, _ map[string]any) (int, string) {
		if model == "paid" {
			return 402, "insufficient credits"
		}
		return 200, "free ok"
	})
	res, idx, err := chatWithFallback(Profile{BaseURL: srv.URL}, []ModelEntry{{ID: "paid"}, {ID: "free"}}, plainBuild)
	if err != nil || idx != 1 || res.msg.Text() != "free ok" {
		t.Fatalf("402: res=%q idx=%d err=%v", res.msg.Text(), idx, err)
	}

	var calls int
	dead := Profile{BaseURL: "http://127.0.0.1:1"} // никто не слушает
	_, _, err = chatWithFallback(dead, []ModelEntry{{ID: "a"}, {ID: "b"}}, func(m ModelEntry) ([]chatMessage, reqOpts) {
		calls++
		return plainBuild(m)
	})
	ae, ok := err.(*apiError)
	if !ok || ae.kind != errConnect || calls != 1 {
		t.Errorf("connect error must stop on the first model: err=%v calls=%d", err, calls)
	}
}

// Все поля таблиц заполнены, и форматные глаголы совпадают между языками:
// иначе одна из локалей печатает %!d(MISSING) или пустую строку.
func TestLangTablesComplete(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)
	ru, en := reflect.ValueOf(RU), reflect.ValueOf(EN)
	typ := ru.Type()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		r, e := ru.Field(i).String(), en.Field(i).String()
		if r == "" || e == "" {
			t.Errorf("%s is empty (ru=%v en=%v)", name, r == "", e == "")
			continue
		}
		if a, b := verbs.FindAllString(r, -1), verbs.FindAllString(e, -1); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: verbs differ ru=%v en=%v", name, a, b)
		}
	}
}

func TestNoToolsMarksConfig(t *testing.T) {
	applyLang(LangEN)
	writeConfig(t, `{"active":"p","profiles":{"p":{"base_url":"http://x","models":[{"id":"a","tools":true},{"id":"b","tools":true}]}}}`)
	if haveTTY() {
		t.Skip("есть терминал — handleNoTools спросит выбор")
	}
	_, ok := handleNoTools("p", []ModelEntry{{ID: "a", Tools: true}, {ID: "b", Tools: true}}, &apiError{kind: errNoTools, model: "a"})
	if ok {
		t.Errorf("without a tty it must stop")
	}
	cf, _ := loadConfigFile()
	if m := cf.Profiles["p"].Models; m[0].Tools || !m[1].Tools {
		t.Errorf("a must be marked tools=false: %+v", m)
	}
	if !hasToolHistory([]chatMessage{{Role: "tool"}}) || hasToolHistory([]chatMessage{{Role: "user"}}) {
		t.Errorf("hasToolHistory")
	}
}
