package main

// Media retry: recover media whose CDN copy has expired.
//
// WhatsApp only keeps uploaded media on its CDN for a limited time; after that a
// download answers 403/404/410 even with a perfectly valid direct path. The fix
// is the protocol's "media retry": we send an encrypted server-error receipt to
// our OWN primary device (the phone), which re-uploads the file -- if it still has
// it -- and answers with a MediaRetry notification carrying a fresh direct path.
// Nothing is sent to the chat or to the other participants; this is purely a
// device-to-own-phone protocol exchange.
//
// Flow of POST /api/media_retry {message_id, chat_jid[, sender_jid, timeout_seconds]}:
//   1. read media_key/sender/is_from_me for the stored message
//   2. SendMediaRetryReceipt with a MessageInfo rebuilt from the DB row
//   3. wait for the matching *events.MediaRetry (dispatched from the main event handler)
//   4. DecryptMediaRetryNotification -> new DirectPath, persisted in messages.direct_path
//   5. downloadMedia() as usual, which now picks the fresh direct_path up.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
)

type MediaRetryRequest struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
	// Optional full participant JID override (e.g. "123@lid"). Needed for group
	// rows whose stored sender is the group itself (history-sync rows that lost
	// the participant).
	SenderJID      string `json:"sender_jid,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	// Async: return right after sending the receipt; the answer is handled in the background.
	Async bool `json:"async,omitempty"`
}

type MediaRetryResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Result     string `json:"result,omitempty"`
	SenderUsed string `json:"sender_used,omitempty"`
	DirectPath string `json:"direct_path,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Path       string `json:"path,omitempty"`
	Pending    bool   `json:"pending,omitempty"`
}

var mediaRetryWaiters = struct {
	sync.Mutex
	m map[string][]chan *events.MediaRetry
}{m: make(map[string][]chan *events.MediaRetry)}

// dispatchMediaRetry hands an incoming MediaRetry event to whoever is waiting on it.
func dispatchMediaRetry(evt *events.MediaRetry) {
	mediaRetryWaiters.Lock()
	chans := mediaRetryWaiters.m[evt.MessageID]
	delete(mediaRetryWaiters.m, evt.MessageID)
	mediaRetryWaiters.Unlock()
	if len(chans) == 0 {
		fmt.Printf("Media retry notification for %s (chat %s) with no waiter; completing in background\n", evt.MessageID, evt.ChatID)
		completeMediaRetryInBackground(evt)
		return
	}
	for _, ch := range chans {
		select {
		case ch <- evt:
		default:
		}
	}
}

func registerMediaRetryWaiter(id string) chan *events.MediaRetry {
	ch := make(chan *events.MediaRetry, 1)
	mediaRetryWaiters.Lock()
	mediaRetryWaiters.m[id] = append(mediaRetryWaiters.m[id], ch)
	mediaRetryWaiters.Unlock()
	return ch
}

func unregisterMediaRetryWaiter(id string, ch chan *events.MediaRetry) {
	mediaRetryWaiters.Lock()
	defer mediaRetryWaiters.Unlock()
	chans := mediaRetryWaiters.m[id]
	for i, c := range chans {
		if c == ch {
			chans = append(chans[:i], chans[i+1:]...)
			break
		}
	}
	if len(chans) == 0 {
		delete(mediaRetryWaiters.m, id)
	} else {
		mediaRetryWaiters.m[id] = chans
	}
}

// resolveUserJID turns a bare user part as stored in messages.sender into a JID.
// The LID map decides LID vs phone number; unknown users fall back to a digit
// count heuristic (phone numbers are at most 13 digits here).
func resolveUserJID(client *whatsmeow.Client, user string) types.JID {
	if strings.Contains(user, "@") {
		if jid, err := types.ParseJID(user); err == nil {
			return jid
		}
	}
	ctx := context.Background()
	lid := types.NewJID(user, types.HiddenUserServer)
	if pn, err := client.Store.LIDs.GetPNForLID(ctx, lid); err == nil && !pn.IsEmpty() {
		return lid
	}
	pn := types.NewJID(user, types.DefaultUserServer)
	if l, err := client.Store.LIDs.GetLIDForPN(ctx, pn); err == nil && !l.IsEmpty() {
		return pn
	}
	if len(user) >= 14 {
		return lid
	}
	return pn
}

func requestMediaRetry(client *whatsmeow.Client, messageStore *MessageStore, req MediaRetryRequest) MediaRetryResponse {
	fail := func(format string, a ...interface{}) MediaRetryResponse {
		return MediaRetryResponse{Success: false, Message: fmt.Sprintf(format, a...)}
	}

	var mediaType, filename, sender string
	var mediaKey []byte
	var isFromMe bool
	err := messageStore.db.QueryRow(
		"SELECT COALESCE(media_type, ''), COALESCE(filename, ''), media_key, COALESCE(sender, ''), is_from_me FROM messages WHERE id = ? AND chat_jid = ?",
		req.MessageID, req.ChatJID,
	).Scan(&mediaType, &filename, &mediaKey, &sender, &isFromMe)
	if err != nil {
		return fail("failed to find message: %v", err)
	}
	if mediaType == "" {
		return fail("not a media message")
	}
	if len(mediaKey) == 0 {
		return fail("no media key stored for this message")
	}

	chat, err := types.ParseJID(req.ChatJID)
	if err != nil {
		return fail("invalid chat JID: %v", err)
	}
	isGroup := chat.Server == types.GroupServer

	var senderJID types.JID
	switch {
	case req.SenderJID != "":
		senderJID, err = types.ParseJID(req.SenderJID)
		if err != nil {
			return fail("invalid sender_jid: %v", err)
		}
	case sender != "" && sender != chat.User:
		senderJID = resolveUserJID(client, sender)
	case isFromMe:
		if own := client.Store.GetJID(); !own.IsEmpty() {
			senderJID = own.ToNonAD()
		}
	case isGroup:
		return fail("sender unknown for this group message (stored sender is the group); pass sender_jid")
	default:
		senderJID = chat
	}

	info := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chat,
			Sender:   senderJID,
			IsFromMe: isFromMe,
			IsGroup:  isGroup,
		},
		ID: types.MessageID(req.MessageID),
	}

	// The phone tends to answer in batches, often 1-3 minutes late.
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 300*time.Second {
		timeout = 60 * time.Second
	}

	ch := registerMediaRetryWaiter(req.MessageID)
	defer unregisterMediaRetryWaiter(req.MessageID, ch)

	fmt.Printf("Sending media retry receipt for %s in %s (sender %s, from_me %v)\n", req.MessageID, chat, senderJID, isFromMe)
	if err := client.SendMediaRetryReceipt(context.Background(), info, mediaKey); err != nil {
		return fail("failed to send media retry receipt: %v", err)
	}

	if req.Async {
		return MediaRetryResponse{Success: true, Pending: true, SenderUsed: senderJID.String(),
			Message: "receipt sent; the phone's answer is processed in the background (see store/_mediaretry_results.jsonl)"}
	}

	var evt *events.MediaRetry
	select {
	case evt = <-ch:
	case <-time.After(timeout):
		r := fail("no answer from the phone within %s; it is still processed in the background if it arrives (see store/_mediaretry_results.jsonl)", timeout)
		r.Pending = true
		r.SenderUsed = senderJID.String()
		return r
	}
	resp := completeMediaRetry(client, messageStore, evt, req.ChatJID)
	resp.SenderUsed = senderJID.String()
	return resp
}

// completeMediaRetry turns a phone's MediaRetry answer into a downloaded file.
func completeMediaRetry(client *whatsmeow.Client, messageStore *MessageStore, evt *events.MediaRetry, chatJID string) MediaRetryResponse {
	fail := func(format string, a ...interface{}) MediaRetryResponse {
		return MediaRetryResponse{Success: false, Message: fmt.Sprintf(format, a...)}
	}
	messageID := string(evt.MessageID)
	if chatJID == "" {
		chatJID = evt.ChatID.String()
	}
	var mediaType string
	var mediaKey []byte
	if err := messageStore.db.QueryRow(
		"SELECT COALESCE(media_type, ''), media_key FROM messages WHERE id = ? AND chat_jid = ?",
		messageID, chatJID,
	).Scan(&mediaType, &mediaKey); err != nil {
		return fail("failed to find message: %v", err)
	}

	notif, err := whatsmeow.DecryptMediaRetryNotification(evt, mediaKey)
	if err != nil {
		return fail("media retry failed: %v", err)
	}
	if notif.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS || notif.GetDirectPath() == "" {
		r := fail("phone answered %s", notif.GetResult().String())
		r.Result = notif.GetResult().String()
		return r
	}
	newPath := notif.GetDirectPath()
	if unk := notif.ProtoReflect().GetUnknown(); len(unk) > 0 {
		fmt.Printf("Media retry notification for %s carries %d bytes of unknown fields: %x\n", messageID, len(unk), []byte(unk))
	}
	fmt.Printf("Media retry for %s: phone re-uploaded the media\n", messageID)

	ok, _, fname, path, err := downloadReuploadedMedia(client, messageStore, messageID, chatJID, newPath, notif.GetMessageSecret())
	resp := MediaRetryResponse{
		Result:     notif.GetResult().String(),
		DirectPath: newPath,
		Filename:   fname,
		Path:       path,
	}
	if !ok || err != nil {
		resp.Message = fmt.Sprintf("media re-uploaded but download failed: %v", err)
		return resp
	}
	resp.Success = true
	resp.Message = fmt.Sprintf("Recovered %s media via media retry", mediaType)
	return resp
}

// Answers that arrive after the HTTP request gave up (the phone often answers
// in batches, minutes later) are completed here and appended to
// store/_mediaretry_results.jsonl.
var mediaRetryBackground struct {
	client *whatsmeow.Client
	store  *MessageStore
	mu     sync.Mutex
}

func completeMediaRetryInBackground(evt *events.MediaRetry) {
	c, st := mediaRetryBackground.client, mediaRetryBackground.store
	if c == nil || st == nil {
		return
	}
	go func() {
		resp := completeMediaRetry(c, st, evt, "")
		fmt.Printf("Background media retry for %s: success=%v %s\n", evt.MessageID, resp.Success, resp.Message)
		line, _ := json.Marshal(map[string]interface{}{
			"time": time.Now().Format(time.RFC3339), "message_id": evt.MessageID, "chat_jid": evt.ChatID.String(),
			"success": resp.Success, "message": resp.Message, "result": resp.Result, "path": resp.Path,
		})
		mediaRetryBackground.mu.Lock()
		defer mediaRetryBackground.mu.Unlock()
		f, err := os.OpenFile("store/_mediaretry_results.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.Write(append(line, '\n'))
			f.Close()
		}
	}()
}

func registerMediaRetryHandler(client *whatsmeow.Client, messageStore *MessageStore) {
	mediaRetryBackground.client, mediaRetryBackground.store = client, messageStore
	http.HandleFunc("/api/media_retry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req MediaRetryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request format", http.StatusBadRequest)
			return
		}
		if req.MessageID == "" || req.ChatJID == "" {
			http.Error(w, "Message ID and Chat JID are required", http.StatusBadRequest)
			return
		}
		resp := requestMediaRetry(client, messageStore, req)
		w.Header().Set("Content-Type", "application/json")
		if !resp.Success {
			w.WriteHeader(http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(resp)
	})
}

// downloadReuploadedMedia fetches media the phone re-uploaded after a media retry.
//
// The phone re-encrypts its local copy with the ORIGINAL media key, but its
// local copy is not always byte-identical to what was first sent (observed:
// "hash of media ciphertext doesn't match" on every re-upload), so the stored
// file_enc_sha256 / file_sha256 no longer match. In that case fetch the raw blob
// and decrypt it ourselves: the HMAC (keyed from the media key) still proves the
// blob was produced by the holder of this message's media key. The new hashes
// are written back so /api/download keeps working for the row.
func downloadReuploadedMedia(client *whatsmeow.Client, messageStore *MessageStore, messageID, chatJID, directPath string, messageSecret []byte) (bool, string, string, string, error) {
	var mediaType, filename string
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	err := messageStore.db.QueryRow(
		"SELECT media_type, filename, media_key, file_sha256, file_enc_sha256 FROM messages WHERE id = ? AND chat_jid = ?",
		messageID, chatJID,
	).Scan(&mediaType, &filename, &mediaKey, &fileSHA256, &fileEncSHA256)
	if err != nil {
		return false, "", "", "", fmt.Errorf("failed to find message: %v", err)
	}
	var waMediaType whatsmeow.MediaType
	switch mediaType {
	case "image":
		waMediaType = whatsmeow.MediaImage
	case "video":
		waMediaType = whatsmeow.MediaVideo
	case "audio":
		waMediaType = whatsmeow.MediaAudio
	case "document":
		waMediaType = whatsmeow.MediaDocument
	default:
		return false, "", "", "", fmt.Errorf("unsupported media type: %s", mediaType)
	}

	chatDir := fmt.Sprintf("store/%s", strings.ReplaceAll(chatJID, ":", "_"))
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		return false, "", "", "", fmt.Errorf("failed to create chat directory: %v", err)
	}
	localPath := fmt.Sprintf("%s/%s_%s", chatDir, sanitizeFilenameComponent(messageID), filename)
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return false, "", "", "", fmt.Errorf("failed to get absolute path: %v", err)
	}

	ctx := context.Background()
	data, err := client.DownloadMediaWithPath(ctx, directPath, fileEncSHA256, fileSHA256, mediaKey, waMediaType, "", false)
	if err != nil {
		if !errors.Is(err, whatsmeow.ErrInvalidMediaEncSHA256) && !errors.Is(err, whatsmeow.ErrInvalidMediaSHA256) {
			return false, "", "", "", fmt.Errorf("failed to download re-uploaded media: %v", err)
		}
		fmt.Printf("Re-uploaded media for %s differs from the original hashes (%v); verifying by HMAC instead\n", messageID, err)
		// Raw fetch: no key/hashes => whatsmeow returns the blob untouched.
		raw, rerr := client.DownloadMediaWithPath(ctx, directPath, nil, nil, nil, waMediaType, "", true)
		if rerr != nil {
			return false, "", "", "", fmt.Errorf("failed to fetch re-uploaded media blob: %v", rerr)
		}
		const macLen = 10
		// Debug dump for diagnosing re-upload format issues.
		_ = os.MkdirAll("store/_mediaretry_debug", 0755)
		_ = os.WriteFile(fmt.Sprintf("store/_mediaretry_debug/%s.enc", sanitizeFilenameComponent(messageID)), raw, 0644)
		_ = os.WriteFile(fmt.Sprintf("store/_mediaretry_debug/%s.secret", sanitizeFilenameComponent(messageID)), messageSecret, 0644)
		fmt.Printf("Re-uploaded blob for %s: %d bytes, message secret %d bytes\n", messageID, len(raw), len(messageSecret))
		if len(raw) <= macLen {
			return false, "", "", "", fmt.Errorf("re-uploaded media blob too short (%d bytes)", len(raw))
		}
		expanded := hkdfutil.SHA256(mediaKey, nil, []byte(waMediaType), 112)
		iv, cipherKey, macKey := expanded[:16], expanded[16:48], expanded[48:80]
		ciphertext, mac := raw[:len(raw)-macLen], raw[len(raw)-macLen:]
		h := hmac.New(sha256.New, macKey)
		h.Write(iv)
		h.Write(ciphertext)
		if !hmac.Equal(h.Sum(nil)[:macLen], mac) {
			return false, "", "", "", fmt.Errorf("re-uploaded media failed HMAC check (not encrypted with this message's media key)")
		}
		data, err = cbcutil.Decrypt(cipherKey, iv, ciphertext)
		if err != nil {
			return false, "", "", "", fmt.Errorf("failed to decrypt re-uploaded media: %v", err)
		}
		encSum := sha256.Sum256(raw)
		plainSum := sha256.Sum256(data)
		if _, uerr := messageStore.db.Exec(
			"UPDATE messages SET file_enc_sha256 = ?, file_sha256 = ?, file_length = ? WHERE id = ? AND chat_jid = ?",
			encSum[:], plainSum[:], len(data), messageID, chatJID,
		); uerr != nil {
			fmt.Printf("Warning: could not store new hashes for %s: %v\n", messageID, uerr)
		}
	}

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return false, "", "", "", fmt.Errorf("failed to save media file: %v", err)
	}
	// Persist the fresh path only once it is known to yield this media.
	if _, err := messageStore.db.Exec(
		"UPDATE messages SET direct_path = ? WHERE id = ? AND chat_jid = ?",
		directPath, messageID, chatJID,
	); err != nil {
		fmt.Printf("Warning: could not store new direct path for %s: %v\n", messageID, err)
	}
	fmt.Printf("Saved re-uploaded %s media to %s (%d bytes)\n", mediaType, absPath, len(data))
	return true, mediaType, filename, absPath, nil
}
