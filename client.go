package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Потолок ожидания перед повтором на той же модели: Retry-After длиннее
	// этого — сразу к следующей модели, а не висеть минуту.
	maxRetryWait = 10 * time.Second

	// Ответ принимается потоком (SSE), поэтому таймауты считают тишину,
	// а не длину всего ответа: длинная генерация жива, пока идут куски.
	dialTimeout = 10 * time.Second
	// До первого события: сюда входят подключение, очередь у провайдера
	// и время до первого токена. Reasoning-модели обычно стримят мысли
	// (reasoning_content), роутеры шлют keep-alive комментарии — это тоже
	// считается признаком жизни.
	firstEventTimeout = 90 * time.Second
	// Между событиями внутри потока.
	idleEventTimeout = 30 * time.Second
)

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type imageURL struct {
	URL string `json:"url"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

// Content с omitempty: у assistant-сообщения с tool_calls текста нет, и
// часть строгих OpenAI-совместимых серверов отвергает пустую строку там,
// где ждёт отсутствующее поле или null. Content может быть string или []contentPart.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

func (m chatMessage) Text() string {
	if s, ok := m.Content.(string); ok {
		return s
	}
	if parts, ok := m.Content.([]contentPart); ok {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	if parts, ok := m.Content.([]any); ok {
		var sb strings.Builder
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if pm["type"] == "text" {
					if t, ok := pm["text"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
		}
		return sb.String()
	}
	if m.Content != nil {
		return fmt.Sprint(m.Content)
	}
	return ""
}

func encodeImageFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return encodeImageData(path, data)
}

func encodeImageData(path string, data []byte) (string, error) {
	if len(data) > 20<<20 {
		return "", errors.New(M.ImageBig)
	}
	mime := http.DetectContentType(data)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	case ".gif":
		mime = "image/gif"
	}
	if !strings.HasPrefix(mime, "image/") {
		return "", fmt.Errorf(M.ImageType, path, mime)
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []chatMessage `json:"messages"`
	Stream          bool          `json:"stream"`
	Tools           []toolDef     `json:"tools,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

// chatResponse — ответ целиком; бывает у серверов, которые игнорируют
// stream:true.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *errorBody `json:"error,omitempty"`
}

// streamChunk — одно SSE-событие chat.completion.chunk.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *errorBody `json:"error,omitempty"`
}

// --- классификация ошибок ---

type errKind int

const (
	errOther     errKind = iota
	errConnect           // не достучались до хоста: он общий для всех моделей профиля
	errTransport         // оборвалось посреди ответа
	errTimeout           // провайдер замолчал дольше таймаута тишины
	errAuth              // 401: ключ не принят — воспроизведётся на любой модели
	errForbidden         // 403: часто доступ к конкретной модели, у роутеров — правила ключа
	errQuota             // 402 или 429 с quota/billing: ждать бесполезно, но :free модели могут работать
	errRateLimit
	errServer
	errModelMissing
	errContextOverflow
	errNoImages  // модель не принимает картинки — чиним запрос
	errNoTools   // модель не умеет tools — помечаем в конфиге
	errBadEffort // модель не принимает такой reasoning_effort — чиним запрос
	errTooLarge  // 413: лимит обычно у прокси, одинаковый для всех моделей
	errBadRequest
	errNotFound
	errConfig // кривой конфиг: повторять и переключать модель бессмысленно
)

type apiError struct {
	kind       errKind
	status     int
	msg        string
	url        string
	model      string
	retryAfter time.Duration // из заголовка Retry-After, 0 — не было
}

func (e *apiError) Error() string {
	var b strings.Builder
	switch {
	case e.status > 0:
		fmt.Fprintf(&b, "HTTP %d", e.status)
	case e.kind == errConfig:
		b.WriteString(M.ErrSetup)
	case e.kind == errTimeout:
		b.WriteString(M.ErrTimeout)
	default:
		b.WriteString(M.ErrNoConn)
	}
	if e.model != "" {
		fmt.Fprintf(&b, M.ModelParen, e.model)
	}
	if e.msg != "" {
		fmt.Fprintf(&b, ": %s", truncateRunes(e.msg, 400))
	}
	if hint := e.hint(); hint != "" {
		fmt.Fprintf(&b, "\n  %s", hint)
	}
	return b.String()
}

// short — однострочная причина для сообщений о переключении модели.
func (e *apiError) short() string {
	switch {
	case e.status > 0:
		return fmt.Sprintf("HTTP %d", e.status)
	case e.kind == errConfig:
		return M.ErrShort
	case e.kind == errTimeout:
		return M.ErrTimeout
	}
	return M.NoConn
}

// reason — причина для строки «X не ответил (…), пробую Y».
func (e *apiError) reason() string {
	if e.kind == errContextOverflow {
		return M.ErrShortCtx
	}
	return e.short()
}

func (e *apiError) hint() string {
	switch e.kind {
	case errAuth:
		return M.HintAuth
	case errForbidden:
		return M.HintForbidden
	case errNotFound:
		return M.HintURL
	case errModelMissing:
		return M.HintModel
	case errContextOverflow:
		return M.HintCtx
	case errRateLimit:
		return M.HintRate
	case errConnect, errTransport, errTimeout:
		return M.HintNet
	case errQuota:
		return M.HintQuota
	case errTooLarge:
		return M.HintTooLarge
	}
	return ""
}

// attempts — сколько всего раз пробовать ту же модель. Таймаут тишины не
// повторяем: модель, промолчавшая 90 секунд, скорее всего промолчит и
// второй раз.
func (e *apiError) attempts() int {
	switch e.kind {
	case errConnect, errRateLimit:
		return 2
	case errServer:
		if e.status == 500 || e.status == 0 {
			return 2
		}
		return 3 // 502/503/504/529 — перегруз или шлюз, обычно проходит
	case errTransport:
		return 3
	}
	return 1
}

// tryNextModel — есть смысл перейти к следующей модели цепочки. Хост,
// ключ, кривой запрос и лимит прокси общие для всех моделей профиля —
// на них цепочку не перебираем.
func (e *apiError) tryNextModel() bool {
	switch e.kind {
	case errTransport, errTimeout, errForbidden, errQuota, errRateLimit, errServer, errModelMissing:
		return true
	}
	return false
}

// classify — вид ошибки по статусу и тексту тела: провайдеры отдают 400 на
// очень разные вещи. Подстроки неточные и пополняются по мере встречи.
func classify(status int, body string) errKind {
	low := strings.ToLower(body)
	quota := containsAny(low, "quota", "billing", "credit", "insufficient_funds", "payment")
	switch {
	case status == 401:
		return errAuth
	case status == 402:
		return errQuota
	case status == 403:
		return errForbidden
	case status == 408:
		return errServer
	case status == 413:
		return errTooLarge
	case status == 429 && quota:
		return errQuota
	case status == 429:
		return errRateLimit
	case status >= 500:
		return errServer
	}
	notSupported := containsAny(low, "not support", "unsupported", "not accept", "not enabled", "not available", "not allowed")
	if containsAny(low, "context length", "context_length", "maximum context", "too many tokens",
		"reduce the length", "context window", "prompt is too long") {
		return errContextOverflow
	}
	if containsAny(low, "model not found", "model_not_found", "unknown model", "no such model",
		"does not exist", "invalid model",
		// mlx_lm и другие локальные серверы при неизвестном имени модели
		// пытаются скачать её с HuggingFace и отдают его 404
		"repository not found") {
		return errModelMissing
	}
	if status >= 400 || status == 0 {
		switch {
		case strings.Contains(low, "image") && notSupported:
			return errNoImages
		case containsAny(low, "tool", "function call") && notSupported:
			return errNoTools
		case containsAny(low, "reasoning", "effort") &&
			(notSupported || containsAny(low, "invalid", "must be one of", "unknown", "expected")):
			return errBadEffort
		}
	}
	if status == 404 {
		return errNotFound
	}
	if status >= 400 {
		return errBadRequest
	}
	return errOther
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// --- транспорт ---

// Клиенты кэшируются по прокси: tool-цикл делает десятки запросов подряд,
// и без переиспользования соединений каждый шаг платил за TCP+TLS заново.
var (
	clientsMu sync.Mutex
	clients   = map[string]*http.Client{}
)

func httpClient(p Profile) (*http.Client, error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[p.Proxy]; ok {
		return c, nil
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
		ExpectContinueTimeout: time.Second,
	}
	if p.Proxy != "" {
		u, err := url.Parse(p.Proxy)
		if err != nil {
			// Раньше кривой прокси молча приводил к прямому соединению:
			// пользователь думал, что трафик идёт через прокси, а он не шёл.
			return nil, fmt.Errorf(M.ProxyParse, p.Proxy, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf(M.ProxyBlind, p.Proxy)
		}
		transport.Proxy = http.ProxyURL(u)
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	// Общего Timeout нет намеренно: длину ответа ограничивают таймауты
	// тишины в doStream, а не потолок на весь запрос.
	c := &http.Client{Transport: transport}
	clients[p.Proxy] = c
	return c, nil
}

func newRequest(p Profile, ctx context.Context, method, path string, body []byte, model string) (*http.Request, string, error) {
	endpoint := normalizeBaseURL(p.BaseURL) + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, endpoint, &apiError{kind: errConfig, msg: err.Error(), url: endpoint, model: model}
	}
	req.Header.Set("Content-Type", "application/json")
	// Локальным серверам ключ часто не нужен — пустой не отправляем.
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	req.Header.Set("User-Agent", "clank/"+version)
	return req, endpoint, nil
}

func statusError(resp *http.Response, endpoint, model string) error {
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	msg := extractErrorMessage(respBody)
	return &apiError{
		kind:       classify(resp.StatusCode, msg+" "+string(respBody)),
		status:     resp.StatusCode,
		msg:        msg,
		url:        endpoint,
		model:      model,
		retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

// parseRetryAfter понимает обе формы заголовка: секунды и HTTP-дату.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// netErrKind — до хоста не достучались (DNS, отказ в соединении, TLS)
// или соединение оборвалось уже в процессе.
func netErrKind(err error) errKind {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	if errors.As(err, &dnsErr) || (errors.As(err, &opErr) && opErr.Op == "dial") {
		return errConnect
	}
	if containsAny(strings.ToLower(err.Error()), "tls:", "x509:", "certificate", "proxyconnect") {
		return errConnect
	}
	return errTransport
}

// apiRequest — простой запрос с ответом целиком (список моделей).
func apiRequest(p Profile, method, path string, body []byte, model string) ([]byte, error) {
	client, err := httpClient(p)
	if err != nil {
		// Профиль настроен неправильно — ни повтор, ни другая модель
		// этого не исправят.
		return nil, &apiError{kind: errConfig, msg: err.Error(), model: model}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*firstEventTimeout)
	defer cancel()
	req, endpoint, err := newRequest(p, ctx, method, path, body, model)
	if err != nil {
		return nil, err
	}
	detail("%s %s", method, endpoint)
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, &apiError{kind: netErrKind(err), msg: err.Error(), url: endpoint, model: model}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, statusError(resp, endpoint, model)
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &apiError{kind: errTransport, msg: err.Error(), url: endpoint, model: model}
	}
	detail(M.RespDetail, resp.StatusCode, time.Since(started).Seconds(), len(respBody))
	return respBody, nil
}

// extractErrorMessage вытаскивает человеческий текст из тела ошибки.
func extractErrorMessage(body []byte) string {
	var wrapped struct {
		Error   *errorBody `json:"error"`
		Message string     `json:"message"`
		Detail  string     `json:"detail"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		switch {
		case wrapped.Error != nil && wrapped.Error.Message != "":
			return wrapped.Error.Message
		case wrapped.Message != "":
			return wrapped.Message
		case wrapped.Detail != "":
			return wrapped.Detail
		}
	}
	return strings.TrimSpace(string(body))
}

// --- запросы ---

type chatResult struct {
	msg          chatMessage
	model        string
	finishReason string
}

// reqOpts — то, что зависит от конкретной модели цепочки.
type reqOpts struct {
	tools     []toolDef
	reasoning string // "" — не отправлять, "off" — "none", иначе уровень
}

// chatComplete — один запрос к одной модели. Ответ принимается потоком,
// но собирается целиком: выводится всё равно разом.
func chatComplete(p Profile, model string, messages []chatMessage, opts reqOpts) (chatResult, error) {
	reqStruct := chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Tools:    opts.tools,
	}
	switch opts.reasoning {
	case "":
	case "off":
		reqStruct.ReasoningEffort = "none"
	default:
		reqStruct.ReasoningEffort = opts.reasoning
	}
	reqBody, err := json.Marshal(reqStruct)
	if err != nil {
		return chatResult{}, err
	}
	detail(M.ReqDetail, model, len(messages), len(reqBody))

	res, err := doStream(p, model, reqBody)
	if err != nil {
		return chatResult{}, err
	}
	// Размышления (теги <think> в тексте) никогда не подставляются в ответ
	// пользователю; reasoning_content в потоке просто не собираем.
	if s, ok := res.msg.Content.(string); ok {
		res.msg.Content = stripThinkBlocks(s)
		if res.msg.Content == "" {
			res.msg.Content = nil
		}
	}
	return res, nil
}

// doStream отправляет запрос и читает SSE. Таймер тишины перезаводится
// на каждой строке потока (включая keep-alive комментарии): длинный ответ
// живёт сколько угодно, а провайдер, который замолчал, отваливается
// через idleEventTimeout.
func doStream(p Profile, model string, body []byte) (chatResult, error) {
	client, err := httpClient(p)
	if err != nil {
		return chatResult{}, &apiError{kind: errConfig, msg: err.Error(), model: model}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(firstEventTimeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()

	req, endpoint, err := newRequest(p, ctx, http.MethodPost, "/v1/chat/completions", body, model)
	if err != nil {
		return chatResult{}, err
	}
	req.Header.Set("Accept", "text/event-stream")
	detail("POST %s", endpoint)
	started := time.Now()

	wrap := func(err error) error {
		if timedOut.Load() {
			return &apiError{kind: errTimeout, msg: fmt.Sprintf(M.SilentFor, time.Since(started).Round(time.Second)), url: endpoint, model: model}
		}
		return &apiError{kind: errTransport, msg: err.Error(), url: endpoint, model: model}
	}

	resp, err := client.Do(req)
	if err != nil {
		if !timedOut.Load() && netErrKind(err) == errConnect {
			return chatResult{}, &apiError{kind: errConnect, msg: err.Error(), url: endpoint, model: model}
		}
		return chatResult{}, wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return chatResult{}, statusError(resp, endpoint, model)
	}

	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "event-stream") {
		// Сервер проигнорировал stream:true и прислал ответ целиком.
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return chatResult{}, wrap(err)
		}
		detail(M.RespDetail, resp.StatusCode, time.Since(started).Seconds(), len(data))
		return parseWholeResponse(data, model)
	}

	var (
		acc    streamAcc
		events int
		bytesN int
	)
	rd := bufio.NewReader(resp.Body)
	for {
		line, err := rd.ReadString('\n')
		if len(line) > 0 {
			timer.Reset(idleEventTimeout)
			bytesN += len(line)
		}
		if err != nil && !(errors.Is(err, io.EOF) && line != "") {
			if errors.Is(err, io.EOF) {
				break
			}
			return chatResult{}, wrap(err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			if err != nil {
				break
			}
			continue // пустые строки, keep-alive комментарии, event:/id:
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		events++
		var ch streamChunk
		if jerr := json.Unmarshal([]byte(payload), &ch); jerr != nil {
			return chatResult{}, &apiError{kind: errOther, model: model,
				msg: fmt.Sprintf(M.RespUnpars, jerr, truncateRunes(payload, 200))}
		}
		if ch.Error != nil && ch.Error.Message != "" {
			return chatResult{}, &apiError{kind: classify(0, ch.Error.Message), model: model, msg: ch.Error.Message}
		}
		acc.add(ch)
		if err != nil {
			break
		}
	}
	detail(M.StreamDetail, events, bytesN, time.Since(started).Seconds())
	if !acc.gotChoice {
		return chatResult{}, &apiError{kind: errServer, model: model, msg: M.EmptyChoi}
	}
	return acc.result(model), nil
}

// streamAcc склеивает дельты потока в одно сообщение. Аргументы tool_calls
// приходят JSON-обрывками и собираются по index; у провайдеров без index
// новый вызов распознаётся по новому id.
type streamAcc struct {
	content   strings.Builder
	calls     []toolCall
	byIndex   map[int]int
	finish    string
	gotChoice bool
}

func (a *streamAcc) add(ch streamChunk) {
	if a.byIndex == nil {
		a.byIndex = map[int]int{}
	}
	for _, c := range ch.Choices {
		a.gotChoice = true
		a.content.WriteString(c.Delta.Content)
		if c.FinishReason != nil && *c.FinishReason != "" {
			a.finish = *c.FinishReason
		}
		for _, d := range c.Delta.ToolCalls {
			pos := -1
			if d.Index != nil {
				if i, ok := a.byIndex[*d.Index]; ok {
					pos = i
				}
			} else if len(a.calls) > 0 && (d.ID == "" || d.ID == a.calls[len(a.calls)-1].ID) {
				pos = len(a.calls) - 1
			}
			if pos >= 0 && d.ID != "" && a.calls[pos].ID != "" && a.calls[pos].ID != d.ID {
				pos = -1 // тот же index, но другой id — значит новый вызов
			}
			if pos < 0 {
				a.calls = append(a.calls, toolCall{})
				pos = len(a.calls) - 1
				if d.Index != nil {
					a.byIndex[*d.Index] = pos
				}
			}
			tc := &a.calls[pos]
			if d.ID != "" {
				tc.ID = d.ID
			}
			if d.Type != "" {
				tc.Type = d.Type
			}
			tc.Function.Name += d.Function.Name
			tc.Function.Arguments += d.Function.Arguments
		}
	}
}

func (a *streamAcc) result(model string) chatResult {
	msg := chatMessage{Role: "assistant"}
	if s := a.content.String(); s != "" {
		msg.Content = s
	}
	for i := range a.calls {
		if a.calls[i].Type == "" {
			a.calls[i].Type = "function"
		}
		if a.calls[i].ID == "" {
			a.calls[i].ID = fmt.Sprintf("call_%d", i)
		}
	}
	msg.ToolCalls = a.calls
	return chatResult{msg: msg, model: model, finishReason: a.finish}
}

func parseWholeResponse(data []byte, model string) (chatResult, error) {
	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return chatResult{}, &apiError{kind: errOther, model: model,
			msg: fmt.Sprintf(M.RespUnpars, err, truncateRunes(string(data), 200))}
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return chatResult{}, &apiError{kind: classify(0, parsed.Error.Message), model: model, msg: parsed.Error.Message}
	}
	if len(parsed.Choices) == 0 {
		return chatResult{}, &apiError{kind: errServer, model: model, msg: M.EmptyChoi}
	}
	c := parsed.Choices[0]
	msg := chatMessage{Role: "assistant", ToolCalls: c.Message.ToolCalls}
	if c.Message.Content != "" {
		msg.Content = c.Message.Content
	}
	return chatResult{msg: msg, model: model, finishReason: c.FinishReason}, nil
}

// stripThinkBlocks убирает <think>…</think>, <thought>…</thought>,
// <reasoning>…</reasoning>: размышления не должны засорять вывод пользователю.
func stripThinkBlocks(s string) string {
	for _, tag := range []string{"think", "thought", "reasoning"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		for {
			start := strings.Index(s, open)
			if start < 0 {
				break
			}
			end := strings.Index(s[start:], close)
			if end < 0 {
				// открывающий тег без закрывающего (обрыв по лимиту токенов) —
				// вырезаем всё от открывающего тега до конца, чтобы мысли не утекли
				s = s[:start]
				break
			}
			s = s[:start] + s[start+end+len(close):]
		}
	}
	return strings.TrimSpace(s)
}

// chatWithFallback идёт по цепочке моделей. Переключается только на
// технических отказах — «плохой по смыслу» ответ фоллбэк не запускает.
// Часть отказов чинится на месте: картинки вырезаются, непринятый
// reasoning_effort убирается. При переполнении контекста дальше идут
// только модели с бóльшим контекстом. Возвращает индекс ответившей
// модели в chain, чтобы вызывающий продолжил с неё.
func chatWithFallback(p Profile, chain []ModelEntry, build func(ModelEntry) ([]chatMessage, reqOpts)) (chatResult, int, error) {
	if len(chain) == 0 {
		return chatResult{}, -1, errors.New(M.NoModels)
	}

	var (
		attempts []string
		lastErr  error
		ctxFloor = -1 // после переполнения: годятся только модели с Context больше
	)
	eligible := func(m ModelEntry) bool { return ctxFloor < 0 || m.Context > ctxFloor }
	next := func(i int) int {
		for j := i + 1; j < len(chain); j++ {
			if eligible(chain[j]) {
				return j
			}
		}
		return -1
	}

	for i, m := range chain {
		if !eligible(m) {
			continue
		}
		msgs, opts := build(m)
		var (
			res chatResult
			err error
		)
		for {
			res, err = chatCompleteRetry(p, m.ID, msgs, opts)
			ae, ok := err.(*apiError)
			if !ok {
				break
			}
			if ae.kind == errNoImages && hasImages(msgs) {
				msgs = stripImages(msgs)
				noteImagesDropped()
				continue
			}
			if ae.kind == errBadEffort && opts.reasoning != "" {
				warn(M.EffortDropped, m.ID, opts.reasoning)
				opts.reasoning = ""
				continue
			}
			break
		}
		if err == nil {
			if i > 0 {
				info(M.Answering, m.label())
			}
			return res, i, nil
		}
		lastErr = err

		ae, ok := err.(*apiError)
		if !ok {
			return chatResult{}, -1, err
		}
		attempts = append(attempts, fmt.Sprintf("%s — %s", m.ID, ae.reason()))

		if ae.kind == errContextOverflow {
			if m.Context == 0 {
				return chatResult{}, -1, fmt.Errorf(M.CtxUnknown, err, m.ID, m.ID)
			}
			ctxFloor = m.Context
			if next(i) < 0 {
				return chatResult{}, -1, fmt.Errorf(M.CtxNoBigger, err, m.ID)
			}
		} else if !ae.tryNextModel() {
			return chatResult{}, -1, err
		}
		if j := next(i); j >= 0 {
			info(M.ModelFail, m.label(), ae.reason(), chain[j].label())
		}
	}

	// При единственной модели список попыток ничего не добавляет, а
	// подсказку из исходной ошибки терять жалко.
	if len(attempts) == 1 {
		return chatResult{}, -1, lastErr
	}
	return chatResult{}, -1, fmt.Errorf(M.NoAnswer, strings.Join(attempts, "\n  "), lastErr)
}

// rotateChain ставит ответившую модель в начало, остальные идут за ней по
// кругу: дальше в этом запуске работаем с рабочей моделью, а если упадёт
// и она — пробуем остальные, включая те, что были до неё.
func rotateChain(chain []ModelEntry, i int) []ModelEntry {
	if i <= 0 || i >= len(chain) {
		return chain
	}
	out := make([]ModelEntry, 0, len(chain))
	out = append(out, chain[i:]...)
	return append(out, chain[:i]...)
}

// chatCompleteRetry повторяет запрос при временных отказах. Retry-After
// уважаем, но дольше maxRetryWait не ждём — тогда лучше другая модель.
func chatCompleteRetry(p Profile, model string, messages []chatMessage, opts reqOpts) (chatResult, error) {
	backoff := time.Second
	for attempt := 1; ; attempt++ {
		res, err := chatComplete(p, model, messages, opts)
		if err == nil {
			return res, nil
		}
		ae, ok := err.(*apiError)
		if !ok || attempt >= ae.attempts() {
			return chatResult{}, err
		}
		wait := backoff
		if ae.retryAfter > 0 {
			if ae.retryAfter > maxRetryWait {
				return chatResult{}, err
			}
			wait = ae.retryAfter
		}
		info(M.RetryIn, model, ae.short(), wait.Round(time.Second), attempt, ae.attempts()-1)
		time.Sleep(wait)
		backoff *= 2
	}
}

// --- картинки ---

var imagesNoted bool

// noteImagesDropped — один раз за запуск: дальше по шагам картинки
// вырезаются молча.
func noteImagesDropped() {
	if !imagesNoted {
		imagesNoted = true
		warn(M.ImagesDropped)
	}
}

func isImagePart(t string) bool { return t == "image_url" || t == "image" || t == "input_image" }

func hasImages(msgs []chatMessage) bool {
	for _, m := range msgs {
		switch c := m.Content.(type) {
		case []contentPart:
			for _, p := range c {
				if isImagePart(p.Type) {
					return true
				}
			}
		case []any:
			for _, p := range c {
				if pm, ok := p.(map[string]any); ok {
					if t, _ := pm["type"].(string); isImagePart(t) {
						return true
					}
				}
			}
		}
	}
	return false
}

// stripImages — копия диалога без картинок; сам диалог не трогаем, чтобы
// модели с vision дальше их видели. Сообщение, от которого остались
// одни картинки, становится текстовой пометкой.
func stripImages(msgs []chatMessage) []chatMessage {
	out := make([]chatMessage, len(msgs))
	for i, m := range msgs {
		out[i] = m
		var texts []string
		dropped := false
		switch c := m.Content.(type) {
		case []contentPart:
			for _, p := range c {
				if isImagePart(p.Type) {
					dropped = true
				} else if p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
		case []any:
			for _, p := range c {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := pm["type"].(string); isImagePart(t) {
					dropped = true
				} else if txt, _ := pm["text"].(string); txt != "" {
					texts = append(texts, txt)
				}
			}
		default:
			continue
		}
		if !dropped {
			continue
		}
		text := strings.Join(texts, "\n")
		if text == "" {
			text = "[image omitted: this model does not accept images]"
		}
		out[i].Content = text
	}
	return out
}

// remoteModel — модель из /v1/models. Поля сверх id отдают не все: это
// формат роутеров в стиле OpenRouter. Чего нет — то нулевое.
type remoteModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	Reasoning *struct {
		SupportedEfforts []string `json:"supported_efforts"`
		DefaultEffort    string   `json:"default_effort"`
	} `json:"reasoning"`
	SupportedParameters []string `json:"supported_parameters"`
}

// textOutput — модель выдаёт текст. Без метаданных считаем, что да.
func (m remoteModel) textOutput() bool {
	if len(m.Architecture.OutputModalities) == 0 {
		return true
	}
	return containsString(m.Architecture.OutputModalities, "text")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func listModels(p Profile) ([]remoteModel, error) {
	respBody, err := apiRequest(p, http.MethodGet, "/v1/models", nil, "")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data  []remoteModel `json:"data"`
		Error *errorBody    `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf(M.ModelsUnp, err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf(M.APIError, parsed.Error.Message)
	}
	return parsed.Data, nil
}
