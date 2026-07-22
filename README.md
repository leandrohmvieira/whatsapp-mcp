# WhatsApp MCP Server

This is a Model Context Protocol (MCP) server for WhatsApp.

With this you can search and read your personal Whatsapp messages (including images, videos, documents, and audio messages), search your contacts and send messages to either individuals or groups. You can also send media files including images, videos, documents, and audio messages.

It connects to your **personal WhatsApp account** directly via the Whatsapp web multidevice API (using the [whatsmeow](https://github.com/tulir/whatsmeow) library). All your messages are stored locally in a SQLite database and only sent to an LLM (such as Claude) when the agent accesses them through tools (which you control).

Here's an example of what you can do when it's connected to Claude.

![WhatsApp MCP](./example-use.png)

> To get updates on this and other projects I work on [enter your email here](https://docs.google.com/forms/d/1rTF9wMBTN0vPfzWuQa2BjfGKdKIpTbyeKxhPMcEzgyI/preview)

> *Caution:* as with many MCP servers, the WhatsApp MCP is subject to [the lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/). This means that project injection could lead to private data exfiltration.

## Installation

### Prerequisites

- Go
- Python 3.6+
- Anthropic Claude Desktop app (or Cursor)
- UV (Python package manager), install with `curl -LsSf https://astral.sh/uv/install.sh | sh`
- FFmpeg (_optional_) - Only needed for audio messages. If you want to send audio files as playable WhatsApp voice messages, they must be in `.ogg` Opus format. With FFmpeg installed, the MCP server will automatically convert non-Opus audio files. Without FFmpeg, you can still send raw audio files using the `send_file` tool.

### Steps

1. **Clone this repository**

   ```bash
   git clone https://github.com/lharries/whatsapp-mcp.git
   cd whatsapp-mcp
   ```

2. **Run the WhatsApp bridge**

   Navigate to the whatsapp-bridge directory and run the Go application:

   ```bash
   cd whatsapp-bridge
   go run main.go
   ```

   The first time you run it, you will be prompted to scan a QR code. Scan the QR code with your WhatsApp mobile app to authenticate.

   After approximately 20 days, you will might need to re-authenticate.

3. **Connect to the MCP server**

   Copy the below json with the appropriate {{PATH}} values:

   ```json
   {
     "mcpServers": {
       "whatsapp": {
         "command": "{{PATH_TO_UV}}", // Run `which uv` and place the output here
         "args": [
           "--directory",
           "{{PATH_TO_SRC}}/whatsapp-mcp/whatsapp-mcp-server", // cd into the repo, run `pwd` and enter the output here + "/whatsapp-mcp-server"
           "run",
           "main.py"
         ]
       }
     }
   }
   ```

   For **Claude**, save this as `claude_desktop_config.json` in your Claude Desktop configuration directory at:

   ```
   ~/Library/Application Support/Claude/claude_desktop_config.json
   ```

   For **Cursor**, save this as `mcp.json` in your Cursor configuration directory at:

   ```
   ~/.cursor/mcp.json
   ```

4. **Restart Claude Desktop / Cursor**

   Open Claude Desktop and you should now see WhatsApp as an available integration.

   Or restart Cursor.

### Windows Compatibility

The bridge uses a **pure-Go SQLite driver** ([`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite)), so **no C compiler and no CGO** are required. It builds and runs out of the box on Windows (and cross-compiles to other platforms) with the default `CGO_ENABLED=0`:

```bash
cd whatsapp-bridge
go run main.go
```

> **Note:** Earlier versions used the cgo-based `go-sqlite3`, which required installing a C compiler (MSYS2) and setting `CGO_ENABLED=1` on Windows. That is no longer needed. If you previously ran `go env -w CGO_ENABLED=1`, you can leave it — the build works either way.

## Architecture Overview

This application consists of two main components:

1. **Go WhatsApp Bridge** (`whatsapp-bridge/`): A Go application that connects to WhatsApp's web API, handles authentication via QR code, and stores message history in SQLite. It serves as the bridge between WhatsApp and the MCP server.

2. **Python MCP Server** (`whatsapp-mcp-server/`): A Python server implementing the Model Context Protocol (MCP), which provides standardized tools for Claude to interact with WhatsApp data and send/receive messages.

### Data Storage

- All message history is stored in a SQLite database within the `whatsapp-bridge/store/` directory
- The database maintains tables for chats and messages
- Messages are indexed for efficient searching and retrieval

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

By default, just the metadata of the media is stored in the local database. The message will indicate that media was sent. To access this media you need to use the download_media tool which takes the `message_id` and `chat_jid` (which are shown when printing messages containing the meda), this downloads the media and then returns the file path which can be then opened or passed to another tool.

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

(Equivalent manual step: `cd whatsapp-mcp-server && uv sync --extra transcription`.)

### 2. Enable the feature

Add the flag to the `whatsapp` server's environment in your MCP client config
(`claude_desktop_config.json` / Cursor `mcp.json`), then restart the client:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "{{PATH_TO_UV}}",
      "args": ["--directory", "{{PATH_TO_SRC}}/whatsapp-mcp/whatsapp-mcp-server", "run", "main.py"],
      "env": { "WHATSAPP_MEDIA_TRANSCRIPTION": "true" }
    }
  }
}
```

When the flag is off (or unset), `transcribe_media` is simply not registered.

### 3. Use it

Call `transcribe_media(message_id, chat_jid)` (both shown in the placeholder when you print messages).
It downloads the media if needed and returns `{ text, language, duration, backend, model }`.

### Backends

| Backend | Best for | Setup |
|---|---|---|
| **faster-whisper** (default) | CPU, or **NVIDIA** GPU (CUDA) | just `install-media.py` |
| **whisper-cpp** | **AMD** GPU (Vulkan) or any self-built whisper.cpp | build whisper.cpp + point env vars at it |

Configuration via environment variables on the MCP server:

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
3. The Go accesses the WhatsApp API and keeps the SQLite database up to date
4. Data flows back through the chain to Claude
5. When sending messages, the request flows from Claude through the MCP server to the Go bridge and to WhatsApp

## Troubleshooting

- If you encounter permission issues when running uv, you may need to add it to your PATH or use the full path to the executable.
- Make sure both the Go application and the Python server are running for the integration to work properly.

### Authentication Issues

- **QR Code Not Displaying**: If the QR code doesn't appear, try restarting the authentication script. If issues persist, check if your terminal supports displaying QR codes.
- **WhatsApp Already Logged In**: If your session is already active, the Go bridge will automatically reconnect without showing a QR code.
- **Device Limit Reached**: WhatsApp limits the number of linked devices. If you reach this limit, you'll need to remove an existing device from WhatsApp on your phone (Settings > Linked Devices).
- **No Messages Loading**: After initial authentication, it can take several minutes for your message history to load, especially if you have many chats.
- **WhatsApp Out of Sync**: If your WhatsApp messages get out of sync with the bridge, delete both database files (`whatsapp-bridge/store/messages.db` and `whatsapp-bridge/store/whatsapp.db`) and restart the bridge to re-authenticate.

For additional Claude Desktop integration troubleshooting, see the [MCP documentation](https://modelcontextprotocol.io/quickstart/server#claude-for-desktop-integration-issues). The documentation includes helpful tips for checking logs and resolving common issues.
