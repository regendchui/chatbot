package survey

import ( // Formatting and WhatsApp send.
	"fmt"     // Errors.
	"strings" // Trim text.

	"whatsapp-bot/common"
	"whatsapp-bot/messaging"

	"go.mau.fi/whatsmeow" // WhatsApp client.
) // End import.

// SendFollowupInvitation sends one follow-up invitation_text + public URL to a participant.
func SendFollowupInvitation(client *whatsmeow.Client, participantPhone string, fu SurveyFollow) error { // Prepared for cron jobs.
	if client == nil { // Guard nil client.
		return fmt.Errorf("client is nil") // Validation error.
	}
	url, err := FollowupSurveyURL(fu.LinkSlug) // Build public survey link.
	if err != nil {                            // Config error.
		return err // Propagate.
	}
	body := strings.TrimSpace(fu.InvitationText) // invitation_text from JSON for this FU.
	if body == "" {                              // Fallback text.
		body = "Please complete your follow-up questionnaire." // Default prompt.
	}
	msg := body + "\n" + url                                                                                // Combined WhatsApp body.
	return messaging.SendMessage(client, participantPhone, msg, common.MessageNatureCronFollowupInvitation) // Send and persist outbound record.
} // End SendFollowupInvitation.
