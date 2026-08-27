package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

const whatsAppVersionRefreshTimeout = 15 * time.Second

var clientOutdatedRecovery = struct {
	sync.Mutex
	lastAttempt time.Time
}{}

// refreshWhatsAppWebVersion updates whatsmeow's advertised WhatsApp Web
// version. The library's embedded version remains in use if this request
// fails, so a temporary web.whatsapp.com outage does not prevent startup.
func refreshWhatsAppWebVersion(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, whatsAppVersionRefreshTimeout)
	defer cancel()

	latest, err := whatsmeow.GetLatestVersion(ctx, &http.Client{Timeout: whatsAppVersionRefreshTimeout})
	if err != nil {
		return fmt.Errorf("fetch current WhatsApp Web version: %w", err)
	}
	store.SetWAVersion(*latest)
	log.Printf("WhatsApp Web client version set to %s", latest.String())
	return nil
}

// recoverWhatsAppClientOutdated performs one controlled reconnect after a 405
// client-outdated event. The cooldown prevents a reconnect loop if WhatsApp
// rejects even the freshly discovered version.
func recoverWhatsAppClientOutdated(client *whatsmeow.Client, state *whatsAppAdminState) {
	clientOutdatedRecovery.Lock()
	if time.Since(clientOutdatedRecovery.lastAttempt) < time.Minute {
		clientOutdatedRecovery.Unlock()
		return
	}
	clientOutdatedRecovery.lastAttempt = time.Now()
	defer clientOutdatedRecovery.Unlock()

	state.setConnected(false)
	state.setLastEvent("client_outdated_recovery")
	if err := refreshWhatsAppWebVersion(context.Background()); err != nil {
		state.setLastError(err.Error())
		log.Printf("WhatsApp client version refresh failed: %v", err)
		return
	}

	client.Disconnect()
	if err := connectWhatsAppWithQR(client, state); err != nil {
		state.setLastError("reconnect after client version refresh: " + err.Error())
		log.Printf("WhatsApp reconnect after client version refresh failed: %v", err)
		return
	}
	state.setLastError("")
	state.setLastEvent("client_version_refreshed")
}
