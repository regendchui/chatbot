package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"whatsapp-bot/db"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const defaultCollectiveResponseDelay = 3 * time.Second

type collectiveResponseBucket struct {
	senderPhone string
	replyJID    types.JID
	messages    []string
	timer       *time.Timer
}

var collectiveResponseState = struct {
	mu      sync.Mutex
	buckets map[string]*collectiveResponseBucket
}{
	buckets: map[string]*collectiveResponseBucket{},
}

func collectiveResponseEnabled() bool {
	return db.GetProjectSettingBool("COLLECTIVE_RESPONSE", false)
}

func collectiveResponseDelay() time.Duration {
	sec := db.GetProjectSettingInt("DELAY_COLLECTIVE_RESPONSE_SECONDS", int(defaultCollectiveResponseDelay/time.Second))
	if sec < 0 {
		sec = int(defaultCollectiveResponseDelay / time.Second)
	}
	return time.Duration(sec) * time.Second
}

func enqueueCollectiveResponse(client *whatsmeow.Client, replyJID types.JID, senderPhone string, text string) {
	if !collectiveResponseEnabled() {
		generateAndSendAIResponse(client, replyJID, senderPhone, text)
		return
	}

	delay := collectiveResponseDelay()
	if delay <= 0 {
		generateAndSendAIResponse(client, replyJID, senderPhone, text)
		return
	}

	key := strings.TrimSpace(senderPhone)
	if key == "" {
		generateAndSendAIResponse(client, replyJID, senderPhone, text)
		return
	}

	collectiveResponseState.mu.Lock()
	bucket, ok := collectiveResponseState.buckets[key]
	if !ok {
		bucket = &collectiveResponseBucket{
			senderPhone: key,
			replyJID:    replyJID,
			messages:    []string{},
		}
		collectiveResponseState.buckets[key] = bucket
	}
	bucket.replyJID = replyJID
	bucket.messages = append(bucket.messages, strings.TrimSpace(text))
	if bucket.timer != nil {
		bucket.timer.Stop()
	}
	bucket.timer = time.AfterFunc(delay, func() {
		flushCollectiveResponseBucket(client, key)
	})
	collectiveResponseState.mu.Unlock()
}

func flushCollectiveResponseBucket(client *whatsmeow.Client, key string) {
	collectiveResponseState.mu.Lock()
	bucket, ok := collectiveResponseState.buckets[key]
	if !ok {
		collectiveResponseState.mu.Unlock()
		return
	}
	delete(collectiveResponseState.buckets, key)
	messages := append([]string(nil), bucket.messages...)
	replyJID := bucket.replyJID
	senderPhone := bucket.senderPhone
	collectiveResponseState.mu.Unlock()

	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		trimmed := strings.TrimSpace(msg)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return
	}
	joinedPrompt := strings.Join(parts, "\n")
	log.Printf("collective response: sender=%s buffered_messages=%d", senderPhone, len(parts))
	generateAndSendAIResponse(client, replyJID, senderPhone, joinedPrompt)
}
