# WhatsApp MCP Server

This is a Model Context Protocol (MCP) server for WhatsApp, built for **Claude Code**.

With this you can search and read your personal WhatsApp messages (including images, videos, documents, and audio messages), search your contacts and send messages to either individuals or groups. You can also send media files including images, videos, documents, and audio messages.

It connects to your **personal WhatsApp account** directly via the WhatsApp Web multidevice API (using the [whatsmeow](https://github.com/tulir/whatsmeow) library). All your messages are stored locally in a SQLite database and only sent to an LLM (such as Claude) when the agent accesses them through tools (which you control).

> *Caution:* as with many MCP servers, the WhatsApp MCP is subject to [the lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/). This means that prompt injection could lead to private data exfiltration.

---

## Install it by asking Claude Code

The fastest path: open Claude Code in any directory and paste this prompt.

```
Install the WhatsApp MCP server from https://github.com/leandrohmvieira/whatsapp-mcp
onto yourself, following the "Instructions for Claude Code" section of its README:
https://raw.githubusercontent.com/leandrohmvieira/whatsapp-mcp/main/README.md

I'll scan the QR code when you get to that step.
```

Claude Code will clone the repo, start the WhatsApp bridge, show you a QR code to scan with your phone, and register the MCP server with itself. The only thing you have to do by hand is scan the QR code and restart Claude Code at the end.

---

## Instructions for Claude Code

**This section is written for the agent.** If you are Claude Code and the user asked you to install this, follow these steps in order. Do not skip the verification step, and do not run the bridge in a way that hides the QR code from the user.

### Step 0 — Check the prerequisites

Run these and report anything that is missing before going further:

```bash
go version      # need Go 1.25 or newer
uv --version    # the Python package manager (installs Python 3.11+ itself)
claude --version
ffmpeg -version # OPTIONAL, only for sending playable voice messages
```

If `go` is missing, install it from https://go.dev/dl/ (or `winget install GoLang.Go`, `brew install go`, your distro's package manager).

If `uv` is missing:

```bash
# macOS / Linux
curl -LsSf https://astral.sh/uv/install.sh | sh
# Windows (PowerShell)
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

Note the **absolute path** to the `uv` executable — you need it in step 3, and MCP servers do not inherit a login shell's PATH:

```bash
# macOS / Linux
which uv                              # e.g. /Users/you/.local/bin/uv
# Windows (PowerShell)
(Get-Command uv).Source               # e.g. C:\Users\you\.local\bin\uv.exe
```

### Step 1 — Clone the repository

Pick a stable, permanent location — the MCP registration stores an absolute path to this clone, and the Python server reads the message database at `../whatsapp-bridge/store/messages.db` relative to itself. **Moving or renaming the folder later breaks the install**, and the two halves must stay in the same clone.

Do not clone it into a temp directory or inside another project.

```bash
git clone https://github.com/leandrohmvieira/whatsapp-mcp.git
cd whatsapp-mcp
```

Record the absolute path of the clone (`pwd`, or `(Get-Location).Path` on Windows). Call it `REPO`.

### Step 2 — Start the WhatsApp bridge and let the user scan the QR code

The bridge is a Go program that logs into WhatsApp and keeps a local SQLite copy of the messages. On first run it prints a QR code that **the user must scan with their phone**, so it has to run somewhere they can actually see it.

Build it first — the binary makes every later restart fast:

```bash
cd whatsapp-bridge
go build -o whatsapp-bridge .        # Windows: go build -o whatsapp-bridge.exe .
```

The first build downloads dependencies and can take a couple of minutes. No C compiler is needed (see [Windows compatibility](#windows-compatibility)).

> **Always start the bridge from inside `whatsapp-bridge/`.** It creates its `store/` database
> directory **relative to the current working directory**, while the Python server always looks for
> it at `<repo>/whatsapp-bridge/store/messages.db`. Launch it from anywhere else and you get a
> second, orphaned database — the bridge looks healthy, asks for a fresh QR scan, and the MCP tools
> report no messages, with nothing obviously wrong. Every command below `cd`s first for this reason.

Now launch it **in a separate, visible terminal window** so the QR renders properly:

```powershell
# Windows (PowerShell)
Start-Process powershell -ArgumentList '-NoExit','-Command',"cd '$REPO\whatsapp-bridge'; .\whatsapp-bridge.exe"
```

```bash
# macOS
osascript -e "tell app \"Terminal\" to do script \"cd '$REPO/whatsapp-bridge' && ./whatsapp-bridge\""
```

```bash
# Linux — use whatever terminal is installed
gnome-terminal -- bash -c "cd '$REPO/whatsapp-bridge' && ./whatsapp-bridge; exec bash"
```

If you cannot open a separate window, ask the user to run the bridge themselves in their own terminal — **do not** run it as a silent background job on the first run, because the QR code will never reach them and the install will stall with no visible reason.

Then tell the user, in plain words:

> Open WhatsApp on your phone → **Settings → Linked Devices → Link a Device**, and scan the QR code in the window that just opened.

Wait for them to confirm. A successful login prints `Successfully connected and authenticated!` followed by `Starting REST API server on :8080...`. History sync then runs for several minutes in the background — that is normal, and it is fine to continue.

Leave this bridge running. It must be up whenever the MCP tools are used.

### Step 3 — Register the MCP server with Claude Code

Use the absolute `uv` path from step 0 and the absolute `REPO` path from step 1:

```bash
claude mcp add whatsapp --scope user -- /absolute/path/to/uv --directory /absolute/path/to/whatsapp-mcp/whatsapp-mcp-server run main.py
```

```powershell
# Windows (PowerShell) — same command, Windows paths
claude mcp add whatsapp --scope user -- C:\Users\you\.local\bin\uv.exe --directory C:\Users\you\whatsapp-mcp\whatsapp-mcp-server run main.py
```

`--scope user` makes WhatsApp available in every project on this machine, which is almost always what you want. Use `--scope local` instead to limit it to the current directory.

To enable the optional offline transcription of voice notes at the same time, add `-e WHATSAPP_MEDIA_TRANSCRIPTION=true` before the `--` (see [Media Transcription](#media-transcription-optional-feature) — it also needs its dependencies installed).

### Step 4 — Verify, then hand off

```bash
claude mcp get whatsapp     # should report ✔ Connected
```

`claude mcp get` health-checks the server, so a ✔ here means `uv` launched the Python server successfully — it installs the Python dependencies on that first launch, which can take a minute. It does **not** prove the bridge is reachable; that is what the next step checks.

A healthy server exposes **12 tools** (13 with transcription enabled). If the count is lower, something failed to load.

The MCP tools only appear in a Claude Code session that started *after* the server was registered, so you cannot call them from this session. Tell the user:

> Restart Claude Code, then ask me "list my WhatsApp chats" to confirm it works end to end.

If `claude mcp get` reports a failure, run the server by hand to see the real error — the MCP transport swallows it otherwise:

```bash
cd "$REPO/whatsapp-mcp-server" && uv run main.py
```

A healthy server prints nothing and waits on stdin — that silence *is* success, so Ctrl-C out of it. A failure prints a traceback instead.

---

## Manual installation

If you'd rather do it yourself, the same four steps condensed:

1. Install Go 1.25+, [uv](https://docs.astral.sh/uv/), and Claude Code. FFmpeg is optional (voice messages only).
2. `git clone https://github.com/leandrohmvieira/whatsapp-mcp.git` into a permanent location.
3. `cd whatsapp-bridge && go run main.go`, then scan the QR code with **WhatsApp → Settings → Linked Devices → Link a Device**. Keep it running, and always start it from inside `whatsapp-bridge/`.
4. From the **repo root**, `claude mcp add whatsapp --scope user -- $(which uv) --directory "$(pwd)/whatsapp-mcp-server" run main.py`, then restart Claude Code.

## Keeping the bridge running

The Go bridge is the piece that talks to WhatsApp. When it is not running, every MCP tool fails with a connection error — the Python server can still read the message history from SQLite, but nothing new arrives and nothing can be sent.

- Start it again any time with `./whatsapp-bridge` (Windows: `.\whatsapp-bridge.exe`) from the `whatsapp-bridge/` directory. After the first login it reconnects silently, with no QR code.
- It listens on **`localhost:8080`**, which is hardcoded. Only one bridge can run per machine, and the port must be free.
- Sessions expire after roughly 20 days, after which it will print a fresh QR code to scan.
- To have it start with the machine, add the binary to your OS's startup mechanism (Windows Task Scheduler / a launchd agent / a systemd user unit).

## Windows compatibility

The bridge uses a **pure-Go SQLite driver** ([`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)), so **no C compiler and no CGO** are required. It builds and runs out of the box on Windows (and cross-compiles to other platforms) with the default `CGO_ENABLED=0`.

> **Note:** Earlier versions used the cgo-based `go-sqlite3`, which required installing a C compiler (MSYS2) and setting `CGO_ENABLED=1` on Windows. That is no longer needed. If you previously ran `go env -w CGO_ENABLED=1`, you can leave it — the build works either way.

## Architecture Overview

This application consists of two main components:

1. **Go WhatsApp Bridge** (`whatsapp-bridge/`): A Go application that connects to WhatsApp's web API, handles authentication via QR code, and stores message history in SQLite. It serves as the bridge between WhatsApp and the MCP server, and exposes a small REST API on `localhost:8080`.

2. **Python MCP Server** (`whatsapp-mcp-server/`): A Python server implementing the Model Context Protocol (MCP), which provides standardized tools for Claude to interact with WhatsApp data and send/receive messages.

### Data Storage

- All message history is stored in a SQLite database within the `whatsapp-bridge/store/` directory
- The database maintains tables for chats and messages
- Messages are indexed for efficient searching and retrieval
- The Python server locates that database *relative to its own file*, which is why both halves must stay inside the same clone

## Usage

Once connected, you can interact with your WhatsApp contacts through Claude, leveraging Claude's AI capabilities in your WhatsApp conversations.

### MCP Tools

Claude can access the following tools to interact with WhatsApp:

- **search_contacts**: Search for contacts by name or phone number
- **list_messages**: Retrieve messages with optional filters and context
- **list_chats**: List available chats with metadata
- **get_chat**: Get information about a specific chat
- **get_direct_chat_by_contact**: Find a direct chat with a specific contact
- **get_contact_chats**: List all chats involving a specific contact
- **get_last_interaction**: Get the most recent message with a contact
- **get_message_context**: Retrieve context around a specific message
- **send_message**: Send a WhatsApp message to a specified phone number or group JID
- **send_file**: Send a file (image, video, raw audio, document) to a specified recipient
- **send_audio_message**: Send an audio file as a WhatsApp voice message (requires the file to be an .ogg opus file or ffmpeg must be installed)
- **download_media**: Download media from a WhatsApp message and get the local file path
- **transcribe_media**: Transcribe a voice note / audio / video message to text — **opt-in**, only registered when the media-transcription feature is enabled (see below)

### Media Handling Features

The MCP server supports both sending and receiving various media types:

#### Media Sending

You can send various media types to your WhatsApp contacts:

- **Images, Videos, Documents**: Use the `send_file` tool to share any supported media type.
- **Voice Messages**: Use the `send_audio_message` tool to send audio files as playable WhatsApp voice messages.
  - For optimal compatibility, audio files should be in `.ogg` Opus format.
  - With FFmpeg installed, the system will automatically convert other audio formats (MP3, WAV, etc.) to the required format.
  - Without FFmpeg, you can still send raw audio files using the `send_file` tool, but they won't appear as playable voice messages.

#### Media Downloading

By default, just the metadata of the media is stored in the local database. The message will indicate that media was sent. To access this media you need to use the `download_media` tool which takes the `message_id` and `chat_jid` (which are shown when printing messages containing the media), this downloads the media and then returns the file path which can be then opened or passed to another tool.

## Media Transcription (optional feature)

Voice notes and audio/video messages arrive as empty placeholders — an agent can `download_media`
them but can't *read* them. This optional feature adds a **`transcribe_media`** tool that turns them
into text, **fully offline** (nothing leaves your machine). It is **off by default** and its
dependencies are not installed unless you opt in, so users who don't need it carry no extra weight.

### 1. Install the dependencies

From the repository root:

```bash
python install-media.py            # faster-whisper backend (default) — CPU or NVIDIA GPU
python install-media.py --model small   # also pre-download a model
```

This pulls in 16 extra packages (faster-whisper, CTranslate2, ONNX Runtime, PyAV, NumPy) — a
gigabyte or so, hence the opt-in.

The helper script needs a system Python on your PATH. If you don't have one — `uv` provides Python
to the project but not to your shell, and on Windows a bare `python` just opens the Microsoft Store —
run the equivalent step directly instead, which needs nothing but `uv`:

```bash
cd whatsapp-mcp-server && uv sync --extra transcription
```

### 2. Enable the feature

The flag lives in the MCP server's environment. Claude Code has no in-place edit for an existing
server, so re-register it with the env var:

```bash
claude mcp remove whatsapp --scope user
claude mcp add whatsapp --scope user -e WHATSAPP_MEDIA_TRANSCRIPTION=true -- /absolute/path/to/uv --directory /absolute/path/to/whatsapp-mcp/whatsapp-mcp-server run main.py
```

Then restart Claude Code. When the flag is off (or unset), `transcribe_media` is simply not registered.

### 3. Use it

Call `transcribe_media(message_id, chat_jid)` (both shown in the placeholder when you print messages).
It downloads the media if needed and returns `{ text, language, duration, backend, model }`.

### Backends

| Backend | Best for | Setup |
|---|---|---|
| **faster-whisper** (default) | CPU, or **NVIDIA** GPU (CUDA) | just `install-media.py` |
| **whisper-cpp** | **AMD** GPU (Vulkan) or any self-built whisper.cpp | build whisper.cpp + point env vars at it |

Configuration via environment variables on the MCP server (pass each with its own `-e` on
`claude mcp add`):

```
WHATSAPP_TRANSCRIPTION_BACKEND   faster-whisper | whisper-cpp     (default: faster-whisper)
WHISPER_MODEL                    faster-whisper size (tiny|base|small|medium|large-v3) — default: small
WHISPER_LANGUAGE                 force a language (e.g. pt, en) or auto (default: auto)
WHISPER_DEVICE                   faster-whisper: cpu | cuda | auto (default: auto)
# whisper-cpp backend only:
WHISPER_CPP_BIN                  path to a prebuilt whisper-cli(.exe)
WHISPER_CPP_MODEL                path to a ggml .bin model
```

> **AMD GPUs:** faster-whisper (CTranslate2) can't use a Radeon — CUDA/CPU only. For GPU acceleration
> on AMD, build [`whisper.cpp`](https://github.com/ggml-org/whisper.cpp) with the **Vulkan** backend
> (`cmake -DGGML_VULKAN=ON`, needs the Vulkan SDK), download a ggml model, and set
> `WHATSAPP_TRANSCRIPTION_BACKEND=whisper-cpp` with `WHISPER_CPP_BIN` / `WHISPER_CPP_MODEL`.

## Technical Details

1. Claude sends requests to the Python MCP server
2. The MCP server queries the Go bridge for WhatsApp data or directly to the SQLite database
3. The Go bridge accesses the WhatsApp API and keeps the SQLite database up to date
4. Data flows back through the chain to Claude
5. When sending messages, the request flows from Claude through the MCP server to the Go bridge and to WhatsApp

## Troubleshooting

### The MCP server won't connect

- Run `claude mcp get whatsapp` to see its status, and `claude mcp list` to confirm it is registered at all.
- Reproduce the real error by starting the server by hand: `cd whatsapp-mcp-server && uv run main.py`.
- Use an **absolute path to `uv`** in the registration. MCP servers don't inherit a login shell's PATH, so a bare `uv` often works in your terminal and fails as an MCP server.
- The tools only appear in sessions started *after* registration — restart Claude Code.
- Moved or renamed the clone? The registered path is absolute. Re-register with `claude mcp remove whatsapp --scope user` followed by a fresh `claude mcp add`.

### Tools fail with a connection error

- Make sure the Go bridge is running — that is the most common cause.
- Check that nothing else is occupying `localhost:8080`, and that only one bridge is running.

### Authentication issues

- **QR Code Not Displaying**: If the QR code doesn't appear, try restarting the bridge. If issues persist, check if your terminal supports displaying QR codes — a small font size helps, since the code needs the whole square to be visible.
- **WhatsApp Already Logged In**: If your session is already active, the Go bridge will automatically reconnect without showing a QR code.
- **Device Limit Reached**: WhatsApp limits the number of linked devices. If you reach this limit, you'll need to remove an existing device from WhatsApp on your phone (Settings > Linked Devices).
- **No Messages Loading**: After initial authentication, it can take several minutes for your message history to load, especially if you have many chats.
- **WhatsApp Out of Sync**: If your WhatsApp messages get out of sync with the bridge, delete both database files (`whatsapp-bridge/store/messages.db` and `whatsapp-bridge/store/whatsapp.db`) and restart the bridge to re-authenticate.

### Other

- If you encounter permission issues when running uv, you may need to add it to your PATH or use the full path to the executable.
- Both the Go bridge and the Python MCP server must be able to run for the integration to work.

## Credits

This is a fork of [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), adding a working media download path, opt-in offline transcription, and a CGO-free Windows build.
