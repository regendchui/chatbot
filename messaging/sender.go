package messaging

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"whatsapp-bot/common"
	"whatsapp-bot/db"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

const (
	cronSendQueueBufferSize     = 256
	defaultCronSendMinDelaySecs = 30
	defaultCronSendMaxDelaySecs = 45
)

type cronSendJob struct {
	client *whatsmeow.Client
	to     string
	text   string
	nature string
	done   chan error
}

var (
	cronSendWorkerOnce sync.Once
	cronSendQueue      chan cronSendJob
)

func ensureCronSendWorkerStarted() {
	cronSendWorkerOnce.Do(func() {
		cronSendQueue = make(chan cronSendJob, cronSendQueueBufferSize)
		go runCronSendWorker()
	})
}

func runCronSendWorker() {
	for job := range cronSendQueue {
		err := SendMessage(job.client, job.to, job.text, job.nature)
		minDelaySecs, maxDelaySecs := getCronSendDelayWindowSeconds()
		sleepSeconds := minDelaySecs
		if maxDelaySecs > minDelaySecs {
			sleepSeconds += rand.Intn(maxDelaySecs - minDelaySecs + 1)
		}
		time.Sleep(time.Duration(sleepSeconds) * time.Second)
		job.done <- err
		close(job.done)
	}
}

func getCronSendDelayWindowSeconds() (int, int) {
	minDelaySecs := db.GetProjectSettingInt("CRON_SEND_MIN_DELAY_SECONDS", defaultCronSendMinDelaySecs)
	maxDelaySecs := db.GetProjectSettingInt("CRON_SEND_MAX_DELAY_SECONDS", defaultCronSendMaxDelaySecs)
	if minDelaySecs < 0 {
		minDelaySecs = defaultCronSendMinDelaySecs
	}
	if maxDelaySecs < 0 {
		maxDelaySecs = defaultCronSendMaxDelaySecs
	}
	if maxDelaySecs < minDelaySecs {
		maxDelaySecs = minDelaySecs
	}
	return minDelaySecs, maxDelaySecs
}

func SendWhatsAppTextWithReceiver(client *whatsmeow.Client, to types.JID, text string, receiverOverride string, nature string) error {
	if client == nil {
		return fmt.Errorf("client is nil")
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("message text is empty")
	}
	if to.IsEmpty() {
		return fmt.Errorf("destination JID is empty")
	}

	msg := &waProto.Message{Conversation: proto.String(trimmed)}
	if _, err := client.SendMessage(context.Background(), to, msg); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	senderPhone := "me"
	if client.Store != nil && client.Store.ID != nil {
		senderPhone = common.ExtractPhone(client.Store.ID.String())
	}
	receiver := common.ExtractPhone(to.String())
	overrideReceiver := common.DigitsOnly(strings.TrimSpace(receiverOverride))
	if overrideReceiver != "" {
		receiver = overrideReceiver
	}

	db.SaveMessage(common.Message{
		Sender:    senderPhone,
		Receiver:  receiver,
		Content:   trimmed,
		Direction: "outbound",
		Nature:    strings.TrimSpace(nature),
	})
	return nil
}

func SendWhatsAppText(client *whatsmeow.Client, to types.JID, text string) error {
	return SendWhatsAppTextWithReceiver(client, to, text, "", common.MessageNatureManualMessage)
}

func SendMessage(client *whatsmeow.Client, to string, text string, nature string) error {
	phone := common.ExtractPhone(strings.TrimSpace(to))
	if phone == "" {
		return fmt.Errorf("target phone is empty")
	}

	jidStr := phone + "@s.whatsapp.net"
	jid, err := types.ParseJID(jidStr)
	if err != nil {
		return fmt.Errorf("parse JID %q: %w", jidStr, err)
	}
	return SendWhatsAppTextWithReceiver(client, jid, text, phone, nature)
}

func SendAutoCronMessage(client *whatsmeow.Client, to string, text string, nature string) error {
	ensureCronSendWorkerStarted()
	done := make(chan error, 1)
	cronSendQueue <- cronSendJob{
		client: client,
		to:     to,
		text:   text,
		nature: strings.TrimSpace(nature),
		done:   done,
	}
	return <-done
}
