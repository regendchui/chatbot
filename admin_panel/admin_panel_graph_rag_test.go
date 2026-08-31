package admin_panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGraphRAGActionConfirmationEscapesJavaScriptString(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/admin/graph-rag", nil)
	got := graphRAGActionForm(request, "/admin/graph-rag/remove", "location.pdf", "Remove", "Remove this document's graph provenance?")
	if strings.Contains(got, `confirm('Remove this document's`) {
		t.Fatalf("confirmation contains a broken single-quoted JavaScript string: %s", got)
	}
	if !strings.Contains(got, `return confirm(&#34;Remove this document&#39;s graph provenance?&#34;);`) {
		t.Fatalf("confirmation was not safely JSON/HTML encoded: %s", got)
	}
}

func TestGraphRAGTableScrollerIsBoundedAndAccessible(t *testing.T) {
	got := graphRAGTableStart("Document ingestion table", 1000)
	for _, want := range []string{
		`aria-label="Document ingestion table"`,
		`tabindex="0"`,
		`max-height:420px`,
		`overflow:auto`,
		`min-width:1000px`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table scroller missing %q: %s", want, got)
		}
	}
}

func TestGraphRAGDeleteConfirmationRequiresLiteralPhrase(t *testing.T) {
	for _, value := range []string{"confirm delete", " confirm delete "} {
		if !graphRAGDeleteConfirmationMatches(value) {
			t.Errorf("confirmation %q should match", value)
		}
	}
	for _, value := range []string{"", "戒得有型", "WhatsApp", "Confirm delete", "confirm-delete"} {
		if graphRAGDeleteConfirmationMatches(value) {
			t.Errorf("confirmation %q should not match", value)
		}
	}
}
