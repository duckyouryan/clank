# AGENTS.md — clank codebase guide

> For AI agents working on this repo. Read this before touching any file.

---

## What is clank

A minimal CLI that wraps any OpenAI-compatible API. Core use case: pipe terminal output into it, ask a question, get an answer — without opening a browser. Secondary use case: let the model look at files, edit them, run shell commands and ask interactive questions via registered tools.

No dependencies outside the Go stdlib. Single binary. Config in `~/.config/clank/`.

---

## Build & install

```
make          # → bin/clank
make install  # → /usr/local/bin/clank (PREFIX overridable)
make check    # fmt + vet + test
```

Version is injected at link time: `git describe --tags`, falling back to `date +%Y.%m.%d`. Releases are cut by `make publish|minor|major` (git tag → GitHub Actions).

Module: `clank` (go 1.26, zero external deps). 1.26 is the floor because of `os.Root` (1.24) with its 1.25 additions (`MkdirAll`, `ReadFile`, `WriteFile`); 1.27 would drop macOS 12. All builds are `CGO_ENABLED=0` (static, no glibc dependency).

`test/` is gitignored scratch space with its own `go.mod`, so `go test ./...` and `go vet ./...` do not descend into it.

---

## Directory layout

All code is in package `main` (single package, flat directory). No subdirectories.

| File            | Responsibility                                              |
|-----------------|-------------------------------------------------------------|
| `main.go`       | Entry point, top-level command dispatch                     |
| `ask.go`        | Core ask loop: flags, chain, sticky fallback, tool loop     |
| `tools.go`      | Tool schemas, path policy (`os.Root`), tool execution       |
| `client.go`     | HTTP client, SSE streaming, error classification, fallback  |
| `config.go`     | Config types, legacy migration, atomic save, `updateConfig` |
| `configcmd.go`  | `clank config *`, `clank config model *`, `clank models`, `clank mode` |
| `completion.go` | `clank completion` scripts (zsh/bash/fish, RU+EN from one template) |
| `lang.go`       | Language tables switch, locale detect, `clank lang`, help topics |
| `lang_ru.go`    | Russian UI strings (`RU` table)                             |
| `lang_en.go`    | English UI strings (`EN` table)                             |
| `exec.go`       | Shell execution, output capping                             |
| `exec_*.go`     | Platform-specific shell execution (Unix/Windows)            |
| `lifecycle.go`  | `purge`, `uninstall`, and `nuke` commands                   |
| `session.go`    | Per-TTY session persistence, GC                             |
| `session_*.go`  | Platform-specific session identity (Unix/Windows)           |
| `systemprompt.go` | Builds the system prompt per model and mode               |
| `systemprompt_*.go` | Platform-specific shell detection (Unix/Windows)        |
| `tty.go`        | All interactive I/O (single reader), prompts                |
| `tty_*.go`      | Platform-specific TTY handle (Unix/Windows)                 |
| `ui.go`         | Logging levels, spinner, exit codes, string helpers         |
| `update.go`     | Self-update via GitHub Releases with caching                |
| `*_test.go`     | `config_test.go` (migration, flags, stream, lang tables), `tools_test.go` (path policy, tools), `completion_test.go`, `update_test.go` |

---

## Command dispatch (`main.go`)

```
os.Args[1] → switch:
  "ask"        → cmdAsk(rest)
  "models"     → cmdModels(rest)        // provider model list, a hint for `config model add`
  "config"     → cmdConfig(rest)
  "session"    → cmdSession(rest)
  "mode"       → cmdMode(rest)          // ask | read | auto
  "completion" → cmdCompletion(rest)
  "lang", "language" → cmdLanguage(rest)
  "update" / "purge" / "uninstall" / "nuke" → lifecycle
  "version"    → print version
  default      → cmdAsk(os.Args[1:])   ← the common path
```

The `default` branch means `clank "why is grub broken"` works without the `ask` subword. Conflict only when the question's first word is itself a subcommand name — for that, `clank ask ...` exists.

**Exit codes** (defined in `ui.go`):

| Constant      | Value | Meaning                                     |
|---------------|-------|---------------------------------------------|
| `exitOK`      | 0     | success                                     |
| `exitConfig`  | 1     | bad config / usage error                    |
| `exitAPI`     | 2     | API / network failure                       |
| `exitDeclined`| 3     | user interrupted execution (Ctrl-C)         |
| `exitNoAnswer`| 4     | model returned empty answer                 |

---

## Config CLI (`configcmd.go`)

No interactive wizard — everything is flags, so it works without a TTY.

```
clank config create <name> --api-url <url> [--api-key] [--proxy] [--reasoning-default] [--exec-timeout]
                    [--model <id> [--name] [--reasoning] [--tools] [--vision] [--context]]...
clank config edit   [-p name] [--rename] [--api-url] [--api-key] [--proxy] [--reasoning-default] [--exec-timeout]
clank config list | show [name] | use <name> | rm <name> [-y]
clank config model list|add|edit|rm|activate|move|enable|disable <id|N> [-p name] [flags]
clank models [-p name] [--all] [filter]
clank mode [show|ask|read|auto]
```

- `scanFlags` keeps flag **order**: in `create`, model flags apply to the last `--model`; a model flag before any `--model` is an error. Profile-level reasoning is `--reasoning-default`, so there is no ambiguity with the per-model `--reasoning`.
- `"-"` resets a field (`--proxy -`, `--api-key -`, `--reasoning -`, `--name -`).
- `<id|N>`: exact id match wins, otherwise a 1-based number from `config model list` (`Profile.findModel`).
- `--context` takes tokens with optional `k`/`m` (`128k` = 131072).
- Booleans are strict (`parseBool`): a typo is an error, not `false`.
- `--reasoning` takes a concrete level or `off`; `on`/`true` are rejected. `none` is normalized to `off`.
- `rm` asks `[y/N]` on the TTY; without a TTY it requires `-y`.
- Every mutation goes through `updateConfig` (see Config).
- `clank models` hides non-text models (embeddings, speech, image output) when the provider returns OpenRouter-style metadata, and prints `tools`/`vision`/`reasoning:<efforts>` tags from it. Nothing is cached and nothing is written to the config — capabilities are set by hand.

---

## The ask loop (`ask.go`)

### Flags (parsed manually, value flags accept `--x v` and `--x=v`)

- `-c/--context`, `-r/--resume`, `-v/--verbose`, `-q/--quiet`, `-i/--image <file>` (repeatable), `--`
- `-p/--profile <name>` — profile for this run
- `-m/--model <id|N>` — that model goes first (even if disabled), the rest of the enabled chain stays as fallback
- `--mode ask|read|auto` — permission mode for this run
- `--reasoning <level|off>` — overrides model and profile reasoning for this run

**Nothing set by flags is persisted** — not in the config, not in the session.

### Flow

```
1. Parse flags
2. Load config, resolve profile (-p or active), validate (api-url + ≥1 enabled model)
3. Build chain = enabled models in order (with -m moved to front)
4. gcSessions(); read stdin if -c or stdin is not a TTY
5. convo = [history (if -r)] + [user]   ← never contains the system message
6. Tool loop:
     a. chatWithFallback(profile, chain, build) → (result, idx)
        build(model) prepends a system prompt built for THAT model's
        tools/vision and the mode, and picks its tools and reasoning
     b. chain = rotateChain(chain, idx)   ← sticky fallback
     c. no tool_calls → final answer
     d. each tool_call → runToolCall(env, tc); on interrupt the remaining
        tool_calls get a "not run" tool message so history stays valid
7. Print final answer; saveSession() on all exits via finish()
```

**Sticky fallback**: when a fallback model answered, the chain is rotated so it is tried first on the next step, and the models before it go to the end. Not remembered between runs.

---

## Tools (`tools.go`)

| Tool | Kind | Notes |
|---|---|---|
| `list_dir(path, recursive, pattern)` | read | dirs end with `/`; `pattern` is a glob on names (recursive+pattern = find); cap 1000 entries |
| `read_file(path, offset, limit)` | read | numbered lines, default/max 2000 lines, 256 KB, binary refused |
| `grep(pattern, path, glob, ignore_case)` | read | RE2; `file:line:text`; cap 500 matches; skips binary and >10 MB files |
| `view_image(path)` | read | only when the model has `vision`; image goes as a follow-up user message |
| `write_file(path, content)` | write | creates parents; keeps the mode of an existing file |
| `edit_file(path, old_string, new_string, replace_all)` | write | exact match; 0 or >1 matches (without replace_all) is an error before any prompt |
| `run_shell_command(command)` | write | `sh -c`, see Shell execution |
| `question(question, options, default)` | — | asks the user on the TTY |

Schemas and tool results to the model are **English and never localized**. Caps protect the context; there is no hardcoded skip list (`.git`, `node_modules`) — the model narrows the path itself.

`toolsFor(model, mode)`: a model with `tools=false` gets nothing; `read` mode does not send write tools or the shell at all; `view_image` only with `vision`. If a model calls a write tool in `read` mode anyway, it gets an error.

### Path policy

`resolvePath`: `~` expansion → `Abs` → `Clean` → `EvalSymlinks` (for a non-existent path the nearest existing ancestor is resolved, the rest appended — it contains no `..` after Clean and cannot hold symlinks). Then:

- `inside` = `within(cwdReal, real)`, compared via `filepath.Rel`, **not** `HasPrefix` (`/a/proj-evil` is not inside `/a/proj`).
- `sensitive` = inside `~/.ssh`, `~/.gnupg` or the clank config dir.

| | inside cwd | outside cwd | sensitive |
|---|---|---|---|
| read, mode ask/read | auto | confirm | confirm |
| read, mode auto | auto | auto | confirm |
| write, mode ask | confirm | confirm | confirm |
| write, mode auto | auto | auto | confirm |

Operations run through `os.Root` (`openAt`): inside cwd the root is cwd itself, so neither `..` nor a symlink swapped after the check can escape; outside cwd (after confirmation) the root is the nearest existing ancestor of the target.

Declines go to the model as `NOT DONE: the user declined…` — the blunt wording is deliberate, small models otherwise report success.

---

## API client (`client.go`)

### Streaming and timeouts

Requests are `stream: true`; the SSE stream is accumulated into one message and printed whole, as before. Timeouts measure **silence**, not total length:

| Constant | Value | What |
|---|---|---|
| `dialTimeout` | 10s | TCP connect, TLS handshake |
| `firstEventTimeout` | 90s | from request to the first stream line (queue + time to first token) |
| `idleEventTimeout` | 30s | between stream lines |

Any line resets the timer, including keep-alive comments (`: PROCESSING`) and `reasoning_content` deltas. There is no overall `http.Client.Timeout`. A server that ignores `stream:true` and returns `application/json` is parsed as a whole response. `streamAcc` assembles `tool_calls` deltas by `index` (a new `id` at the same index means a new call), and `stripThinkBlocks` runs on the final text.

`http.Client` is cached per proxy value, so the tool loop reuses connections. `Authorization` is only sent when `api_key` is set (local servers).

### Call stack

```
cmdAsk
  └─ chatWithFallback(profile, chain, build)        → (result, index in chain)
       └─ for each model: chatCompleteRetry(...)    ← up to maxRetries=3, backoff 1s, 2s
            └─ chatComplete(profile, model, msgs, reqOpts{tools, reasoning})
                 └─ doStream() → POST /v1/chat/completions (SSE)
```

### Error classification (`classify`) and fallback

| Kind | Source | Attempts on the same model | Then |
|---|---|---|---|
| `errConnect` | DNS, connection refused, TLS — before any response | 2 | **stop**: the host is shared by all models of the profile |
| `errTransport` | connection broke mid-response | 3 | next model |
| `errTimeout` | silence past the timeouts | 1 | next model |
| `errServer` | 500, empty stream / 408, 502, 503, 504, 529 | 2 / 3 | next model |
| `errRateLimit` | 429 | 2 | next model |
| `errQuota` | 402, or 429 with quota/billing/credit in the body | 1 | next model |
| `errForbidden` | 403 (routers: model not allowed for this key) | 1 | next model |
| `errModelMissing` | body text | 1 | next model |
| `errContextOverflow` | body text | 1 | only models with a **bigger** `context`; current `context` unknown → stop with a hint |
| `errNoImages` | 400 + "image" + "not support/accept…" | — | strip images, retry the same model, print `ImagesDropped` once |
| `errBadEffort` | 400 + "reasoning/effort" + "invalid/must be one of…" | — | drop `reasoning_effort`, retry the same model, `warn` |
| `errNoTools` | 400 + "tool" + "not support…" | 1 | `ask.go` marks `tools=false` in the config, offers another tool model on the TTY, otherwise stops |
| `errTooLarge` | 413 | 1 | stop (proxy limit) |
| `errAuth`, `errBadRequest`, `errNotFound`, `errConfig` | | 1 | stop |

`Retry-After` (seconds or HTTP date) is honoured, but longer than `maxRetryWait` (10 s) means "next model" instead of waiting. Backoff otherwise: 1 s, 2 s. Body substrings are imprecise by nature — extend the lists in `classify` when a provider phrases it differently. Errors that arrive inside the stream (`data: {"error":…}`) are classified the same way.

`finish_reason`: `length` → `warn` (answer cut), `content_filter` → `warn`; neither triggers the fallback.

### Images and tool history

- A model with `vision=false` gets a copy of the dialogue with images removed (`stripImages`; `-i`, `view_image` follow-ups and `-r` history alike), and the user sees `Изображения не поддерживаются моделью, ответ генерируется без учёта изображения` once per run. The dialogue itself keeps the images for vision models.
- Once the dialogue contains `tool_calls` or `role:"tool"` (this run or from `-r`), models with `tools=false` leave the chain. No tool model left → stop.

### Reasoning

`reqOpts.reasoning`: `""` → the parameter is not sent; `"off"` → `reasoning_effort: "none"`; anything else is sent as-is. Effective value: `--reasoning` flag → `model.reasoning` → `profile.reasoning_default`. Reasoning text is never shown (`reasoning_content` isn't collected; `<think>/<thought>/<reasoning>` tags are stripped).

---

## Config (`config.go`)

### File location

`~/.config/clank/config.json` (hardcoded — intentionally consistent across macOS and Linux, avoids `~/Library/Application Support` on macOS).

### Structure

```go
ConfigFile {
    Active      string             // active profile name
    Profiles    map[string]Profile
    Mode        ToolMode           // "ask" | "read" | "auto"; "" = ask. Global, not per profile.
    Language    string             // "ru", "en", "" = auto-detect
    Completions []string           // completion scripts we installed
}

Profile {
    BaseURL          string        // without trailing /v1 — normalizeBaseURL() strips it
    APIKey           string        // optional, plaintext, file is 0600
    Proxy            string        // "" = HTTP_PROXY/HTTPS_PROXY env
    Models           []ModelEntry  // order = fallback order; first enabled = active
    ReasoningDefault string        // for models without their own reasoning
    ExecTimeoutSec   int           // 0 = defaultExecTimeout (10 min)
    Created          string        // RFC3339, used for auto-selection when Active is stale
}

ModelEntry {
    ID        string
    Name      string   // display only
    Tools     bool
    Vision    bool
    Reasoning string   // level | "off" | "" = profile default
    Context   int      // context size in tokens, 0 = unknown; used on context overflow
    Disabled  bool     // kept in place, skipped by the fallback
}
```

Capabilities live on the **model**: in a fallback chain neighbouring models often differ, and a per-profile `use_tools` used to break a fallback model without tool support with a 400.

### Saving

- `saveConfigFile` is atomic: temp file in the same dir → `fsync` → `rename`.
- `updateConfig(fn)` re-reads the file, applies `fn`, saves. **All mutations go through it**, so a stale in-memory snapshot (e.g. a long `clank` run in another tab) never reverts someone else's change.

### Migration from the old format

`Profile.UnmarshalJSON` / `ConfigFile.UnmarshalJSON` accept the old shape (`models: ["a","b"]`, `model`, profile-level `use_tools`/`vision`/`reasoning`/`reasoning_effort`, top-level `yolo`/`allowed_commands`):

- per-profile `use_tools`/`vision` are copied onto every model (`vision: null` meant "assume yes" → `true`);
- `reasoning: false` → `reasoning_default: "off"`; `true` + effort → that effort; `true` alone → `"max"` (what the old version sent);
- `yolo: true` → `mode: "auto"`; `allowed_commands` is dropped.

Loading never writes (it runs from `initLang` on every start). `isLegacyConfig` remembers the raw bytes, and the first save writes them to `config.json.bak` (only if no `.bak` exists yet).

### `activeProfile` / `resolveProfile`

If `cf.Active` points nowhere, `activeProfile` auto-selects `newestProfileName` (latest `Created`, ties alphabetical), saves via `updateConfig`, and logs a notice. `resolveProfile(cf, name)` is `-p` or the active one.

---

## Shell execution (`exec.go`)

### `execShell(command string, timeout time.Duration) execResult`

- Runs `sh -c <command>`
- Stdout and stderr go to **both** the user's stdout (live) and a `cappedWriter` buffer
- Stdin of the child is `/dev/tty` (not inherited stdin) — allows `sudo` password prompts even when clank's own stdin is a pipe
- Signal handling: `SIGINT` (Ctrl-C) is caught; child is in the **same process group** so it also receives SIGINT naturally. clank waits up to 3s for child to die, then `SIGKILL`. This is intentional — putting child in its own group would make it background and break `sudo` / `read` prompts via SIGTTIN.
- Timeout: `SIGTERM` first, then `SIGKILL` after 5s
- Returns `execResult{output, exitCode, interrupted, timedOut}`

Whether a command may run is decided only by the mode (`ask` → confirm every command, `auto` → run, `read` → shell is not offered). There is no command allow-list any more.

### `cappedWriter`

Caps output at `maxToolOutputBytes = 10MB`. Keeps **head** and **tail** (each `limit/2`), drops the middle with a note. Cuts on UTF-8 rune boundaries so the model doesn't see partial Cyrillic characters. `Write` always reports success to avoid breaking `io.MultiWriter`.

---

## Session management (`session.go`)

### Session identity

Sessions are per-TTY, not per-user. ID is `<tty_rdev_hex>-<ppid>`:
- `rdev` of `/dev/tty` — identifies the terminal device
- `ppid` — the shell PID of the terminal tab

Headless environments (no `/dev/tty`) use the fixed ID `"headless"`.

### Storage

`~/.config/clank/sessions/<id>.json`, written atomically, `0600`.

```go
sessionFile {
    Profile  string
    Updated  time.Time
    Messages []chatMessage  // no system message; rebuilt fresh each run
}
```

The mode is **not** stored in the session: `--mode` applies to one run only (the old `--yolo` leaked into `-r` this way).

- `loadSession(profile)` discards a session written by another profile (it may contain `role:"tool"` messages) with a notice. Don't suppress that notice.
- `saveSession` is called via `finish()` in `cmdAsk` on **all** exit paths, including API errors.
- `gcSessions` removes files older than 7 days whose parent shell is dead.

---

## System prompt (`systemprompt.go`)

### `buildSystemPrompt(useTools, vision bool, mode ToolMode) string`

Built for **each model attempt** (the fallback may land on a model without tools or vision — then the prompt stops promising them). Follows the UI language. Contains:
- Role, answer rules, explicit "no markdown" instruction
- cwd, hostname, date, detected shell, OS/arch; session-history note
- With tools: act via tools; look at files with `list_dir`/`read_file`/`grep`, not the shell (those need no confirmation); `question` only for real forks; never announce calls
- `view_image` paragraph only with `vision`
- `read` mode: no changes possible, suggest rerunning with `--mode ask`; otherwise: `edit_file`/`write_file` rules, shell for the rest, destructive only on request
- Without tools: suggest a one-liner pipe to `clank -r`

The tool list itself is NOT enumerated — schemas are in the request. The prompt carries policy, schemas carry mechanics.

### `detectShell`

Uses `ps -p <ppid>` to find the actual current shell, not `$SHELL`. Login shells (`-zsh`) are normalized.

---

## Localization (`lang.go`, `lang_ru.go`, `lang_en.go`)

All user-visible strings live in `Msgs` tables (`RU`, `EN`); code uses `M.Field`. New language = new `lang_xx.go` table + one branch in `applyLang`. `TestLangTablesComplete` fails on an empty field or on format verbs that differ between languages. Tool schemas and tool results stay English.

- `initLang()` runs first in `main()`: explicit `ConfigFile.Language` wins, otherwise `LC_ALL` → `LC_MESSAGES` → `LANG` (`ru*` → RU, anything else → EN).
- `clank lang [en|ru|auto]` (alias `language`) shows or sets the override and reinstalls recorded completion scripts.
- `clank help [cmd [sub]]` routes to per-topic help (`help config create`, `help config model`).
- Completion scripts are generated from one template per shell (`zshScript`, `fishScript`, `bashScript`) and a description table per language (`compEN`, `compRU`). `completion_test.go` checks that every `case "…":` in `main.go` and `configcmd.go` and every ask flag appears in every script.

---

## Release body & update verification

- Release body = NOTES from the tag annotation + GitHub generated notes.
  Checksums are NEVER in the body — they ride as the `checksums.txt` asset.
- `release.yml` fetches the tag object explicitly before reading the
  annotation (`for-each-ref ... %(contents:body)`); checkout alone doesn't
  guarantee it — v1.6 shipped a checksums-only body because of this.
- `clank update` strips any checksums block from the displayed notes
  (`stripChecksumsSection`, covered by `update_test.go`) — old releases
  still carry it in the body.
- Downloads are verified against `checksums.txt` (`verifyArchive`, SHA-256).
  Releases without it (≤v1.6) install with a warning.

---

## TTY and interactive I/O (`tty.go`)

**Single global `/dev/tty` reader** (`ttyOnce sync.Once`). This is non-negotiable: `bufio.Reader` buffers ahead; two readers on the same fd would lose data. All interactive prompts use `ttyIO()`.

stdin is deliberately **not** used for prompts — it may be carrying a context pipe.

| Function                                   | Purpose                                          |
|--------------------------------------------|--------------------------------------------------|
| `ttyIO() (*os.File, *bufio.Reader, error)` | Open/return the single tty handle                |
| `haveTTY() bool`                           | Whether we have a terminal at all                |
| `askLine(prompt) (string, bool)`           | Read one line from tty; ok=false on EOF/no-tty   |
| `confirmYN(prompt, defaultYes) bool`       | Y/N prompt with re-ask on garbage input          |
| `confirmTool(title, body) bool`            | Show what the model wants, ask y/N; no TTY / EOF / empty = no |
| `promptQuestion(q, options, default)`      | The `question` tool                              |

`confirmYN` loops on unrecognized input instead of treating it as "no". `confirmTool` writes the action and the prompt to the **same fd** (`/dev/tty`); with `2>log` the user still sees what they confirm.

---

## Logging (`ui.go`)

All diagnostic output → **stderr**. All model output and command stdout → **stdout**. This means `clank "..." > file` captures both the model answer and command output in the file.

| Function         | Level condition     | Prefix   | Notes                              |
|------------------|---------------------|----------|------------------------------------|
| `detail()`       | `vVerbose` only     | `  · `   | API timing, stream stats, counts   |
| `info()`         | `vNormal` and above | none     | fallback switches, tool actions    |
| `warn()`         | always              | `! `     | data loss risk, degraded operation |
| `fail()`         | always              | `ошибка:`/`error:` | fatal errors before exit |

### Spinner

`startSpinner(msg) *spinner` — animated frames on stderr, only when not quiet and stderr is a TTY. Nil-safe.

### `sanitizeForDisplay(s string)`

Replaces control characters (except `\n` and `\t`) with `\xNN` / `\uNNNN` escapes. Prevents `\r`/ANSI sequences from overwriting the confirmation prompt — otherwise `"echo ok\r\033[2Krm -rf ~"` could display one thing and run another.

---

## Key invariants / design rules

1. **No external deps.** Stdlib only. Don't add modules.
2. **`content omitempty` on chatMessage.** Some servers reject `"content": ""` on assistant messages that have `tool_calls`.
3. **System message never in the saved session or in `convo`.** It is built per model attempt in `build()`.
4. **Save session on all exit paths** through `finish()`.
5. **Single tty reader.** All prompts go through `ttyIO()`.
6. **Child process in same process group** (sudo prompts).
7. **Fallback triggers only on technical failures** (table above), never on a "bad" answer.
8. **`normalizeBaseURL` is idempotent.**
9. **All user-visible model-provided text goes through `sanitizeForDisplay`** (commands, paths, previews).
10. **`cappedWriter.Write` always returns `(n, nil)`.**
11. **Config mutations go through `updateConfig`**, never load-modify-`saveConfigFile` across a long-running operation.
12. **File tools operate through `os.Root`** from `openAt` after `resolvePath`; never `os.ReadFile`/`os.WriteFile` on a model-supplied path directly.
13. **Run flags are never persisted** (`-p`, `-m`, `--mode`, `--reasoning`).

---

## Config file format (reference)

```json
{
  "active": "home",
  "mode": "ask",
  "language": "",
  "profiles": {
    "home": {
      "base_url": "https://api.example.com",
      "api_key": "sk-...",
      "proxy": "",
      "reasoning_default": "low",
      "exec_timeout_sec": 0,
      "created": "2026-11-13T10:00:00Z",
      "models": [
        {"id": "big-model", "name": "Big", "tools": true, "vision": true, "reasoning": "high", "context": 1048576},
        {"id": "small-model", "tools": true, "vision": false},
        {"id": "flaky-model", "tools": false, "vision": false, "disabled": true}
      ]
    }
  }
}
```

---

## Things to watch out for

- **`-r` with a different profile** in the session file: history is discarded with a notice.
- **Reasoning models** (`reasoning_content`, `<think>` blocks): handled in `chatComplete`/`streamAcc`. Extend those, not the callers.
- **`confirmYN` with no TTY** returns `defaultYes`; **`confirmTool` with no TTY** always declines.
- **`config rm` without a TTY** needs `-y`.
- **Spinner** is nil-safe.
