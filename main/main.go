package main // Use the main package to build the executable entrypoint.

import ( // Import all packages required by runtime bot logic.
	"context" // Use context for WhatsApp connect/send operations.
	"fmt"     // Print QR code and console information.
	"log"     // Log non-fatal runtime errors.
	"os"      // Read optional runtime environment variables.
	"strings" // Clean and validate text values.
	"time"

	"whatsapp-bot/admin_panel"
	"whatsapp-bot/common"
	"whatsapp-bot/cron_task"
	"whatsapp-bot/db"
	"whatsapp-bot/survey"

	_ "github.com/lib/pq"                // Register "postgres" driver for whatsmeow sqlstore usage.
	"go.mau.fi/whatsmeow"                // WhatsApp Web client implementation.
	"go.mau.fi/whatsmeow/store/sqlstore" // SQL-backed login/session storage for whatsmeow.
	"go.mau.fi/whatsmeow/types/events"   // Event types emitted by whatsmeow client.
	waLog "go.mau.fi/whatsmeow/util/log" // Logger adapter expected by whatsmeow internals.
) // End import block.

func envBoolEnabled(name string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}

func runEmergencyAdminCommand(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	if cmd != "force-reset-admin-password" {
		return false, nil
	}
	if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
		return true, fmt.Errorf("usage: force-reset-admin-password <new_password> [new_username]")
	}
	newPassword := strings.TrimSpace(args[1])
	if err := db.UpdateAdminPassword(newPassword); err != nil {
		return true, fmt.Errorf("force reset admin password: %w", err)
	}
	if len(args) >= 3 && strings.TrimSpace(args[2]) != "" {
		if err := db.UpdateAdminUsername(strings.TrimSpace(args[2])); err != nil {
			return true, fmt.Errorf("force reset admin username: %w", err)
		}
	}
	log.Println("Emergency admin credential reset completed.")
	return true, nil
}

func main() { // Start application setup and keep bot alive.
	db.InitDB()         // Initialize PostgreSQL pool and create messages table.
	defer db.DB.Close() // Ensure DB connections are closed on process exit.
	if err := db.EnsureProjectSettingsInitialized(); err != nil {
		panic(fmt.Errorf("project settings bootstrap: %w", err))
	}
	if handled, err := runEmergencyAdminCommand(os.Args[1:]); handled {
		if err != nil {
			panic(err)
		}
		return
	}
	db.EnsureAutoMessageInfrastructure()
	if err := survey.InitSurveyInfrastructure(); err != nil {
		panic(fmt.Errorf("survey infrastructure: %w", err))
	}
	survey.SetSchedulingHooks(survey.SchedulingHooks{
		ScheduleAutoAIMessages:       cron_task.ScheduleAutoAIMessagesForParticipant,
		ScheduleAutoFollowupMessages: cron_task.ScheduleAutoFollowupMessagesForParticipant,
		DeletePendingFollowup:        db.DeletePendingAutoFollowupSchedules,
		AfterBaselineCompleted:       nil,
	})

	survey.StartSurveyHTTPServer()          // Serve /survey/{slug} forms for baseline and follow-ups (separate public URL in production).
	admin_panel.StartAdminPanelHTTPServer() // Serve admin CMS routes on dedicated HTTP listener.

	// Build a DSN dedicated to whatsmeow session/device storage.
	storeDSN := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		db.MustGetEnv("DB_USER", "postgres"),
		db.MustGetEnv("DB_PASSWORD", "postgres"),
		db.MustGetEnv("DB_HOST", "postgres"),
		db.MustGetEnv("DB_PORT", "5432"),
		db.MustGetEnv("DB_NAME", "wa_db"),
	)

	// Create logger for whatsmeow SQL store internals.
	dbLogger := waLog.Stdout("WA-DB", "INFO", true)

	// Create SQL container that stores device keys/sessions in PostgreSQL.
	container, err := sqlstore.New(context.Background(), "postgres", storeDSN, dbLogger)
	if err != nil { // Check for container initialization errors.
		panic(fmt.Errorf("init sqlstore: %w", err)) // Stop app if session store cannot initialize.
	}

	// Load first saved device session (or prepare empty device when first run).
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil { // Check device retrieval errors.
		panic(fmt.Errorf("get first device: %w", err)) // Stop app if device store cannot be read.
	}

	// Create logger for WhatsApp network/client activity.
	clientLogger := waLog.Stdout("WA-CLIENT", "INFO", true)

	// Build the WhatsApp client using stored device credentials.
	client := whatsmeow.NewClient(deviceStore, clientLogger)
	waState := &whatsAppAdminState{}
	admin_panel.SetWhatsAppStatusProvider(waState.snapshot)
	admin_panel.SetWhatsAppQRRefreshHandler(func() error {
		err := refreshWhatsAppQRCode(client, waState)
		if err != nil {
			waState.setLastError(err.Error())
		}
		return err
	})
	admin_panel.SetWhatsAppLogoutHandler(func() error {
		err := logoutWhatsAppSession(client, waState)
		if err != nil {
			waState.setLastError(err.Error())
		}
		return err
	})
	admin_panel.SetClientInfoSendMessageHandler(func(participantPhone string, text string) error {
		return sendMessage(client, participantPhone, text, "manual_message")
	})
	admin_panel.SetAutoMessageRetrySendHandler(func(taskID int64) error {
		return cron_task.RetryPastDueAutoMessageTask(client, taskID)
	})
	admin_panel.SetEnrollmentBaselineInviteHandler(func(participantPhone string) error {
		msg, err := survey.ComposeBaselineInvitationMessage(participantPhone)
		if err != nil {
			return err
		}
		return sendMessage(client, participantPhone, msg, common.MessageNatureBaselineInvitationMessage)
	})
	admin_panel.SetVerificationApprovedHandler(func(participantPhone string) error {
		return sendInitialAIMessageToParticipant(client, participantPhone)
	})
	survey.SetSchedulingHooks(survey.SchedulingHooks{
		ScheduleAutoAIMessages:       cron_task.ScheduleAutoAIMessagesForParticipant,
		ScheduleAutoFollowupMessages: cron_task.ScheduleAutoFollowupMessagesForParticipant,
		DeletePendingFollowup:        db.DeletePendingAutoFollowupSchedules,
		AfterBaselineCompleted: func(participantPhone string) error {
			return sendPostBaselineMessage(client, participantPhone)
		},
	})

	// Register event handler that listens for incoming messages.
	client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) { // Branch based on concrete event type.
		case *events.Message: // Handle text/caption messages here.
			handleIncomingMessage(client, v) // Process inbound text and optional auto-reply.
		}
	})
	err = connectWhatsAppWithQR(client, waState) // Connect and start QR watcher when login is needed.
	if err != nil {
		waState.setLastError(err.Error())
		panic(fmt.Errorf("connect whatsapp: %w", err))
	}

	setMessageHandlingStart(time.Now()) // Ignore old replayed inbound events from before this process connected.

	cron_task.StartAutoAIMessageCron(client)       // Start immediate + daily AI auto-message cron worker.
	cron_task.StartAutoFollowupMessageCron(client) // Start immediate + daily follow-up prompt cron worker.
	cron_task.StartAutoManualMessageCron(client)   // Start immediate + daily manual-message cron worker.

	// Optional startup message: send once only when explicitly enabled.
	targetJID := strings.TrimSpace(os.Getenv("TARGET_JID"))
	bootMessage := strings.TrimSpace(os.Getenv("BOOT_MESSAGE"))
	if envBoolEnabled("ENABLE_BOOT_MESSAGE") && targetJID != "" && bootMessage != "" {
		err = sendMessage(client, targetJID, bootMessage, "manual_message") // Send configured startup message.
		if err != nil {                                                     // Check startup send failure.
			log.Println("Startup send error:", err) // Log but keep bot running.
		}
	}

	select {} // Keep process alive forever so event handler continues running.
} // End main function.
