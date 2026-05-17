package main // Use main package so handler can call shared app functions directly.

import ( // Import packages required for receive/handle logic.
	"errors"
	"fmt" // Print incoming message details for debugging.
	"log" // Log non-fatal send failures.
	"strconv"
	"strings" // Clean extracted text values.
	"sync"
	"time"

	"whatsapp-bot/AI"
	"whatsapp-bot/common"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"

	"go.mau.fi/whatsmeow" // WhatsApp client type needed for reply send.
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events" // Message event type from whatsmeow.
) // End import block.

var (
	messageHandlingStartMu   sync.RWMutex
	messageHandlingStartUTC  = time.Now().UTC()
	inboundMessageDedupMu    sync.Mutex
	inboundMessageDedupSet   = map[string]struct{}{}
	inboundMessageDedupQueue = make([]string, 0, 2048)
)

const (
	defaultInboundReplayGraceWindow = 30 * time.Second
	maxInboundDedupKeys             = 2000
)

// setMessageHandlingStart defines the oldest inbound timestamp the bot should process.
func setMessageHandlingStart(t time.Time) {
	if t.IsZero() {
		t = time.Now()
	}
	messageHandlingStartMu.Lock()
	messageHandlingStartUTC = t.UTC()
	messageHandlingStartMu.Unlock()
}

func getMessageHandlingStart() time.Time {
	messageHandlingStartMu.RLock()
	defer messageHandlingStartMu.RUnlock()
	return messageHandlingStartUTC
}

func shouldSkipReplayedInboundMessage(msg *events.Message) bool {
	if msg == nil || msg.Info.Timestamp.IsZero() {
		return false
	}
	cutoff := getMessageHandlingStart().Add(-getInboundReplayGraceWindow())
	return msg.Info.Timestamp.UTC().Before(cutoff)
}

func getInboundReplayGraceWindow() time.Duration {
	raw := strings.TrimSpace(db.GetProjectSettingString("INBOUND_REPLAY_GRACE_WINDOW_SECONDS", strconv.Itoa(int(defaultInboundReplayGraceWindow/time.Second))))
	if raw == "" {
		return defaultInboundReplayGraceWindow
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return defaultInboundReplayGraceWindow
	}
	return time.Duration(seconds) * time.Second
}

func rememberInboundMessage(msg *events.Message) bool {
	if msg == nil {
		return false
	}
	msgID := strings.TrimSpace(msg.Info.ID)
	if msgID == "" {
		return false
	}
	key := msgID + "|" + msg.Info.Chat.String() + "|" + msg.Info.Sender.String()
	inboundMessageDedupMu.Lock()
	defer inboundMessageDedupMu.Unlock()
	if _, exists := inboundMessageDedupSet[key]; exists {
		return true
	}
	inboundMessageDedupSet[key] = struct{}{}
	inboundMessageDedupQueue = append(inboundMessageDedupQueue, key)
	if len(inboundMessageDedupQueue) > maxInboundDedupKeys {
		oldest := inboundMessageDedupQueue[0]
		inboundMessageDedupQueue = inboundMessageDedupQueue[1:]
		delete(inboundMessageDedupSet, oldest)
	}
	return false
}

func shouldSendAIErrorFallback() bool {
	raw := strings.ToLower(strings.TrimSpace(db.GetProjectSettingString("SEND_AI_ERROR_FALLBACK", "false")))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func interventionEndMessageFromConfig() string {
	msg := strings.TrimSpace(db.GetProjectSettingString("INTERVENTION_END_MESSAGE", ""))
	if msg != "" {
		return msg
	}
	cfg := survey.GlobalSurveyConfig()
	if cfg != nil {
		msg = strings.TrimSpace(cfg.Project.InterventionEndMessage)
		if msg != "" {
			return msg
		}
	}
	return ""
}

func isVerificationRequired() bool {
	return db.GetProjectSettingBool("REQUIRE_VERIFICATION", false)
}

func verificationMessageFromConfig() string {
	cfg := survey.GlobalSurveyConfig()
	if cfg != nil {
		msg := strings.TrimSpace(cfg.Project.VerificationMessage)
		if msg != "" {
			return msg
		}
	}
	return "Your baseline is complete. Please wait for admin verification before chatting with the AI chatbot."
}

func handleIncomingMessage(client *whatsmeow.Client, msg *events.Message) { // Handle inbound events and save messages.
	if msg != nil && msg.Info.IsFromMe { // Ignore our own outgoing messages to avoid reply loops.
		return // Exit early when message was sent by this bot account.
	}
	if shouldSkipReplayedInboundMessage(msg) {
		log.Printf("Skipping replayed inbound message id=%s timestamp=%s sender=%s", msg.Info.ID, msg.Info.Timestamp.UTC().Format(time.RFC3339), participantPhoneForStorage(msg))
		return
	}
	if rememberInboundMessage(msg) {
		log.Printf("Skipping duplicate inbound message id=%s sender=%s", msg.Info.ID, participantPhoneForStorage(msg))
		return
	}

	text := extractText(msg)
	isVoiceMessage := false
	if strings.TrimSpace(text) == "" {
		voiceText, voiceOK, voiceErr := processIncomingVoiceMessage(client, msg)
		if voiceErr != nil {
			log.Println("Voice message processing error:", voiceErr)
			if voiceOK && errors.Is(voiceErr, ErrVoiceMessageNoSpeech) {
				handleUnintelligibleVoiceMessage(client, msg)
			}
			return
		}
		if !voiceOK {
			return
		}
		text = voiceText
		isVoiceMessage = true
	}

	inboundNature := common.MessageNatureClientMessage
	if isVoiceMessage {
		inboundNature = common.MessageNatureVoiceMessage
	}

	// Prefer phone-number JID when WhatsApp uses LID addressing (matches meta + survey full-phone flow).
	senderPhone := participantPhoneForStorage(msg)
	blacklisted, err := db.IsPhoneBlacklisted(senderPhone)
	if err != nil {
		log.Println("Blacklist status lookup error:", err)
	} else if blacklisted {
		log.Println("Inbound message ignored for blacklisted participant:", senderPhone)
		return
	}

	replyJID := msg.Info.Chat // Conversation to reply in (DM, group, or LID thread).
	if replyJID.IsEmpty() {
		replyJID = msg.Info.Sender
	}

	receiverPhone := "me"                                               // Default logical receiver for inbound records.
	if client != nil && client.Store != nil && client.Store.ID != nil { // Check if bot account JID is available.
		receiverPhone = common.ExtractPhone(client.Store.ID.String()) // Store bot account phone number when available.
	}

	fmt.Println("Received:", text, "from", senderPhone) // Print inbound message details.

	isNewParticipant, err := db.EnsureParticipantMeta(senderPhone) // Ensure participant metadata exists on first contact.
	if err != nil {                                                // Check meta table update errors.
		log.Println("Meta update error:", err) // Log meta update failure without stopping conversation flow.
	} else if isNewParticipant { // Detect first-ever contact from this participant.
		log.Println("New participant detected and inserted into meta table:", senderPhone) // Log successful new client registration.
	}

	baselineDone, err := db.IsParticipantBaselineComplete(senderPhone) // Check whether baseline questionnaire is finished.
	if err != nil {                                                    // Log lookup errors.
		log.Println("Baseline status error:", err) // Non-fatal diagnostic.
		baselineDone = false                       // Fail closed: require baseline before AI.
	}
	if !baselineDone { // Block AI until baseline survey is completed (meta + web form).
		db.SaveMessage(common.Message{
			Sender:    senderPhone,
			Receiver:  receiverPhone,
			Content:   text,
			Direction: "inbound",
			Nature:    inboundNature,
		})
		if err := survey.SendBaselineInvitation(client, replyJID, senderPhone); err != nil { // Send invitation_text + survey link.
			log.Println("Baseline invitation error:", err) // Log send failure.
		}
		return // Do not call Gemini until baseline is complete.
	}

	if isVerificationRequired() {
		verified, err := db.IsParticipantVerified(senderPhone)
		if err != nil {
			log.Println("Verification status error:", err)
			verified = false
		}
		if !verified {
			db.SaveMessage(common.Message{
				Sender:    senderPhone,
				Receiver:  receiverPhone,
				Content:   text,
				Direction: "inbound",
				Nature:    inboundNature,
			})
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
		db.SaveMessage(common.Message{
			Sender:    senderPhone,
			Receiver:  receiverPhone,
			Content:   text,
			Direction: "inbound",
			Nature:    inboundNature,
		})
		endMessageSent, err := db.IsParticipantEndMessageSent(senderPhone)
		if err != nil {
			log.Println("Intervention end-message status error:", err)
			endMessageSent = false
		}
		if !endMessageSent {
			endMessage := interventionEndMessageFromConfig()
			if strings.TrimSpace(endMessage) == "" {
				log.Println("Intervention ended but no end message configured; skipping send.")
			} else if err := sendWhatsAppTextWithReceiver(client, replyJID, endMessage, senderPhone, common.MessageNatureInterventionEndMessage); err != nil {
				log.Println("Intervention end-message send error:", err)
			} else if _, err := db.MarkParticipantEndMessageSentForPhoneDigits(senderPhone); err != nil {
				log.Println("Intervention end-message mark error:", err)
			}
		}
		return
	}

	db.SaveMessage(common.Message{
		Sender:    senderPhone,
		Receiver:  receiverPhone,
		Content:   text,
		Direction: "inbound",
		Nature:    inboundNature,
	})
	enqueueCollectiveResponse(client, replyJID, senderPhone, text)
} // End handleIncomingMessage function.

func generateAndSendAIResponse(client *whatsmeow.Client, replyJID types.JID, senderPhone string, prompt string) {
	memoryMessages, err := ai.GetLastMessagesForParticipant(senderPhone, ai.GetAIMemoryMessageLimitFromEnv()) // Load participant-scoped chat memory for AI.
	if err != nil {                                                                // Check memory loading failure.
		log.Println("Memory load error:", err) // Log error and continue with minimal context.
		memoryMessages = []common.Message{}    // Use empty memory fallback when query fails.
	}

	surveyContext, err := ai.BuildParticipantSurveyContextForAI(senderPhone) // Build completed survey context for this participant.
	if err != nil {
		log.Println("Survey context load error:", err)
		surveyContext = ""
	}
	phaseContext, err := ai.BuildParticipantPhaseContextForAI(senderPhone, time.Now())
	if err != nil {
		log.Println("Phase context load error:", err)
		phaseContext = ""
	}

	reply, err := ai.GenerateAIResponse(prompt, memoryMessages, surveyContext, phaseContext)
	if err != nil {
		log.Println("AI response error:", err)
		if !shouldSendAIErrorFallback() {
			return
		}
		reply = "Sorry, I am having trouble generating a response right now."
	}

	if err := sendWhatsAppTextWithReceiver(client, replyJID, reply, senderPhone, common.MessageNatureRegularAIMessage); err != nil {
		log.Println("Send error:", err)
	}
}

// participantPhoneForStorage prefers the phone JID (SenderAlt) when present so meta/survey match real numbers.
func participantPhoneForStorage(msg *events.Message) string {
	if msg == nil {
		return ""
	}
	ms := msg.Info.MessageSource
	if !ms.SenderAlt.IsEmpty() {
		return common.ExtractPhone(ms.SenderAlt.String())
	}
	return common.ExtractPhone(msg.Info.Sender.String())
}

func extractText(evt *events.Message) string { // Extract text from common WhatsApp message variants.
	if evt == nil || evt.Message == nil { // Guard against nil pointers.
		return "" // Return empty for invalid event payload.
	}

	if v := strings.TrimSpace(evt.Message.GetConversation()); v != "" { // Read plain text conversation field.
		return v // Return plain text directly.
	}
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil { // Read extended text message variant.
		if v := strings.TrimSpace(ext.GetText()); v != "" {
			return v // Return extended text content.
		}
	}
	if img := evt.Message.GetImageMessage(); img != nil { // Read caption from image messages.
		if v := strings.TrimSpace(img.GetCaption()); v != "" {
			return v // Return image caption text.
		}
	}
	if vid := evt.Message.GetVideoMessage(); vid != nil { // Read caption from video messages.
		if v := strings.TrimSpace(vid.GetCaption()); v != "" {
			return v // Return video caption text.
		}
	}

	return "" // Return empty when no text-like content exists.
} // End extractText function.
