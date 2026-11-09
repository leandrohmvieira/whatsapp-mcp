# Media retry (2026-11-06)

Code: `mediaretry.go` (hooks in `main.go`: the `*events.MediaRetry` case in the event handler,
`registerMediaRetryHandler` in `startRESTServer`).

## What it is

WhatsApp's CDN drops media after a while; `/api/download` then answers 403/404/410 even with a
valid direct path. whatsmeow's "media retry" asks the **own primary phone** to re-upload the file:
an encrypted `server-error` receipt goes to our own JID (nothing reaches the chat or any other
participant), and the phone answers with a `mediaretry` notification carrying a fresh direct path.

```
POST /api/media_retry
{"message_id": "...", "chat_jid": "...@g.us|@lid|@s.whatsapp.net",
 "sender_jid": "optional participant override, e.g. 123@lid",
 "timeout_seconds": 60,      // max 300
 "async": false}             // true = return right after sending the receipt
```

## What works

- Receipt is built from the `messages.db` row (sender resolved LID vs phone via
  `whatsmeow_lid_map`; own messages use the stored own JID). Group rows whose stored sender is the
  group JID itself (history-sync rows that lost the participant) need `sender_jid`.
- The phone answers reliably, but **in batches, often 1-3 minutes late**. Answers that arrive
  after the HTTP request gave up are completed in the background and appended to
  `store/_mediaretry_results.jsonl`.
- `media no longer available on phone` (error code 2) = gone for good.
- `/api/download` now also accepts rows with an empty `url` when a `direct_path` exists.
- `WA_LOG_LEVEL=DEBUG` (env, default INFO) logs every XML node sent/received - diagnostics only.

## The open problem: re-uploads use a new media key

Every SUCCESS answer seen so far (22 of 22 on 2026-11-06) points to a blob that is **not encrypted
with the message's stored media key**: right size (padded plaintext + 10-byte MAC) but HMAC fails
and CBC padding is garbage. Proven on an image that still downloaded normally: re-encrypting the
good file with the stored key reproduces the original `file_enc_sha256` exactly; the re-upload
does not. The decrypted notification holds only stanzaID, directPath and result (no unknown
fields, empty messageSecret), and no other node arrives from the phone. Brute-forcing obvious
derivations (HKDF of media key / message secret with media-type, retry and use-case infos) found
nothing.

So, today, media retry **recovers nothing**; the endpoint reports
`re-uploaded media failed HMAC check`. Raw re-uploaded blobs (+ notification dumps) are kept in
`store/_mediaretry_debug/` for offline analysis. `direct_path` is only overwritten after a
successful download.

Untested lead: a `PLACEHOLDER_MESSAGE_RESEND` peer-data-operation to the own phone
(`BuildUnavailableMessageRequest` + `SendPeerMessage`) might return the message as the phone now
stores it, possibly with the new media key/path. Not tried - it is a peer *send*.

## Binaries

- `whatsapp-bridge.exe` - current build with media retry.
- `whatsapp-bridge.exe.bak-20261106` - the previous build (pre media retry, 2026-10-26).
- `whatsapp-bridge.exe.bak` / `.pre405` - older builds, untouched.

Running instance (2026-11-06) was started detached:
`Start-Process .\whatsapp-bridge.exe -WindowStyle Hidden -RedirectStandardOutput bridge.run.log -RedirectStandardError bridge.run.err.log`.

## Side note

`/api/history-sync` calls `BuildHistorySyncRequest(nil, count)`, which dereferences nil (the
request panics and fails) and addresses `status@s.whatsapp.net`. Untouched.
