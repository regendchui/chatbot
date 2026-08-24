package admin_panel

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"whatsapp-bot/db"
)

const adminSessionCookieName = "admin_panel_session"

type adminSessionContextKey struct{}

type adminSession struct {
	Username  string
	ExpiresAt time.Time
	IsRoot    bool
	CSRFToken string
}

var adminSessions = struct {
	sync.Mutex
	byToken map[string]adminSession
}{
	byToken: map[string]adminSession{},
}

func adminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		adminRenderLoginPage(w, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		adminRenderLoginPage(w, "Invalid form data.")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	now := time.Now()
	clientIP := adminClientIP(r)
	userKey := adminRateLimitUserKey(username)
	if allowed, wait := adminLoginAllowed(clientIP, userKey, now); !allowed {
		adminRecordLoginHistory(username, "unknown", false, "rate_limited", clientIP)
		adminRenderLoginPage(w, fmt.Sprintf("Too many login attempts. Try again in %s.", adminFormatWait(wait)))
		return
	}
	isRoot, ok := adminAuthenticateCredentials(username, password)
	if !ok {
		adminRecordLoginFailure(clientIP, userKey, now)
		adminRecordLoginHistory(username, "unknown", false, "invalid_credentials", clientIP)
		adminRenderLoginPage(w, "Invalid username or password.")
		return
	}
	userType := "role_user"
	if isRoot {
		userType = "admin"
	}
	adminRecordLoginHistory(username, userType, true, "", clientIP)
	adminClearLoginGuards(clientIP, userKey)
	token, created := adminCreateSessionToken(username, isRoot)
	if !created {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(token) == "" {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	secureCookie := adminShouldUseSecureCookie(r)
	expiresAt := adminSessionExpiryFromNow(time.Now())
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(adminSessionTimeoutDuration().Seconds()),
	})
	http.Redirect(w, r, "/admin/home/", http.StatusSeeOther)
}

func adminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	adminDestroySession(r)
	secureCookie := adminShouldUseSecureCookie(r)
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func adminRequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := adminCurrentSession(r)
		if !ok {
			adminExpireSessionCookie(w, r)
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		if !adminSessionCanAccess(session, r.URL.Path) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), adminSessionContextKey{}, session)
		next(w, r.WithContext(ctx))
	}
}

func adminAuthenticateCredentials(username string, password string) (bool, bool) {
	ok, err := db.VerifyAdminCredentials(username, password)
	if err != nil {
		return false, false
	}
	if ok {
		return true, true
	}
	roleOK, roleErr := db.VerifyRoleCredentials(username, password)
	if roleErr != nil {
		return false, false
	}
	if roleOK {
		return false, true
	}
	return false, false
}

func adminCreateSessionToken(username string, isRoot bool) (string, bool) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", false
	}
	csrfBuf := make([]byte, 24)
	if _, err := rand.Read(csrfBuf); err != nil {
		return "", false
	}
	token := hex.EncodeToString(buf)
	adminSessions.Lock()
	adminSessions.byToken[token] = adminSession{
		Username:  strings.TrimSpace(username),
		ExpiresAt: adminSessionExpiryFromNow(time.Now()),
		IsRoot:    isRoot,
		CSRFToken: hex.EncodeToString(csrfBuf),
	}
	adminSessions.Unlock()
	return token, true
}

func adminRequireCSRF(w http.ResponseWriter, r *http.Request) bool {
	session, ok := adminSessionFromRequest(r)
	if !ok || strings.TrimSpace(session.CSRFToken) == "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	if provided == "" {
		provided = strings.TrimSpace(r.FormValue("csrf_token"))
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return false
	}
	return true
}

func adminCurrentSession(r *http.Request) (adminSession, bool) {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return adminSession{}, false
	}
	token := strings.TrimSpace(cookie.Value)
	now := time.Now()
	adminSessions.Lock()
	defer adminSessions.Unlock()
	session, ok := adminSessions.byToken[token]
	if !ok {
		return adminSession{}, false
	}
	if now.After(session.ExpiresAt) {
		delete(adminSessions.byToken, token)
		return adminSession{}, false
	}
	return session, true
}

func adminDestroySession(r *http.Request) {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return
	}
	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return
	}
	adminSessions.Lock()
	delete(adminSessions.byToken, token)
	adminSessions.Unlock()
}

func adminDestroyAllSessions() {
	adminSessions.Lock()
	adminSessions.byToken = map[string]adminSession{}
	adminSessions.Unlock()
}

func adminExpireSessionCookie(w http.ResponseWriter, r *http.Request) {
	secureCookie := adminShouldUseSecureCookie(r)
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func adminSessionCanAccess(session adminSession, requestedPath string) bool {
	if session.IsRoot {
		return true
	}
	path := adminPermissionBasePath(requestedPath)
	if path == "/admin/logout" {
		return true
	}
	allowed, err := db.RoleUserCanAccessPath(session.Username, path)
	if err != nil {
		return false
	}
	return allowed
}

func adminPermissionBasePath(path string) string {
	p := strings.TrimSpace(path)
	switch {
	case p == "/admin/home" || p == "/admin/home/":
		return "/admin/home"
	case strings.HasPrefix(p, "/admin/table/conversation"):
		return "/admin/table/conversation"
	case strings.HasPrefix(p, "/admin/client-info"):
		return "/admin/client-info"
	case strings.HasPrefix(p, "/admin/enrollment"):
		return "/admin/enrollment"
	case strings.HasPrefix(p, "/admin/blacklist"):
		return "/admin/blacklist"
	case strings.HasPrefix(p, "/admin/survey-responses"):
		return "/admin/survey-responses"
	case strings.HasPrefix(p, "/admin/table/meta"):
		return "/admin/table/meta"
	case strings.HasPrefix(p, "/admin/verification"):
		return "/admin/verification"
	case strings.HasPrefix(p, "/admin/table/auto-messages"):
		return "/admin/table/auto-messages"
	case strings.HasPrefix(p, "/admin/table/db-tables"):
		return "/admin/table/db-tables"
	case strings.HasPrefix(p, "/admin/table/project-setting"):
		return "/admin/table/project-setting"
	case strings.HasPrefix(p, "/admin/whatsapp"):
		return "/admin/whatsapp"
	case strings.HasPrefix(p, "/admin/configuration"):
		return "/admin/configuration"
	case strings.HasPrefix(p, "/admin/role"):
		return "/admin/role"
	case strings.HasPrefix(p, "/admin/rag"):
		return "/admin/rag"
	case strings.HasPrefix(p, "/admin/intention-routing-rag"):
		return "/admin/intention-routing-rag"
	case strings.HasPrefix(p, "/admin/table/embedding"):
		return "/admin/table/embedding"
	case strings.HasPrefix(p, "/admin/log"):
		return "/admin/log"
	default:
		return p
	}
}

func adminSessionFromRequest(r *http.Request) (adminSession, bool) {
	if r == nil {
		return adminSession{}, false
	}
	if v := r.Context().Value(adminSessionContextKey{}); v != nil {
		if s, ok := v.(adminSession); ok {
			return s, true
		}
	}
	return adminCurrentSession(r)
}

// adminShouldUseSecureCookie enables Secure cookies when:
// 1) request is HTTPS directly, or
// 2) proxy marks HTTPS with X-Forwarded-Proto=https, or
// 3) ADMIN_PANEL_COOKIE_SECURE=true is explicitly set.
func adminShouldUseSecureCookie(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ADMIN_PANEL_COOKIE_SECURE")), "true") {
		return true
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if r != nil {
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			return true
		}
	}
	return false
}

func adminRecordLoginHistory(username string, userType string, success bool, failureType string, clientIP string) {
	if err := db.InsertLoginHistory(username, userType, success, failureType, clientIP); err != nil {
		log.Printf("admin login history insert error: %v", err)
	}
}
