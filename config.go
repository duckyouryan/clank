package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ModelEntry — одна модель профиля со своими возможностями. Возможности
// живут на модели, а не на профиле: в цепочке фоллбэка соседние модели
// часто умеют разное, и общий на профиль use_tools ронял запасную модель
// без поддержки tools на 400.
type ModelEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"` // человеческое название, только для показа
	Tools  bool   `json:"tools"`
	Vision bool   `json:"vision"`
	// Reasoning — что отправлять в reasoning_effort: уровень (low, high…),
	// "off" (отправляется как "none") или пусто = взять reasoning_default
	// профиля.
	Reasoning string `json:"reasoning,omitempty"`
	// Context — размер контекста в токенах, 0 — не указан. Нужен, чтобы
	// при переполнении идти только к моделям с бóльшим контекстом.
	Context int `json:"context,omitempty"`
	// Disabled — модель остаётся в списке и на своём месте, но фоллбэк её
	// пропускает.
	Disabled bool `json:"disabled,omitempty"`
}

// label — что показывать человеку.
func (m ModelEntry) label() string {
	if m.Name != "" {
		return m.Name + " [" + m.ID + "]"
	}
	return m.ID
}

// Profile — один провайдер: куда ходить, с каким ключом и какие модели
// там брать. Порядок Models — порядок фоллбэка, первая включённая —
// активная.
type Profile struct {
	BaseURL string       `json:"base_url"`
	APIKey  string       `json:"api_key,omitempty"`
	Proxy   string       `json:"proxy,omitempty"` // пусто = HTTP_PROXY/HTTPS_PROXY/NO_PROXY
	Models  []ModelEntry `json:"models"`

	// ReasoningDefault — уровень reasoning для моделей, у которых свой не
	// задан. Пусто = параметр не отправляется, решает провайдер.
	ReasoningDefault string `json:"reasoning_default,omitempty"`

	ExecTimeoutSec int    `json:"exec_timeout_sec,omitempty"` // 0 = дефолт
	Created        string `json:"created,omitempty"`
}

// ToolMode — что модели можно делать без спроса. Режим глобальный: это
// вопрос безопасности, а не провайдера, и `config use` не должен внезапно
// включать auto.
type ToolMode string

const (
	ModeAsk  ToolMode = "ask"  // чтение само, запись и shell — с подтверждением
	ModeRead ToolMode = "read" // только чтение, остальное модели не отдаётся
	ModeAuto ToolMode = "auto" // всё без подтверждения
)

func parseMode(s string) (ToolMode, bool) {
	switch ToolMode(strings.ToLower(strings.TrimSpace(s))) {
	case ModeAsk:
		return ModeAsk, true
	case ModeRead:
		return ModeRead, true
	case ModeAuto:
		return ModeAuto, true
	}
	return "", false
}

// ConfigFile — весь файл конфига.
type ConfigFile struct {
	Active   string             `json:"active"`
	Profiles map[string]Profile `json:"profiles"`
	Mode     ToolMode           `json:"mode,omitempty"` // пусто = ask
	// Language — язык интерфейса: "ru", "en", пусто = по LANG/LC_*.
	Language string `json:"language,omitempty"`
	// Completions — куда мы сами ставили скрипты автодополнения;
	// uninstall/nuke убирают перечисленное.
	Completions []string `json:"completions,omitempty"`
}

func (cf ConfigFile) mode() ToolMode {
	if m, ok := parseMode(string(cf.Mode)); ok {
		return m
	}
	return ModeAsk
}

// Путь фиксирован явно (не os.UserConfigDir()) — на macOS это дало бы
// ~/Library/Application Support, а на Linux ~/.config. Раз тулза живёт
// на обеих платформах, путь один и тот же везде.
func clankDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "clank"), nil
}

func configPath() (string, error) {
	dir, err := clankDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// normalizeBaseURL снимает хвостовой /v1: канонический base_url у OpenAI и
// большинства совместимых провайдеров уже с ним (https://api.openai.com/v1),
// а clank сам дописывает /v1/chat/completions. Без нормализации выходило
// /v1/v1/... и глухой 404.
func normalizeBaseURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	for strings.HasSuffix(u, "/v1") {
		u = strings.TrimSuffix(u, "/v1")
		u = strings.TrimRight(u, "/")
	}
	return u
}

// --- загрузка и миграция ---

// legacyConfigRaw — исходные байты конфига старого формата. Загрузка
// ничего не пишет (её зовёт initLang на каждом старте), поэтому бэкап
// старого файла кладётся при первом сохранении.
var legacyConfigRaw []byte

// UnmarshalJSON принимает и новый формат, и старый: модели строками,
// use_tools/vision/reasoning на профиле. Старое переводится на модели.
func (p *Profile) UnmarshalJSON(data []byte) error {
	var raw struct {
		BaseURL          string          `json:"base_url"`
		APIKey           string          `json:"api_key"`
		Proxy            string          `json:"proxy"`
		Models           json.RawMessage `json:"models"`
		ReasoningDefault string          `json:"reasoning_default"`
		ExecTimeoutSec   int             `json:"exec_timeout_sec"`
		Created          string          `json:"created"`

		// старый формат
		Model           string `json:"model"`
		UseTools        *bool  `json:"use_tools"`
		Vision          *bool  `json:"vision"`
		Reasoning       *bool  `json:"reasoning"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Profile{
		BaseURL:          normalizeBaseURL(raw.BaseURL),
		APIKey:           raw.APIKey,
		Proxy:            raw.Proxy,
		ReasoningDefault: raw.ReasoningDefault,
		ExecTimeoutSec:   raw.ExecTimeoutSec,
		Created:          raw.Created,
	}

	var entries []ModelEntry
	var ids []string
	legacy := false
	if len(raw.Models) > 0 && string(raw.Models) != "null" {
		if err := json.Unmarshal(raw.Models, &entries); err != nil {
			// Unmarshal успевает дописать нулевые элементы до ошибки.
			entries = nil
			if err2 := json.Unmarshal(raw.Models, &ids); err2 != nil {
				return err
			}
			legacy = true
		}
	}
	if len(entries) == 0 && len(ids) == 0 && raw.Model != "" {
		ids = []string{raw.Model}
		legacy = true
	}
	if raw.UseTools != nil || raw.Vision != nil || raw.Reasoning != nil || raw.ReasoningEffort != "" {
		legacy = true
	}

	if !legacy {
		p.Models = entries
		return nil
	}

	// Старые значения были общими на профиль — раздаём их каждой модели.
	// vision=nil раньше значило «не проверяли, считаем что умеет».
	tools := raw.UseTools != nil && *raw.UseTools
	vision := raw.Vision == nil || *raw.Vision
	for _, id := range ids {
		entries = append(entries, ModelEntry{ID: id, Tools: tools, Vision: vision})
	}
	for i := range entries {
		if raw.UseTools != nil {
			entries[i].Tools = tools
		}
	}
	p.Models = entries
	if raw.Reasoning != nil && p.ReasoningDefault == "" {
		switch {
		case !*raw.Reasoning:
			p.ReasoningDefault = "off"
		case raw.ReasoningEffort != "":
			p.ReasoningDefault = raw.ReasoningEffort
		default:
			p.ReasoningDefault = "max" // так старая версия включала reasoning без калибровки
		}
	}
	return nil
}

func (cf *ConfigFile) UnmarshalJSON(data []byte) error {
	var raw struct {
		Active      string                     `json:"active"`
		Profiles    map[string]json.RawMessage `json:"profiles"`
		Mode        string                     `json:"mode"`
		Language    string                     `json:"language"`
		Completions []string                   `json:"completions"`

		// старый формат
		Yolo    *bool           `json:"yolo"`
		Allowed json.RawMessage `json:"allowed_commands"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*cf = ConfigFile{
		Active:      raw.Active,
		Profiles:    map[string]Profile{},
		Mode:        ToolMode(raw.Mode),
		Language:    raw.Language,
		Completions: raw.Completions,
	}
	for name, pr := range raw.Profiles {
		var p Profile
		if err := json.Unmarshal(pr, &p); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
		cf.Profiles[name] = p
	}
	// YOLO и список разрешённых команд заменены режимом.
	if raw.Yolo != nil && *raw.Yolo && cf.Mode == "" {
		cf.Mode = ModeAuto
	}
	return nil
}

// isLegacyConfig — старый ли формат лежит в файле. Определяем по
// признакам старых полей, чтобы бэкап делался только при реальной
// миграции.
func isLegacyConfig(data []byte) bool {
	var probe struct {
		Yolo     *bool                      `json:"yolo"`
		Allowed  json.RawMessage            `json:"allowed_commands"`
		Profiles map[string]json.RawMessage `json:"profiles"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	if probe.Yolo != nil || len(probe.Allowed) > 0 {
		return true
	}
	for _, pr := range probe.Profiles {
		var fields map[string]json.RawMessage
		if json.Unmarshal(pr, &fields) != nil {
			continue
		}
		for _, k := range []string{"model", "use_tools", "vision", "reasoning", "reasoning_effort", "reasoning_budget"} {
			if _, ok := fields[k]; ok {
				return true
			}
		}
		if m, ok := fields["models"]; ok {
			var ids []string
			if json.Unmarshal(m, &ids) == nil && len(ids) > 0 {
				return true
			}
		}
	}
	return false
}

func loadConfigFile() (ConfigFile, error) {
	cf := ConfigFile{Profiles: map[string]Profile{}}
	path, err := configPath()
	if err != nil {
		return cf, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cf, nil
	}
	if err != nil {
		return cf, err
	}
	if err := json.Unmarshal(data, &cf); err != nil {
		return cf, fmt.Errorf(M.ConfigCorrupt, path, err)
	}
	if cf.Profiles == nil {
		cf.Profiles = map[string]Profile{}
	}
	if isLegacyConfig(data) {
		legacyConfigRaw = data
	}
	return cf, nil
}

// saveConfigFile пишет атомарно: временный файл в том же каталоге →
// fsync → rename. Прямой WriteFile, убитый посреди записи, оставлял
// обрубок вместо конфига с ключами.
func saveConfigFile(cf ConfigFile) error {
	dir, err := clankDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	if legacyConfigRaw != nil {
		bak := path + ".bak"
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			if err := os.WriteFile(bak, legacyConfigRaw, 0600); err != nil {
				return err
			}
			info(M.ConfigMigrated, bak)
		}
		legacyConfigRaw = nil
	}
	return writeFileAtomic(path, append(data, '\n'), 0600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// updateConfig перечитывает конфиг с диска, применяет изменение и
// сохраняет. Так параллельный clank в соседней вкладке не откатывает
// чужие правки своим устаревшим снимком: окно гонки сужается до
// микросекунд между чтением и rename. Файл маленький — перечитывание
// ничего не стоит.
func updateConfig(fn func(cf *ConfigFile) error) (ConfigFile, error) {
	cf, err := loadConfigFile()
	if err != nil {
		return cf, err
	}
	if err := fn(&cf); err != nil {
		return cf, err
	}
	if err := saveConfigFile(cf); err != nil {
		return cf, fmt.Errorf(M.SaveFail, err)
	}
	return cf, nil
}

// --- профили ---

// activeProfile возвращает активный профиль. Если активный не задан или
// указывает в пустоту, но профили есть — переключается сам (на самый
// свежий, при равенстве — первый по алфавиту) и сохраняет выбор.
func activeProfile(cf *ConfigFile) (string, Profile, error) {
	if cf.Active != "" {
		if p, ok := cf.Profiles[cf.Active]; ok {
			return cf.Active, p, nil
		}
		warn(M.ActiveNotFound, cf.Active)
	}
	if len(cf.Profiles) == 0 {
		return "", Profile{}, errors.New(M.NoProfiles)
	}

	name := newestProfileName(*cf)
	cf.Active = name
	if _, err := updateConfig(func(c *ConfigFile) error {
		c.Active = name
		return nil
	}); err != nil {
		warn(M.ActiveSaveFail, err)
	}
	info(M.ActiveSwitch, name)
	return name, cf.Profiles[name], nil
}

// resolveProfile — профиль по имени из -p или активный, если имя пустое.
func resolveProfile(cf *ConfigFile, name string) (string, Profile, error) {
	if name == "" {
		return activeProfile(cf)
	}
	p, ok := cf.Profiles[name]
	if !ok {
		return "", Profile{}, fmt.Errorf(M.NoSuchProfileList, name, strings.Join(profileNames(*cf), ", "))
	}
	return name, p, nil
}

func newestProfileName(cf ConfigFile) string {
	names := profileNames(cf)
	best := names[0]
	var bestTime time.Time
	for _, n := range names {
		t, err := time.Parse(time.RFC3339, cf.Profiles[n].Created)
		if err != nil {
			continue
		}
		if t.After(bestTime) {
			bestTime, best = t, n
		}
	}
	return best
}

func profileNames(cf ConfigFile) []string {
	names := make([]string, 0, len(cf.Profiles))
	for n := range cf.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (p Profile) validate() error {
	var missing []string
	if p.BaseURL == "" {
		missing = append(missing, "api-url")
	}
	if len(p.enabledModels()) == 0 {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return fmt.Errorf(M.ProfileInvalid, strings.Join(missing, ", "))
	}
	return nil
}

func (p Profile) execTimeout() time.Duration {
	if p.ExecTimeoutSec > 0 {
		return time.Duration(p.ExecTimeoutSec) * time.Second
	}
	return defaultExecTimeout
}

// --- модели профиля ---

func (p Profile) enabledModels() []ModelEntry {
	var out []ModelEntry
	for _, m := range p.Models {
		if !m.Disabled {
			out = append(out, m)
		}
	}
	return out
}

// activeIndex — индекс первой включённой модели или -1.
func (p Profile) activeIndex() int {
	for i, m := range p.Models {
		if !m.Disabled {
			return i
		}
	}
	return -1
}

// findModel ищет модель по id или по номеру из `config model list`.
// Точное совпадение id важнее номера — на случай модели с именем "2".
func (p Profile) findModel(ref string) (int, error) {
	for i, m := range p.Models {
		if m.ID == ref {
			return i, nil
		}
	}
	if n, err := strconv.Atoi(ref); err == nil && n >= 1 && n <= len(p.Models) {
		return n - 1, nil
	}
	return -1, fmt.Errorf(M.NoSuchModel, ref)
}

// effectiveReasoning — что реально уйдёт в запрос для модели: флаг
// запуска, затем настройка модели, затем дефолт профиля.
func (p Profile) effectiveReasoning(m ModelEntry, override string) string {
	if override != "" {
		return override
	}
	if m.Reasoning != "" {
		return m.Reasoning
	}
	return p.ReasoningDefault
}

// maskKey показывает первые 3 символа и фиксированные звёздочки: по
// длине маски не угадать длину ключа.
func maskKey(key string) string {
	if key == "" {
		return M.KeyMissing
	}
	r := []rune(key)
	if len(r) <= 3 {
		return "***"
	}
	return string(r[:3]) + "***"
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
