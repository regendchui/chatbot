package admin_panel

import (
	"log"
	"net/http"
	"os"
	"strings"
)

// StartAdminPanelHTTPServer starts a dedicated HTTP server for CMS/admin routes.
func StartAdminPanelHTTPServer() {
	addr := strings.TrimSpace(os.Getenv("ADMIN_PANEL_HTTP_ADDR"))
	if addr == "" {
		addr = ":8081"
	}
	mux := http.NewServeMux()
	registerAdminPanelRoutes(mux)
	go func() {
		log.Printf("admin panel HTTP listening on %s", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("admin panel HTTP server error: %v", err)
		}
	}()
}

func registerAdminPanelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/login", adminLoginHandler)
	mux.HandleFunc("/admin/logout", adminLogoutHandler)
	mux.HandleFunc("/admin/home", adminRequireAuth(adminHomePageHandler))
	mux.HandleFunc("/admin/home/", adminRequireAuth(adminHomePageHandler))
	mux.HandleFunc("/admin/table/conversation", adminRequireAuth(adminConversationHandler))
	mux.HandleFunc("/admin/table/conversation/export", adminRequireAuth(adminConversationExportCSVHandler))
	mux.HandleFunc("/admin/table/conversation/delete-one", adminRequireAuth(adminConversationDeleteOneHandler))
	mux.HandleFunc("/admin/table/conversation/delete-by-phone", adminRequireAuth(adminConversationDeleteByPhoneHandler))
	mux.HandleFunc("/admin/survey-responses", adminRequireAuth(adminSurveyResponsesHandler))
	mux.HandleFunc("/admin/survey-responses/export", adminRequireAuth(adminSurveyResponsesExportCSVHandler))
	mux.HandleFunc("/admin/survey-responses/delete-one", adminRequireAuth(adminSurveyResponsesDeleteOneHandler))
	mux.HandleFunc("/admin/survey-responses/delete-orphans", adminRequireAuth(adminSurveyResponsesDeleteOrphansHandler))
	mux.HandleFunc("/admin/table/meta", adminRequireAuth(adminMetaHandler))
	mux.HandleFunc("/admin/table/meta/export", adminRequireAuth(adminMetaExportCSVHandler))
	mux.HandleFunc("/admin/enrollment", adminRequireAuth(adminEnrollmentHandler))
	mux.HandleFunc("/admin/enrollment/add", adminRequireAuth(adminEnrollmentAddHandler))
	mux.HandleFunc("/admin/enrollment/delete", adminRequireAuth(adminEnrollmentDeleteHandler))
	mux.HandleFunc("/admin/blacklist", adminRequireAuth(adminBlacklistHandler))
	mux.HandleFunc("/admin/blacklist/add", adminRequireAuth(adminBlacklistAddHandler))
	mux.HandleFunc("/admin/blacklist/remove", adminRequireAuth(adminBlacklistRemoveHandler))
	mux.HandleFunc("/admin/role", adminRequireAuth(adminRoleHandler))
	mux.HandleFunc("/admin/role/add", adminRequireAuth(adminRoleAddHandler))
	mux.HandleFunc("/admin/role/permissions/update", adminRequireAuth(adminRoleUpdatePermissionsHandler))
	mux.HandleFunc("/admin/role/password/reset", adminRequireAuth(adminRoleResetPasswordHandler))
	mux.HandleFunc("/admin/role/delete", adminRequireAuth(adminRoleDeleteHandler))
	mux.HandleFunc("/admin/table/auto-messages", adminRequireAuth(adminAutoMessagesHandler))
	mux.HandleFunc("/admin/table/auto-messages/export", adminRequireAuth(adminAutoMessagesExportCSVHandler))
	mux.HandleFunc("/admin/table/auto-messages/retry-send", adminRequireAuth(adminAutoMessageRetrySendHandler))
	mux.HandleFunc("/admin/table/auto-messages/insert", adminRequireAuth(adminAutoMessageInsertHandler))
	mux.HandleFunc("/admin/table/auto-messages/delete", adminRequireAuth(adminAutoMessageDeleteHandler))
	mux.HandleFunc("/admin/table/auto-messages/delete-by-phone", adminRequireAuth(adminAutoMessageDeleteByPhoneHandler))
	mux.HandleFunc("/admin/rag", adminRequireAuth(adminRAGHandler))
	mux.HandleFunc("/admin/rag/add", adminRequireAuth(adminRAGAddHandler))
	mux.HandleFunc("/admin/rag/delete-document", adminRequireAuth(adminRAGDeleteDocumentHandler))
	mux.HandleFunc("/admin/table/embedding", adminRequireAuth(adminEmbeddingTableHandler))
	mux.HandleFunc("/admin/client-info", adminRequireAuth(adminClientInfoHandler))
	mux.HandleFunc("/admin/client-info/send", adminRequireAuth(adminClientInfoSendHandler))
	mux.HandleFunc("/admin/risk-message", adminRequireAuth(adminRiskMessageHandler))
	mux.HandleFunc("/admin/table/db-tables", adminRequireAuth(adminDBTablesHandler))
	mux.HandleFunc("/admin/table/db-tables/export", adminRequireAuth(adminDBTablesExportCSVHandler))
	mux.HandleFunc("/admin/table/project-setting", adminRequireAuth(adminProjectSettingTableHandler))
	mux.HandleFunc("/admin/verification", adminRequireAuth(adminVerificationHandler))
	mux.HandleFunc("/admin/verification/approve", adminRequireAuth(adminVerificationApproveHandler))
	mux.HandleFunc("/admin/verification/approve-no-ai", adminRequireAuth(adminVerificationApproveNoAIHandler))
	mux.HandleFunc("/admin/verification/unverify", adminRequireAuth(adminVerificationUnverifyHandler))
	mux.HandleFunc("/admin/whatsapp", adminRequireAuth(adminWhatsAppHandler))
	mux.HandleFunc("/admin/whatsapp/logout", adminRequireAuth(adminWhatsAppLogoutHandler))
	mux.HandleFunc("/admin/configuration", adminRequireAuth(adminConfigurationHandler))
	mux.HandleFunc("/admin/configuration/update/ai", adminRequireAuth(adminConfigurationUpdateAIHandler))
	mux.HandleFunc("/admin/configuration/update/voice-message", adminRequireAuth(adminConfigurationUpdateVoiceHandler))
	mux.HandleFunc("/admin/configuration/update/survey-thankyou", adminRequireAuth(adminConfigurationUpdateSurveyThankYouHandler))
	mux.HandleFunc("/admin/configuration/update/risk-message", adminRequireAuth(adminConfigurationUpdateRiskMessageHandler))
	mux.HandleFunc("/admin/configuration/update/behavior", adminRequireAuth(adminConfigurationUpdateBehaviorHandler))
	mux.HandleFunc("/admin/configuration/update/rag", adminRequireAuth(adminConfigurationUpdateRAGHandler))
	mux.HandleFunc("/admin/configuration/update/verification-message", adminRequireAuth(adminConfigurationUpdateVerificationMessageHandler))
	mux.HandleFunc("/admin/configuration/update/cron-delay", adminRequireAuth(adminConfigurationUpdateCronDelayHandler))
	mux.HandleFunc("/admin/configuration/update/intervention-message", adminRequireAuth(adminConfigurationUpdateInterventionMessageHandler))
	mux.HandleFunc("/admin/configuration/update/admin-credentials", adminRequireAuth(adminConfigurationUpdateAdminCredentialsHandler))
	mux.HandleFunc("/admin/configuration/logout-all-admin-sessions", adminRequireAuth(adminConfigurationLogoutAllAdminSessionsHandler))
	mux.HandleFunc("/admin/configuration/update/json/text", adminRequireAuth(adminConfigurationUpdateJSONTextHandler))
	mux.HandleFunc("/admin/configuration/update/json/url", adminRequireAuth(adminConfigurationUpdateJSONURLHandler))
	mux.HandleFunc("/admin/configuration/update/json/file", adminRequireAuth(adminConfigurationUpdateJSONFileHandler))
	mux.HandleFunc("/admin/log", adminRequireAuth(adminLogHandler))
}

func adminHomePageHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin/home" {
		http.Redirect(w, r, "/admin/home/", http.StatusSeeOther)
		return
	}
	adminHomeHandler(w, r)
}
