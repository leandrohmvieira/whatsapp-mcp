# AGENTS.md — installing this server with Claude Code

**This file is written for the agent.** If you are Claude Code and the user asked you to install the
WhatsApp MCP server onto yourself, follow these steps in order.

Four things make this install fail in ways that are hard to diagnose afterwards, so don't improvise
around them:

- Register the server with an **absolute path to `uv`** — MCP servers don't inherit a login shell's PATH.
- Clone to a **permanent location** — the registration and the message-database lookup are both path-bound.
- Start the bridge in a **terminal the user can see** — the first run prints a QR code they must scan.
- Start the bridge **from inside `whatsapp-bridge/`** — its database path is relative to the working directory.

Human-readable setup instructions, the tool list, and the optional transcription feature are in
[README.md](./README.md).

## Step 0 — Check the prerequisites

Run these and report anything missing before going further:

```bash
go version      # need Go 1.25 or newer
uv --version    # the Python package manager (brings its own Python 3.11+)
claude --version
ffmpeg -version # OPTIONAL, only for sending playable voice messages
```

If `go` is missing, install it from https://go.dev/dl/ (or `winget install GoLang.Go`, `brew install go`, the distro package manager).

If `uv` is missing:

```bash
# macOS / Linux
curl -LsSf https://astral.sh/uv/install.sh | sh
# Windows (PowerShell)
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

Note the **absolute path** to the `uv` executable — you need it in step 3:

```bash
# macOS / Linux
which uv                              # e.g. /Users/you/.local/bin/uv
# Windows (PowerShell)
(Get-Command uv).Source               # e.g. C:\Users\you\.local\bin\uv.exe
```

## Step 1 — Clone the repository

Pick a stable, permanent location. The MCP registration stores an absolute path to this clone, and
the Python server reads the message database at `../whatsapp-bridge/store/messages.db` relative to
itself — so **moving or renaming the folder later breaks the install**, and the two halves must stay
in the same clone.

Do not clone into a temp directory or inside another project.

```bash
git clone https://github.com/leandrohmvieira/whatsapp-mcp.git
cd whatsapp-mcp
```

Record the absolute path of the clone (`pwd`, or `(Get-Location).Path` on Windows). Call it `REPO`.

## Step 2 — Start the bridge and let the user scan the QR code

The bridge is a Go program that logs into WhatsApp and keeps a local SQLite copy of the messages. On
first run it prints a QR code that **the user must scan with their phone**, so it has to run
somewhere they can actually see it.

Build it first — the binary makes every later restart fast:

```bash
cd whatsapp-bridge
go build -o whatsapp-bridge .        # Windows: go build -o whatsapp-bridge.exe .
```

The first build downloads dependencies and can take a couple of minutes. No C compiler is needed.

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

If you cannot open a separate window, ask the user to run the bridge themselves in their own
terminal. **Do not** run it as a silent background job on the first run — the QR code will never
reach them and the install stalls with no visible reason.

Then tell the user, in plain words:

> Open WhatsApp on your phone → **Settings → Linked Devices → Link a Device**, and scan the QR code
> in the window that just opened.

Wait for them to confirm. A successful login prints `Successfully connected and authenticated!`
followed by `Starting REST API server on :8080...`. History sync then runs for several minutes in
the background — that is normal, and it is fine to continue.

Leave the bridge running. It must be up whenever the MCP tools are used.

## Step 3 — Register the server with Claude Code

Use the absolute `uv` path from step 0 and the absolute `REPO` path from step 1:

```bash
claude mcp add whatsapp --scope user -- /absolute/path/to/uv --directory /absolute/path/to/whatsapp-mcp/whatsapp-mcp-server run main.py
```

```powershell
# Windows (PowerShell) — same command, Windows paths
claude mcp add whatsapp --scope user -- C:\Users\you\.local\bin\uv.exe --directory C:\Users\you\whatsapp-mcp\whatsapp-mcp-server run main.py
```

`--scope user` makes WhatsApp available in every project on this machine, which is almost always
what the user wants. Use `--scope local` to limit it to the current directory.

To enable the optional offline transcription of voice notes at the same time, add
`-e WHATSAPP_MEDIA_TRANSCRIPTION=true` before the `--`. It also needs its dependencies installed —
see "Media Transcription" in [README.md](./README.md). Don't install those unless the user asks;
they're about a gigabyte.

## Step 4 — Verify, then hand off

```bash
claude mcp get whatsapp     # should report ✔ Connected
```

`claude mcp get` health-checks the server, so a ✔ here means `uv` launched the Python server
successfully — it installs the Python dependencies on that first launch, which can take a minute. It
does **not** prove the bridge is reachable; the user's own test in a moment covers that.

A healthy server exposes **12 tools** (13 with transcription enabled). If the count is lower,
something failed to load.

The MCP tools only appear in a Claude Code session that started *after* the server was registered,
so you cannot call them from this session. Tell the user:

> Restart Claude Code, then ask me "list my WhatsApp chats" to confirm it works end to end.

If `claude mcp get` reports a failure, run the server by hand to see the real error — the MCP
transport swallows it otherwise:

```bash
cd "$REPO/whatsapp-mcp-server" && uv run main.py
```

A healthy server prints nothing and waits on stdin — that silence *is* success, so Ctrl-C out of it.
A failure prints a traceback instead.

Finish by telling the user two things they'll otherwise learn the hard way: the bridge has to stay
running for the tools to work, and WhatsApp will ask for a fresh QR scan roughly every 20 days.
