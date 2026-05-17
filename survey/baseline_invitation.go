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
func ComposeBaselineInvitationMessage() (string, error) { // Text used in WhatsApp outbound.
	cfg := GlobalSurveyConfig() // Require loaded survey config.
	if cfg == nil {             // Missing config.
		return "", fmt.Errorf("survey config not loaded") // Error.
	}
	url, err := BaselineSurveyURL() // Public link to baseline web form.
	if err != nil {                 // Missing SURVEY_PUBLIC_BASE_URL or slug.
		return "", err // Propagate.
	}
	body := strings.TrimSpace(cfg.Baseline.InvitationText) // invitation_text from JSON.
	if body == "" {                                        // Fallback if empty.
		body = "Please complete the baseline questionnaire." // Default English prompt.
	}
	return body + "\n" + url, nil // Full message body for WhatsApp.
} // End ComposeBaselineInvitationMessage.

// SendBaselineInvitation sends baseline invitation + link to the chat JID from the incoming message.
func SendBaselineInvitation(client *whatsmeow.Client, to types.JID, participantPhone string) error {
	msg, err := ComposeBaselineInvitationMessage()
	if err != nil {
		return err
	}
	return messaging.SendWhatsAppTextWithReceiver(client, to, msg, participantPhone, common.MessageNatureBaselineInvitationMessage)
} // End SendBaselineInvitation.
