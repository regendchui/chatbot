package main

import (
	"fmt"
	"strings"
	"time"

	"whatsapp-bot/AI"
	"whatsapp-bot/common"
	"whatsapp-bot/db"

	"go.mau.fi/whatsmeow"
)

const baselineInitiationPrompt = "The participant has just completed baseline. Send one warm, concise opening WhatsApp message to initiate supportive conversation."

func sendInitialAIMessageToParticipant(client *whatsmeow.Client, participantPhone string) error {
	phone := common.DigitsOnly(strings.TrimSpace(participantPhone))
	if phone == "" {
		return fmt.Errorf("participant phone is empty")
	}

	memoryMessages, err := ai.GetLastMessagesForParticipant(phone, ai.GetAIMemoryMessageLimitFromEnv())
	if err != nil {
		return fmt.Errorf("load chat memory: %w", err)
	}
	surveyContext, err := ai.BuildParticipantSurveyContextForAI(phone)
	if err != nil {
		return fmt.Errorf("load survey context: %w", err)
	}
	phaseContext, err := ai.BuildParticipantPhaseContextForAI(phone, time.Now())
	if err != nil {
		return fmt.Errorf("load phase context: %w", err)
	}
	reply, err := ai.GenerateAIResponse(baselineInitiationPrompt, memoryMessages, surveyContext, phaseContext, ai.LatestInboundMediumNone)
	if err != nil {
		return fmt.Errorf("generate initial AI message: %w", err)
	}
	return sendMessage(client, phone, reply, common.MessageNatureRegularAIMessage)
}

func sendPostBaselineMessage(client *whatsmeow.Client, participantPhone string) error {
	if db.GetProjectSettingBool("REQUIRE_VERIFICATION", false) {
		return sendMessage(client, participantPhone, verificationMessageFromConfig(), common.MessageNatureVerificationMessage)
	}
	return sendInitialAIMessageToParticipant(client, participantPhone)
}

