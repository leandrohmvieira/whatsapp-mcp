"""Offline speech-to-text for WhatsApp voice notes / audio-video (opt-in feature).

Enabled by the `WHATSAPP_MEDIA_TRANSCRIPTION` flag (see main.py). Two backends:

  * faster-whisper  (default) — pure-pip, cross-platform, runs on CPU or an NVIDIA
    GPU (CUDA). Model auto-downloads from Hugging Face on first use. Easiest for
    most users.
  * whisper-cpp     — shells out to a prebuilt `whisper-cli` binary. Use this for
    an AMD GPU via the Vulkan backend (CTranslate2/faster-whisper can't drive AMD),
    or any platform where you've built whisper.cpp yourself.

Everything runs locally — audio never leaves the machine.

Configuration (environment variables)
-------------------------------------
  WHATSAPP_TRANSCRIPTION_BACKEND   faster-whisper | whisper-cpp   (default: faster-whisper)
  WHISPER_MODEL                    faster-whisper: size or path (default: small)
                                   whisper-cpp:    path to a ggml .bin model (required)
  WHISPER_LANGUAGE                 force a language, e.g. pt / en  (default: auto)
  WHISPER_DEVICE                   faster-whisper: cpu | cuda | auto (default: auto)
  WHISPER_COMPUTE_TYPE             faster-whisper: int8 | float16 | ...  (default: int8/float16)
  WHISPER_CPP_BIN                  whisper-cpp: path to whisper-cli(.exe)
  WHISPER_CPP_MODEL                whisper-cpp: path to the ggml model (overrides WHISPER_MODEL)

Public API: transcribe_file(path, language=None) -> dict
  {"ok": True, "text": "...", "language": "pt", "duration": 9.4, "backend": "...", "model": "..."}
  or {"ok": False, "error": "..."}
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import wave
from pathlib import Path

DEFAULT_BACKEND = os.environ.get("WHATSAPP_TRANSCRIPTION_BACKEND", "faster-whisper").strip().lower()
DEFAULT_LANGUAGE = os.environ.get("WHISPER_LANGUAGE", "auto").strip() or "auto"

# Cache the faster-whisper model for the life of the process (the MCP server is
# long-running), so only the first transcription pays the load cost.
_FW_MODEL = None
_FW_MODEL_KEY = None


def _lang_arg(language):
    lang = (language or DEFAULT_LANGUAGE or "auto").strip().lower()
    return None if lang in ("", "auto") else lang


# --------------------------------------------------------------------------- #
# faster-whisper backend
# --------------------------------------------------------------------------- #
def _transcribe_faster_whisper(path, language):
    global _FW_MODEL, _FW_MODEL_KEY
    from faster_whisper import WhisperModel

    model_name = os.environ.get("WHISPER_MODEL", "small").strip() or "small"
    device = os.environ.get("WHISPER_DEVICE", "auto").strip() or "auto"
    compute_type = os.environ.get("WHISPER_COMPUTE_TYPE", "").strip()
    if not compute_type:
        compute_type = "int8" if device == "cpu" else "default"

    key = (model_name, device, compute_type)
    if _FW_MODEL is None or _FW_MODEL_KEY != key:
        _FW_MODEL = WhisperModel(model_name, device=device, compute_type=compute_type)
        _FW_MODEL_KEY = key

    segments, info = _FW_MODEL.transcribe(path, language=_lang_arg(language), vad_filter=True)
    text = "".join(seg.text for seg in segments).strip()
    return {
        "ok": True,
        "text": text,
        "language": info.language,
        "duration": round(float(info.duration), 2),
        "backend": "faster-whisper",
        "model": model_name,
    }


# --------------------------------------------------------------------------- #
# whisper-cpp backend (decode with PyAV, transcribe with whisper-cli)
# --------------------------------------------------------------------------- #
def _decode_to_wav(src, dst):
    """Decode any audio/video to 16 kHz mono s16 WAV via PyAV. Returns duration (s)."""
    import av
    import numpy as np

    container = av.open(src)
    resampler = av.AudioResampler(format="s16", layout="mono", rate=16000)
    chunks = []
    for frame in container.decode(audio=0):
        for rf in resampler.resample(frame):
            chunks.append(rf.to_ndarray())
    for rf in resampler.resample(None):
        chunks.append(rf.to_ndarray())
    container.close()
    data = np.concatenate(chunks, axis=1) if chunks else np.zeros((1, 0), "<i2")
    n = data.shape[-1]
    with wave.open(dst, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(16000)
        w.writeframes(data.astype("<i2").tobytes())
    return round(n / 16000, 2)


def _transcribe_whisper_cpp(path, language):
    bin_path = os.environ.get("WHISPER_CPP_BIN", "").strip()
    model_path = (os.environ.get("WHISPER_CPP_MODEL") or os.environ.get("WHISPER_MODEL") or "").strip()
    if not bin_path or not Path(bin_path).is_file():
        return {"ok": False, "error": f"WHISPER_CPP_BIN not found: {bin_path!r}"}
    if not model_path or not Path(model_path).is_file():
        return {"ok": False, "error": f"whisper-cpp model not found: {model_path!r}"}

    with tempfile.TemporaryDirectory(prefix="wa_stt_") as td:
        wav = os.path.join(td, "audio.wav")
        duration = _decode_to_wav(path, wav)
        lang = _lang_arg(language) or "auto"
        out_base = os.path.join(td, "out")
        cmd = [bin_path, "-m", model_path, "-f", wav, "-l", lang, "-nt", "-oj", "-of", out_base]
        proc = subprocess.run(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE,
                              text=True, encoding="utf-8", errors="replace")
        jf = out_base + ".json"
        if not Path(jf).is_file():
            tail = (proc.stderr or "")[-400:]
            return {"ok": False, "error": f"whisper-cli produced no output; stderr: {tail}"}
        d = json.loads(Path(jf).read_text(encoding="utf-8"))
        text = "".join(s.get("text", "") for s in d.get("transcription", [])).strip()
        return {
            "ok": True,
            "text": text,
            "language": (d.get("result") or {}).get("language"),
            "duration": duration,
            "backend": "whisper-cpp",
            "model": Path(model_path).stem,
        }


# --------------------------------------------------------------------------- #
# Public entry point
# --------------------------------------------------------------------------- #
def transcribe_file(path, language=None, backend=None):
    """Transcribe one audio/video file. Never raises — returns {ok: False, error} on failure."""
    if not path or not Path(path).is_file():
        return {"ok": False, "error": f"file not found: {path!r}"}
    backend = (backend or DEFAULT_BACKEND).strip().lower()
    try:
        if backend in ("whisper-cpp", "whispercpp", "whisper.cpp"):
            return _transcribe_whisper_cpp(path, language)
        return _transcribe_faster_whisper(path, language)
    except ImportError as e:
        return {"ok": False, "error": f"transcription deps missing ({e}); run install-media.py"}
    except Exception as e:
        return {"ok": False, "error": f"{type(e).__name__}: {e}"}


if __name__ == "__main__":
    # Standalone CLI: python transcribe.py file1 [file2 ...]
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    results = [dict(path=p, **transcribe_file(p)) for p in sys.argv[1:]]
    print(json.dumps(results, ensure_ascii=False, indent=2))
