package main

import (
	"fmt"
	"strings"
	"whatsapp-bot/common"
	"whatsapp-bot/messaging"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func sendWhatsAppTextWithReceiver(client *whatsmeow.Client, to types.JID, text string, receiverOverride string, nature string) error {
	return sendSlicedWhatsAppTextWithReceiver(client, to, text, receiverOverride, nature)
}

func sendWhatsAppText(client *whatsmeow.Client, to types.JID, text string, nature string) error {
	return messaging.SendWhatsAppTextWithReceiver(client, to, text, "", nature)
}

func sendMessage(client *whatsmeow.Client, to string, text string, nature string) error {
	phone := common.ExtractPhone(strings.TrimSpace(to))
	if phone == "" {
		return fmt.Errorf("target phone is empty")
	}
	jidStr := phone + "@s.whatsapp.net"
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return fmt.Errorf("parse JID %q: %w", jidStr, err)
	}
	return sendSlicedWhatsAppTextWithReceiver(client, jid, text, phone, nature)
}

func sendAutoCronMessage(client *whatsmeow.Client, to string, text string, nature string) error {
	return messaging.SendAutoCronMessage(client, to, text, nature)
}
