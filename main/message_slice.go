package main

import (
	"log"
	"strings"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/messaging"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const (
	defaultMessageSliceEnabled  = false
	defaultMessageSliceDelaySec = 1
	maxChunkSendRetries         = 3
)

func messageSliceEnabled() bool {
	return db.GetProjectSettingBool("MESSAGE_SLICE_ENABLED", defaultMessageSliceEnabled)
}

func messageSliceDelay() time.Duration {
	seconds := db.GetProjectSettingInt("MESSAGE_SLICE_DELAY_SECONDS", defaultMessageSliceDelaySec)
	if seconds < 0 {
		seconds = defaultMessageSliceDelaySec
	}
	return time.Duration(seconds) * time.Second
}

func sendSlicedWhatsAppTextWithReceiver(client *whatsmeow.Client, to types.JID, text string, receiverOverride string, nature string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return messaging.SendWhatsAppTextWithReceiver(client, to, text, receiverOverride, nature)
	}
	if !shouldSliceMessageNature(nature) || !messageSliceEnabled() {
		return messaging.SendWhatsAppTextWithReceiver(client, to, trimmed, receiverOverride, nature)
	}
	chunks := splitMessageForOutbound(trimmed)
	if len(chunks) <= 1 {
		return messaging.SendWhatsAppTextWithReceiver(client, to, trimmed, receiverOverride, nature)
	}
	log.Printf("message slice: nature=%s receiver=%s chunks=%d mode=paragraph", strings.TrimSpace(nature), strings.TrimSpace(receiverOverride), len(chunks))
	delay := messageSliceDelay()
	for i, chunk := range chunks {
		if err := sendChunkWithRetry(client, to, chunk, receiverOverride, nature); err != nil {
			return err
		}
		if i < len(chunks)-1 && delay > 0 {
			time.Sleep(delay)
		}
	}
	return nil
}

func splitMessageForOutbound(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	normalized := strings.ReplaceAll(trimmed, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	paragraphs := strings.Split(normalized, "\n\n")
	var chunks []string
	for _, p := range paragraphs {
		chunk := strings.TrimSpace(p)
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func shouldSliceMessageNature(nature string) bool {
	switch strings.TrimSpace(nature) {
	case common.MessageNatureRegularAIMessage, common.MessageNatureCronAIMessage:
		return true
	default:
		return false
	}
}

func sendChunkWithRetry(client *whatsmeow.Client, to types.JID, chunk string, receiverOverride string, nature string) error {
	var lastErr error
	for attempt := 1; attempt <= maxChunkSendRetries; attempt++ {
		if err := messaging.SendWhatsAppTextWithReceiver(client, to, chunk, receiverOverride, nature); err == nil {
			return nil
		} else {
			lastErr = err
			if attempt < maxChunkSendRetries {
				backoff := time.Duration(attempt) * time.Second
				log.Printf("message slice: send chunk retry=%d/%d receiver=%s err=%v", attempt, maxChunkSendRetries, strings.TrimSpace(receiverOverride), err)
				time.Sleep(backoff)
			}
		}
	}
	return lastErr
}
