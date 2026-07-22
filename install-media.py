#!/usr/bin/env python3
"""Install dependencies for the optional WhatsApp media-transcription feature.

This is the one-shot installer for the opt-in `transcribe_media` tool. It installs the
transcription extra into the MCP server's uv environment and verifies it imports.

Usage
-----
    python install-media.py                        # faster-whisper (default) — CPU or NVIDIA
    python install-media.py --model base           # also pre-download a faster-whisper model
    python install-media.py --backend whisper-cpp  # AMD-GPU (Vulkan) / self-built whisper.cpp

After it finishes, ENABLE the feature by adding this to your MCP client config's env for the
`whatsapp` server, then restart the client:

    "env": { "WHATSAPP_MEDIA_TRANSCRIPTION": "true" }

See the "Media Transcription" section of README.md for backend configuration and usage.
"""
from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
SERVER = ROOT / "whatsapp-mcp-server"
VENV_PY = SERVER / ".venv" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")


def run(cmd, **kw):
    print("+ " + " ".join(str(c) for c in cmd), flush=True)
    return subprocess.run(cmd, **kw)


def main() -> int:
    ap = argparse.ArgumentParser(description="Install the WhatsApp media-transcription deps")
    ap.add_argument("--backend", choices=["faster-whisper", "whisper-cpp"],
                    default="faster-whisper",
                    help="STT engine (default: faster-whisper). whisper-cpp = AMD/Vulkan or self-built.")
    ap.add_argument("--model", default=None,
                    help="faster-whisper only: pre-download this model size (e.g. base, small, medium).")
    args = ap.parse_args()

    uv = shutil.which("uv")
    if not uv:
        print("ERROR: 'uv' not found on PATH. Install it: https://docs.astral.sh/uv/", file=sys.stderr)
        return 1
    if not SERVER.is_dir():
        print(f"ERROR: server dir not found: {SERVER}", file=sys.stderr)
        return 1

    # 1) Install the transcription extra into the server's venv.
    print("\n== Installing transcription dependencies (this can take a few minutes) ==")
    r = run([uv, "sync", "--extra", "transcription"], cwd=str(SERVER))
    if r.returncode != 0:
        print("ERROR: dependency install failed.", file=sys.stderr)
        return r.returncode

    # 2) Verify the imports load.
    print("\n== Verifying ==")
    check = run([str(VENV_PY), "-c",
                 "import faster_whisper, av, numpy; "
                 "print('faster-whisper', faster_whisper.__version__, '| av', av.__version__)"])
    if check.returncode != 0:
        print("ERROR: verification import failed.", file=sys.stderr)
        return 1

    # 3) Backend-specific guidance.
    if args.backend == "faster-whisper":
        if args.model:
            print(f"\n== Pre-downloading faster-whisper model '{args.model}' ==")
            run([str(VENV_PY), "-c",
                 f"from faster_whisper import WhisperModel; WhisperModel('{args.model}'); "
                 f"print('model {args.model} cached')"])
        print("\nBackend: faster-whisper (CPU, or NVIDIA GPU if CUDA is available).")
        print("  Optional env: WHISPER_MODEL=small  WHISPER_DEVICE=auto  WHISPER_LANGUAGE=auto")
    else:
        print("\nBackend: whisper-cpp — you must provide a prebuilt whisper-cli and a ggml model.")
        print("  Set these env vars for the MCP server:")
        print("    WHATSAPP_TRANSCRIPTION_BACKEND=whisper-cpp")
        print("    WHISPER_CPP_BIN=/path/to/whisper-cli(.exe)")
        print("    WHISPER_CPP_MODEL=/path/to/ggml-model.bin")
        print("  Build whisper.cpp with the Vulkan backend for an AMD GPU — see README "
              "'Media Transcription' → whisper-cpp.")

    print("\n== Done ==")
    print("Enable the feature: add  \"env\": { \"WHATSAPP_MEDIA_TRANSCRIPTION\": \"true\" }  to the")
    print("whatsapp server in your MCP client config, then restart the client.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
