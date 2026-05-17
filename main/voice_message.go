package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	defaultVoiceTranscriptionURL        = "https://openrouter.ai/api/v1/audio/transcriptions"
	defaultVoiceMessageModel            = "openai/whisper-1"
	defaultUnintelligibleVoiceNoteReply = "I couldn't hear anything in that voice note."
)

// ErrVoiceMessageNoSpeech is returned when a voice note has no usable transcription.
var ErrVoiceMessageNoSpeech = errors.New("voice message has no meaningful speech")

type openRouterSTTRequest struct {
	Model      string              `json:"model"`
	InputAudio openRouterSTTAudio  `json:"input_audio"`
}

type openRouterSTTAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type openRouterSTTResponse struct {
	Text string `json:"text"`
}

// Phrases Whisper often returns for silent or near-empty audio (normalized ASCII, lowercase).
var voiceTranscriptionHallucinationPhrases = []string{
	"thank you so much for watching",
	"thank you for watching",
	"thanks for watching",
	"thanks for listening",
	"please subscribe",
	"subscribe to my channel",
	"subtitle by",
	"you",
}

func isVoiceMessageEnabled() bool {
	return db.GetProjectSettingBool("VOICE_MESSAGE_ENABLED", false)
}

func voiceMessageModel() string {
	model := strings.TrimSpace(db.GetProjectSettingString("VOICE_MESSAGE_MODEL", defaultVoiceMessageModel))
	if model == "" {
		return defaultVoiceMessageModel
	}
	return model
}

func unintelligibleVoiceNoteReply() string {
	reply := strings.TrimSpace(db.GetProjectSettingString("VOICE_MESSAGE_UNINTELLIGIBLE_REPLY", defaultUnintelligibleVoiceNoteReply))
	if reply == "" {
		return defaultUnintelligibleVoiceNoteReply
	}
	return reply
}

func isIncomingVoiceMessage(msg *events.Message) bool {
	if msg == nil || msg.Message == nil {
		return false
	}
	return msg.Message.GetAudioMessage() != nil
}

// processIncomingVoiceMessage downloads and transcribes a WhatsApp voice note when enabled.
// Returns transcribed text, whether the message was a voice note, and any error.
func processIncomingVoiceMessage(client *whatsmeow.Client, msg *events.Message) (string, bool, error) {
	if !isVoiceMessageEnabled() {
		return "", false, nil
	}
	if !isIncomingVoiceMessage(msg) {
		return "", false, nil
	}
	if client == nil {
		return "", true, fmt.Errorf("whatsapp client is nil")
	}

	audio := msg.Message.GetAudioMessage()
	data, err := client.Download(context.Background(), audio)
	if err != nil {
		return "", true, fmt.Errorf("download voice message: %w", err)
	}
	if len(data) == 0 {
		return "", true, fmt.Errorf("downloaded voice message is empty")
	}

	format := audioFormatForTranscription(audio.GetMimetype())
	text, err := transcribeVoiceMessageAudio(data, format)
	if err != nil {
		return "", true, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", true, ErrVoiceMessageNoSpeech
	}
	if isLikelyVoiceTranscriptionHallucination(text) {
		log.Printf("Voice message ignored: silence hallucination (%d bytes, format=%s): %q", len(data), format, text)
		return "", true, ErrVoiceMessageNoSpeech
	}
	log.Printf("Voice message transcribed (%d bytes, format=%s): %q", len(data), format, text)
	return text, true, nil
}

func normalizeForVoiceHallucinationCheck(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isLikelyVoiceTranscriptionHallucination(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	normalized := normalizeForVoiceHallucinationCheck(trimmed)
	for _, phrase := range voiceTranscriptionHallucinationPhrases {
		if normalized == phrase {
			return true
		}
		if strings.HasPrefix(normalized, phrase) && len(normalized) <= len(phrase)+12 {
			return true
		}
	}
	// Common non-English silence hallucinations.
	for _, fragment := range []string{"请不吝点赞", "視聴ありがとう", "ご視聴ありがとう", "字幕"} {
		if strings.Contains(trimmed, fragment) {
			return true
		}
	}
	return false
}

// handleUnintelligibleVoiceMessage replies when a voice note has no usable speech (silent / hallucinated).
func handleUnintelligibleVoiceMessage(client *whatsmeow.Client, msg *events.Message) {
	if client == nil || msg == nil {
		return
	}
	senderPhone := participantPhoneForStorage(msg)
	if senderPhone == "" {
		return
	}
	blacklisted, err := db.IsPhoneBlacklisted(senderPhone)
	if err != nil {
		log.Println("Blacklist status lookup error:", err)
	} else if blacklisted {
		return
	}

	replyJID := msg.Info.Chat
	if replyJID.IsEmpty() {
		replyJID = msg.Info.Sender
	}
	if replyJID.IsEmpty() {
		return
	}

	if _, err := db.EnsureParticipantMeta(senderPhone); err != nil {
		log.Println("Meta update error:", err)
	}

	baselineDone, err := db.IsParticipantBaselineComplete(senderPhone)
	if err != nil {
		log.Println("Baseline status error:", err)
		baselineDone = false
	}
	if !baselineDone {
		if err := survey.SendBaselineInvitation(client, replyJID, senderPhone); err != nil {
			log.Println("Baseline invitation error:", err)
		}
		return
	}

	if isVerificationRequired() {
		verified, err := db.IsParticipantVerified(senderPhone)
		if err != nil {
			log.Println("Verification status error:", err)
			verified = false
		}
		if !verified {
			waitingMessage := verificationMessageFromConfig()
			if strings.TrimSpace(waitingMessage) != "" {
				if err := sendWhatsAppTextWithReceiver(client, replyJID, waitingMessage, senderPhone, common.MessageNatureVerificationMessage); err != nil {
					log.Println("Verification waiting-message send error:", err)
				}
			}
			return
		}
	}

	interventionEnded, err := isParticipantInterventionEnded(senderPhone, time.Now())
	if err != nil {
		log.Println("Intervention end status error:", err)
		interventionEnded = false
	}
	if interventionEnded {
		return
	}

	reply := unintelligibleVoiceNoteReply()
	if err := sendWhatsAppTextWithReceiver(client, replyJID, reply, senderPhone, common.MessageNatureRegularAIMessage); err != nil {
		log.Println("Unintelligible voice note reply send error:", err)
	}
}

func audioFormatForTranscription(mimetype string) string {
	lower := strings.ToLower(strings.TrimSpace(mimetype))
	switch {
	case strings.Contains(lower, "ogg"):
		return "ogg"
	case strings.Contains(lower, "mpeg"), strings.Contains(lower, "mp3"):
		return "mp3"
	case strings.Contains(lower, "mp4"), strings.Contains(lower, "m4a"):
		return "m4a"
	case strings.Contains(lower, "wav"):
		return "wav"
	case strings.Contains(lower, "webm"):
		return "webm"
	case strings.Contains(lower, "flac"):
		return "flac"
	case strings.Contains(lower, "aac"):
		return "aac"
	default:
		return "ogg"
	}
}

func transcribeVoiceMessageAudio(audio []byte, format string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY is required for voice transcription")
	}

	model := voiceMessageModel()
	payload := openRouterSTTRequest{
		Model: model,
		InputAudio: openRouterSTTAudio{
			Data:   base64.StdEncoding.EncodeToString(audio),
			Format: format,
		},
	}
	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal transcription request: %w", err)
	}

	url := voiceTranscriptionURL()
	httpClient := &http.Client{Timeout: 120 * time.Second}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(requestJSON))
	if err != nil {
		return "", fmt.Errorf("create transcription request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	if referer := strings.TrimSpace(os.Getenv("OPENROUTER_SITE_URL")); referer != "" {
		request.Header.Set("HTTP-Referer", referer)
	}
	if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_NAME")); title != "" {
		request.Header.Set("X-Title", title)
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("call OpenRouter transcription API: %w", err)
	}
	defer response.Body.Close()

	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read transcription response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("OpenRouter transcription API error status %d: %s", response.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result openRouterSTTResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", fmt.Errorf("unmarshal transcription response: %w", err)
	}
	return strings.TrimSpace(result.Text), nil
}

func voiceTranscriptionURL() string {
	if custom := strings.TrimSpace(db.GetProjectSettingString("VOICE_MESSAGE_TRANSCRIPTION_URL", "")); custom != "" {
		return custom
	}
	if custom := strings.TrimSpace(os.Getenv("VOICE_MESSAGE_TRANSCRIPTION_URL")); custom != "" {
		return custom
	}
	openRouterURL := strings.TrimSpace(os.Getenv("OPENROUTER_URL"))
	if openRouterURL != "" {
		if strings.Contains(openRouterURL, "/chat/completions") {
			return strings.Replace(openRouterURL, "/chat/completions", "/audio/transcriptions", 1)
		}
		if strings.HasSuffix(strings.TrimRight(openRouterURL, "/"), "/api/v1") {
			return strings.TrimRight(openRouterURL, "/") + "/audio/transcriptions"
		}
	}
	return defaultVoiceTranscriptionURL
}
