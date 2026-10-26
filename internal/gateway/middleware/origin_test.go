package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOriginPolicyRejectsWildcardAndPaths(t *testing.T) {
	for _, value := range []string{"*", "https://example.com/path", "file://example.com"} {
		if _, err := NewOriginPolicy(value); err == nil {
			t.Fatalf("NewOriginPolicy(%q) succeeded", value)
		}
	}
}

func TestCORSWithPolicyUsesExactOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	policy, err := NewOriginPolicy("https://app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(CORSWithPolicy(policy))
	router.GET("/ok", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	allowed := httptest.NewRequest(http.MethodGet, "/ok", nil)
	allowed.Header.Set("Origin", "https://app.example.com")
	allowedResponse := httptest.NewRecorder()
	router.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusNoContent {
		t.Fatalf("allowed response code = %d", allowedResponse.Code)
	}
	if got := allowedResponse.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow origin = %q", got)
	}

	denied := httptest.NewRequest(http.MethodGet, "/ok", nil)
	denied.Header.Set("Origin", "https://evil.example")
	deniedResponse := httptest.NewRecorder()
	router.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("denied response code = %d", deniedResponse.Code)
	}
}
