package admin_panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGraphRAGAdminRouteIsAuthenticatedAndPermissionScoped(t *testing.T) {
	mux := http.NewServeMux()
	registerAdminPanelRoutes(mux)
	request := httptest.NewRequest(http.MethodGet, "/admin/graph-rag", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated Graph RAG page status=%d want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); location != "/admin/login" {
		t.Fatalf("redirect=%q want /admin/login", location)
	}
	if got := adminPermissionBasePath("/admin/graph-rag/delete-all"); got != "/admin/graph-rag" {
		t.Fatalf("permission base=%q", got)
	}
}
