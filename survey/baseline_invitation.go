package survey

import ( // Strings and errors.
	"fmt"     // Error formatting.
	"strings" // Trim message parts.

	"whatsapp-bot/common"
	"whatsapp-bot/messaging"

	"go.mau.fi/whatsmeow"       // WhatsApp client for sending invitation.
	"go.mau.fi/whatsmeow/types" // Destination JID (phone or LID addressing).
) // End import.

// ComposeBaselineInvitationMessage builds invitation_text + newline + public survey URL.
func ComposeBaselineInvitationMessage(participantPhone string) (string, error) {
	cfg := GlobalSurveyConfig()
	if cfg == nil {
		return "", fmt.Errorf("survey config not loaded")
	}
	url, err := BaselineSurveyURLForParticipant(participantPhone)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(cfg.Baseline.InvitationText) // invitation_text from JSON.
	if body == "" {                                        // Fallback if empty.
		body = "Please complete the baseline questionnaire." // Default English prompt.
	}
	return body + "\n" + url, nil // Full message body for WhatsApp.
} // End ComposeBaselineInvitationMessage.

// SendBaselineInvitation sends baseline invitation + link to the chat JID from the incoming message.
func SendBaselineInvitation(client *whatsmeow.Client, to types.JID, participantPhone string) error {
	msg, err := ComposeBaselineInvitationMessage(participantPhone)
	if err != nil {
		return err
	}
	return messaging.SendWhatsAppTextWithReceiver(client, to, msg, participantPhone, common.MessageNatureBaselineInvitationMessage)
} // End SendBaselineInvitation.
