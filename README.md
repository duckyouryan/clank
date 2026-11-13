_Читать на русском: [README.ru.md](README.ru.md)_

# clank

A small CLI agent for the terminal.

```bash
dmesg | tail -50 | clank "what's wrong here"
clank "why doesn't grub see the second disk"
```

Works with any OpenAI-compatible API: cloud providers, local Ollama / LocalAI / vLLM.


## What it can do

- Answers questions in plain text, no markdown garbage (depends on used model).
- Reads context from a pipe: logs, diffs, output of any commands.
- Remembers the conversation within the terminal tab — `clank -r "now this way"`.
- Can run commands itself to figure things out.
- If the question is unclear — asks back, suggests options.
- Understands images: `clank -i screen.png "what's wrong here"`, or asks permission to read them (e.g. for `clank "find all pictures with triangles"` it loads them itself).


## Installation

Release builds are on the releases page: https://github.com/Gangsta0607/clank/releases
Put it in your PATH and you're good to go.

## Install from source

```bash
git clone https://github.com/Gangsta0607/clank.git
cd clank
make
sudo make install
```


## Quick start

```bash
clank config create home --api-url https://api.example.com --api-key sk-... \
  --model some-model --tools true --vision true
clank "connection check"
```

`clank models` lists what the provider offers. If you have several providers — create a profile for each (`clank config create ...`) and switch between them (`clank config use ...`); for a single run use `-p <profile>`.

What the model may do is set by the mode: `clank mode ask` (default: reads freely, file writes and commands need confirmation), `read` (read-only) or `auto` (no confirmations).


The full list of commands and flags is in the built-in help:

```bash
clank help
```

## License

MIT / Public Domain.
