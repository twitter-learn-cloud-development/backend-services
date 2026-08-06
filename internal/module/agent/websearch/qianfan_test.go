package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentModel "twitter-clone/internal/module/agent/model"
)

func TestQianfanProviderSearchNormalizesReferences(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s", request.Method)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload qianfanRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(payload.Messages) != 1 ||
			payload.Messages[0].Role != "user" ||
			payload.Messages[0].Content != "Go agent search" ||
			payload.SearchSource != qianfanSearchSource ||
			len(payload.ResourceTypeFilter) != 1 ||
			payload.ResourceTypeFilter[0].Type != "web" ||
			payload.ResourceTypeFilter[0].TopK != 2 ||
			!payload.SafeSearch {
			t.Errorf("request payload = %+v", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"request_id":"request-1",
			"references":[
				{"id":1,"title":"<em>First</em> result","url":"https://example.com/a#fragment","snippet":"Useful <strong>snippet</strong>","type":"web"},
				{"id":2,"title":"Image","url":"https://example.com/image","content":"drop me","type":"image"},
				{"id":3,"title":"Unsafe","url":"javascript:alert(1)","content":"drop me","type":"web"},
				{"id":4,"web_anchor":"Second result","url":"https://example.org/b?q=1","content":"Second content","type":"web"}
			]
		}`))
	}))
	defer server.Close()

	provider, err := NewQianfanProvider(QianfanConfig{
		BaseURL:        server.URL,
		APIKey:         "test-secret",
		MaxResults:     2,
		EndpointPolicy: agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewQianfanProvider() error = %v", err)
	}
	result, err := provider.Search(context.Background(), Request{
		Query: "  Go   agent search  ",
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.Schema != "web.search.v1" ||
		result.Provider != QianfanProviderName ||
		result.Query != "Go agent search" ||
		len(result.Items) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Items[0].Title != "First result" ||
		result.Items[0].Snippet != "Useful snippet" ||
		result.Items[0].URL != "https://example.com/a" ||
		result.Items[0].Rank != 1 {
		t.Fatalf("first item = %+v", result.Items[0])
	}
	if result.Items[1].Title != "Second result" ||
		result.Items[1].Snippet != "Second content" ||
		result.Items[1].URL != "https://example.org/b?q=1" ||
		result.Items[1].Rank != 2 {
		t.Fatalf("second item = %+v", result.Items[1])
	}
}

func TestQianfanProviderFailsClosedWithoutLeakingProviderBody(t *testing.T) {
	t.Parallel()

	if _, err := NewQianfanProvider(QianfanConfig{
		BaseURL: DefaultQianfanBaseURL,
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing key error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":216003,"message":"private-provider-detail"}`))
	}))
	defer server.Close()
	provider, err := NewQianfanProvider(QianfanConfig{
		BaseURL:        server.URL,
		APIKey:         "test-secret",
		EndpointPolicy: agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewQianfanProvider() error = %v", err)
	}
	_, err = provider.Search(context.Background(), Request{Query: "query"})
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("Search() error = %v", err)
	}
	if strings.Contains(err.Error(), "private-provider-detail") {
		t.Fatalf("provider response body leaked in error: %v", err)
	}
}

func TestProviderFactoryRoutesQianfanAndSharesAdmission(t *testing.T) {
	t.Parallel()

	factory, err := NewProviderFactory(ProviderFactoryConfig{
		MaxConcurrent:  2,
		EndpointPolicy: agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewProviderFactory() error = %v", err)
	}
	provider, err := factory.NewFor(QianfanProviderName, "http://127.0.0.1/search", "secret")
	if err != nil {
		t.Fatalf("NewFor(qianfan) error = %v", err)
	}
	qianfan, ok := provider.(*QianfanProvider)
	if !ok {
		t.Fatalf("provider = %T", provider)
	}
	if qianfan.admission != factory.admission || cap(qianfan.admission) != 2 {
		t.Fatal("Qianfan provider does not share the deployment admission gate")
	}
	if _, err := factory.NewFor("unknown", "https://example.com/search", "secret"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("NewFor(unknown) error = %v", err)
	}
}
