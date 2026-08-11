# WhatsApp MCP Server

This is a Model Context Protocol (MCP) server for WhatsApp, built for **Claude Code**.

With this you can search and read your personal WhatsApp messages (including images, videos, documents, and audio messages), search your contacts and send messages to either individuals or groups. You can also send media files including images, videos, documents, and audio messages.

It connects to your **personal WhatsApp account** directly via the WhatsApp Web multidevice API (using the [whatsmeow](https://github.com/tulir/whatsmeow) library). All your messages are stored locally in a SQLite database and only sent to an LLM (such as Claude) when the agent accesses them through tools (which you control).

> *Caution:* as with many MCP servers, the WhatsApp MCP is subject to [the lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/). This means that prompt injection could lead to private data exfiltration.

## What you need

- **Go 1.25+** — [go.dev/dl](https://go.dev/dl/), or `winget install GoLang.Go` / `brew install go`
- **uv** — the Python package manager; it brings its own Python, so you don't need one installed
  - macOS / Linux: `curl -LsSf https://astral.sh/uv/install.sh | sh`
  - Windows (PowerShell): `powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"`
- **Claude Code**
- **FFmpeg** — optional, only if you want to send playable voice messages
- Your **phone**, to scan a QR code once

## Setup

The whole thing takes about ten minutes, most of it waiting for the first build.

### 1. Clone the repository

Put it somewhere permanent — your home directory is fine. The setup records the full path to this
folder, so **moving or renaming it later breaks the install**.

```bash
git clone https://github.com/leandrohmvieira/whatsapp-mcp.git
cd whatsapp-mcp
```

### 2. Start the WhatsApp bridge and link your phone

The bridge is the piece that talks to WhatsApp and keeps a local copy of your messages. Open a
terminal, go into the `whatsapp-bridge` folder, and start it:

```bash
cd whatsapp-bridge
go run main.go
```

The first run downloads dependencies and takes a couple of minutes. Then a QR code appears in the
terminal. On your phone, open **WhatsApp → Settings → Linked Devices → Link a Device** and scan it.

You'll see `Successfully connected and authenticated!` followed by
`Starting REST API server on :8080...`. Your message history then syncs in the background, which can
take a few minutes if you have a lot of chats.

**Leave this terminal open.** The bridge has to keep running for any of this to work — see
[Keeping the bridge running](#keeping-the-bridge-running) below.

> **Always start the bridge from inside the `whatsapp-bridge` folder.** It creates its database in
> whatever folder you launch it from, and the rest of the setup expects it in
> `whatsapp-bridge/store/`. Start it from somewhere else and you get a second, orphaned database:
> everything *looks* fine, but it asks you to scan a new QR code and Claude finds no messages.

### 3. Tell Claude Code about it

In a **second** terminal, from anywhere, run the command below. You need two absolute paths: where
`uv` lives, and where you cloned this repo.

```bash
# macOS / Linux — find your uv path with:  which uv
claude mcp add whatsapp --scope user -- /Users/you/.local/bin/uv --directory /Users/you/whatsapp-mcp/whatsapp-mcp-server run main.py
```

```powershell
# Windows (PowerShell) — find your uv path with:  (Get-Command uv).Source
claude mcp add whatsapp --scope user -- C:\Users\you\.local\bin\uv.exe --directory C:\Users\you\whatsapp-mcp\whatsapp-mcp-server run main.py
```

Use the **full path to `uv`**, not just `uv`. MCP servers don't inherit your terminal's PATH, so the
bare name usually works when you type it and then fails here.

`--scope user` makes WhatsApp available in all your projects, which is normally what you want. Swap
in `--scope local` to limit it to the current folder.

### 4. Check it and restart

```bash
claude mcp get whatsapp
```

You want `✔ Connected`. The first launch installs the Python dependencies, so give it a minute.

Now **restart Claude Code** — new tools only show up in sessions started after you add them. Then
ask it something like *"list my WhatsApp chats"*. If you get your chats back, you're done.

If anything went wrong, see [Troubleshooting](#troubleshooting).

## Prefer to have Claude Code install it?

Claude Code can do the whole setup itself. Open it in any folder and paste:

```
Install the WhatsApp MCP server from https://github.com/leandrohmvieira/whatsapp-mcp
onto yourself, following its AGENTS.md:
https://raw.githubusercontent.com/leandrohmvieira/whatsapp-mcp/main/AGENTS.md

I'll scan the QR code when you get to that step.
```

It clones the repo, builds and starts the bridge, shows you the QR code, and registers the server
with itself. You still scan the code with your phone and restart Claude Code at the end.

It will also ask, once, whether you want offline transcription of voice notes — a gigabyte of
dependencies that lets Claude read audio messages instead of seeing empty placeholders. Saying no is
fine; you can turn it on later from [Media Transcription](#media-transcription-optional-feature).

The instructions it follows live in [AGENTS.md](./AGENTS.md).

## Keeping the bridge running

The Go bridge is what talks to WhatsApp. While it's stopped, Claude can still read the messages
already synced, but nothing new arrives and nothing can be sent.

- Start it again any time with `go run main.go` from the `whatsapp-bridge` folder. After the first
  login it reconnects silently — no QR code.
- `go run` is the recommended way to start it, including day to day. You *can* compile a standalone
  binary with `go build -o whatsapp-bridge .` for a slightly faster start, but on work or otherwise
  managed Windows machines a freshly built unsigned `.exe` tends to get blocked by application
  control, SmartScreen or antivirus — `go run` avoids that entirely and only costs a second or two.
- It uses **`localhost:8080`**, which is fixed. Only one bridge can run at a time, and that port has
  to be free.
- Sessions expire after about 20 days, after which it shows a fresh QR code to scan.
- To start it automatically with your machine, add it to Windows Task Scheduler, a launchd agent, or
  a systemd user unit.

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
- Use the **full path to `uv`** in the registration. MCP servers don't inherit your terminal's PATH, so a bare `uv` often works when you type it and fails as an MCP server.
- The tools only appear in sessions started *after* you register the server — restart Claude Code.
- Moved or renamed the folder? The recorded path is absolute. Run `claude mcp remove whatsapp --scope user` and add it again with the new path.
- To see the real error, start the server by hand: `cd whatsapp-mcp-server && uv run main.py`. A healthy server prints nothing and waits — that silence *is* success, so press Ctrl-C. A broken one prints an error.

### Claude finds no messages, or tools fail with a connection error

- Check that the Go bridge is still running. That's the usual cause.
- Make sure nothing else is using `localhost:8080`, and that you only have one bridge running.
- If you once started the bridge from the wrong folder, look for a stray `store/` directory outside `whatsapp-bridge/` and delete it, then restart the bridge from the right place.

### Windows blocks the bridge from starting

If you compiled a binary and Windows refuses to run it — a SmartScreen "Windows protected your PC"
box, an antivirus warning, an app-control or AppLocker message, or the executable vanishing after
the build — don't fight it. Start the bridge with `go run main.go` instead, which is the documented
way anyway. Managed machines routinely block unsigned executables in user folders, and `go run`
executes from the Go build cache without tripping those rules.

Delete the stray binary if one was built (`whatsapp-bridge.exe` in `whatsapp-bridge/`); nothing
needs it.

### Windows: "running scripts is disabled on this system"

PowerShell refuses to run script files until you allow it. You'll see this as
`... .ps1 cannot be loaded because running scripts is disabled on this system`, most often on the
`uv` install line or on `claude` itself when it was installed through npm.

Fix it for your account — no admin rights needed:

```powershell
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned
```

Close the terminal, open a new one, and re-run the step that failed. `RemoteSigned` permits scripts
you wrote locally and requires a signature on downloaded ones, which is the usual recommended
setting.

If it's still blocked, check who's enforcing it:

```powershell
Get-ExecutionPolicy -List
```

If **MachinePolicy** or **UserPolicy** is anything other than `Undefined`, your machine is managed by
Group Policy and the per-user setting can't override it. Work around it per command with
`powershell -ExecutionPolicy Bypass -Command "..."`, or use `cmd.exe` instead, where the policy
doesn't apply.

### Authentication issues

- **QR Code Not Displaying**: If the QR code doesn't appear, restart the bridge. If it still doesn't, your terminal may be too small to draw it — shrink the font size so the whole square fits.
- **WhatsApp Already Logged In**: If your session is already active, the Go bridge reconnects automatically without showing a QR code.
- **Device Limit Reached**: WhatsApp limits the number of linked devices. If you reach this limit, remove an existing device from your phone (Settings > Linked Devices).
- **No Messages Loading**: After the initial link, it can take several minutes for your history to load, especially with many chats.
- **WhatsApp Out of Sync**: If your messages get out of sync with the bridge, delete both database files (`whatsapp-bridge/store/messages.db` and `whatsapp-bridge/store/whatsapp.db`) and restart the bridge to link again.

### Other

- If you hit permission issues running uv, add it to your PATH or use the full path to the executable.
- Both the Go bridge and the Python MCP server must be able to run for the integration to work.

## Credits

This is a fork of [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), adding a working media download path, opt-in offline transcription, and a CGO-free Windows build.
