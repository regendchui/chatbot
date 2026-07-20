package messaging

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

const (
	// WhatsApp drops composing indicators after roughly 25s; refresh before that.
	defaultTypingRefreshInterval = 10 * time.Second
)

// StartTyping shows the "..." typing indicator in the given chat.
func StartTyping(client *whatsmeow.Client, chatJID types.JID) error {
	return sendChatPresence(client, chatJID, types.ChatPresenceComposing, types.ChatPresenceMediaText)
}

// StartRecording shows the voice-note recording indicator in the given chat.
func StartRecording(client *whatsmeow.Client, chatJID types.JID) error {
	return sendChatPresence(client, chatJID, types.ChatPresenceComposing, types.ChatPresenceMediaAudio)
}

// StopTyping clears the typing/recording indicator in the given chat.
func StopTyping(client *whatsmeow.Client, chatJID types.JID) error {
	return sendChatPresence(client, chatJID, types.ChatPresencePaused, types.ChatPresenceMediaText)
}

func sendChatPresence(client *whatsmeow.Client, chatJID types.JID, state types.ChatPresence, media types.ChatPresenceMedia) error {
	if client == nil {
		return fmt.Errorf("client is nil")
	}
	if chatJID.IsEmpty() {
		return fmt.Errorf("chat JID is empty")
	}
	if err := client.SendChatPresence(context.Background(), chatJID, state, media); err != nil {
		return fmt.Errorf("send chat presence (%s): %w", state, err)
	}
	return nil
}

// TypingSession keeps the typing indicator alive until Stop is called.
// WhatsApp expires composing after ~25s, so this refreshes on an interval.
type TypingSession struct {
	client   *whatsmeow.Client
	chatJID  types.JID
	stopOnce sync.Once
	done     chan struct{}
	wg       sync.WaitGroup
}

// StartTypingSession sends composing immediately and refreshes it until Stop().
// Returns nil if client/chat is invalid (caller can ignore typing failures).
func StartTypingSession(client *whatsmeow.Client, chatJID types.JID) *TypingSession {
	return StartTypingSessionWithInterval(client, chatJID, defaultTypingRefreshInterval)
}

// StartTypingSessionWithInterval is like StartTypingSession with a custom refresh interval.
func StartTypingSessionWithInterval(client *whatsmeow.Client, chatJID types.JID, refreshEvery time.Duration) *TypingSession {
	if client == nil || chatJID.IsEmpty() {
		return nil
	}
	if refreshEvery <= 0 {
		refreshEvery = defaultTypingRefreshInterval
	}
	s := &TypingSession{
		client:  client,
		chatJID: chatJID,
		done:    make(chan struct{}),
	}
	if err := StartTyping(client, chatJID); err != nil {
		log.Printf("typing signal: start: %v", err)
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(refreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				if err := StartTyping(client, chatJID); err != nil {
					log.Printf("typing signal: refresh: %v", err)
				}
			}
		}
	}()
	return s
}

// Stop ends the typing indicator and waits for the refresh goroutine to exit.
func (s *TypingSession) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
		if err := StopTyping(s.client, s.chatJID); err != nil {
			log.Printf("typing signal: stop: %v", err)
		}
	})
}

// WithTyping shows the typing indicator while fn runs, then clears it.
// Typing failures are logged and do not fail fn.
func WithTyping(client *whatsmeow.Client, chatJID types.JID, fn func()) {
	session := StartTypingSession(client, chatJID)
	defer session.Stop()
	fn()
}
