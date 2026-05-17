package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"whatsapp-bot/admin_panel"

	"go.mau.fi/whatsmeow"
)

type whatsAppAdminState struct {
	mu sync.RWMutex

	connected     bool
	authenticated bool
	deviceID      string
	lastEvent     string
	lastError     string
	latestQRCode  string
	updatedAt     time.Time
}

func (s *whatsAppAdminState) setConnected(v bool) {
	s.mu.Lock()
	s.connected = v
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) setAuthenticated(v bool, deviceID string) {
	s.mu.Lock()
	s.authenticated = v
	s.deviceID = strings.TrimSpace(deviceID)
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) setLastEvent(evt string) {
	s.mu.Lock()
	s.lastEvent = strings.TrimSpace(evt)
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) setLastError(err string) {
	s.mu.Lock()
	s.lastError = strings.TrimSpace(err)
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) setLatestQRCode(code string) {
	s.mu.Lock()
	s.latestQRCode = strings.TrimSpace(code)
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) clearQRCode() {
	s.mu.Lock()
	s.latestQRCode = ""
	s.updatedAt = time.Now().UTC()
	s.mu.Unlock()
}

func (s *whatsAppAdminState) snapshot() admin_panel.WhatsAppStatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return admin_panel.WhatsAppStatusSnapshot{
		Connected:     s.connected,
		Authenticated: s.authenticated,
		DeviceID:      s.deviceID,
		LastEvent:     s.lastEvent,
		LastError:     s.lastError,
		LatestQRCode:  s.latestQRCode,
		UpdatedAt:     s.updatedAt,
	}
}

func startWhatsAppQRWatcher(qrChan <-chan whatsmeow.QRChannelItem, client *whatsmeow.Client, state *whatsAppAdminState) {
	go func() {
		for evt := range qrChan {
			state.setLastEvent(evt.Event)
			if evt.Event == "code" {
				state.setLatestQRCode(evt.Code)
			}
			if evt.Event == "success" {
				deviceID := ""
				if client != nil && client.Store != nil && client.Store.ID != nil {
					deviceID = client.Store.ID.String()
				}
				state.setAuthenticated(true, deviceID)
				state.clearQRCode()
			}
		}
	}()
}

func connectWhatsAppWithQR(client *whatsmeow.Client, state *whatsAppAdminState) error {
	if client == nil {
		return fmt.Errorf("whatsapp client is nil")
	}
	if client.Store != nil && client.Store.ID != nil {
		state.setAuthenticated(true, client.Store.ID.String())
	}
	if client.Store != nil && client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(context.Background())
		if err != nil {
			return fmt.Errorf("get QR channel: %w", err)
		}
		startWhatsAppQRWatcher(qrChan, client, state)
	}
	if err := client.Connect(); err != nil {
		return err
	}
	state.setConnected(true)
	if client.Store != nil && client.Store.ID != nil {
		state.setAuthenticated(true, client.Store.ID.String())
	}
	return nil
}

func refreshWhatsAppQRCode(client *whatsmeow.Client, state *whatsAppAdminState) error {
	if client == nil {
		return fmt.Errorf("whatsapp client is nil")
	}
	if client.Store != nil && client.Store.ID != nil {
		return fmt.Errorf("whatsapp already logged in")
	}
	state.setLastError("")
	state.clearQRCode()
	state.setConnected(false)
	client.Disconnect()
	qrChan, err := client.GetQRChannel(context.Background())
	if err != nil {
		return fmt.Errorf("get fresh QR channel: %w", err)
	}
	startWhatsAppQRWatcher(qrChan, client, state)
	if err := client.Connect(); err != nil {
		return fmt.Errorf("reconnect for QR refresh: %w", err)
	}
	state.setConnected(true)
	return nil
}

func logoutWhatsAppSession(client *whatsmeow.Client, state *whatsAppAdminState) error {
	if client == nil {
		return fmt.Errorf("whatsapp client is nil")
	}
	if client.Store == nil || client.Store.ID == nil {
		return fmt.Errorf("whatsapp is not logged in")
	}

	state.setLastError("")
	if err := client.Logout(context.Background()); err != nil {
		return fmt.Errorf("logout whatsapp session: %w", err)
	}
	client.Disconnect()
	state.setConnected(false)
	state.setAuthenticated(false, "")
	state.clearQRCode()
	state.setLastEvent("logout")

	if err := refreshWhatsAppQRCode(client, state); err != nil {
		return fmt.Errorf("logged out but failed to request new QR: %w", err)
	}
	return nil
}
