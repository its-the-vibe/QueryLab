package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/its-the-vibe/QueryLab/internal/config"
	queryservice "github.com/its-the-vibe/QueryLab/internal/query"
)

type fakeExecutor struct {
	commandOutput map[string][]byte
	err           error
	lastCommand   string
}

func (f *fakeExecutor) Execute(_ context.Context, command string) ([]byte, error) {
	f.lastCommand = command
	if f.err != nil {
		return nil, f.err
	}
	output, ok := f.commandOutput[command]
	if !ok {
		return nil, errors.New("unknown command")
	}
	return output, nil
}

func TestBuildAllowedSchemaTablesAndMatch(t *testing.T) {
	t.Parallel()

	allowed := buildAllowedSchemaTables([]config.AllowedTable{
		{Dataset: "analytics", Table: "events"},
		{Dataset: "analytics", Table: "users"},
		{Dataset: " reporting ", Table: " daily "},
		{Dataset: " ", Table: "ignored"},
		{Dataset: "analytics", Table: " "},
	})

	if !isAllowedSchemaTable(allowed, "analytics", "events") {
		t.Fatalf("expected analytics.events to be allowed")
	}
	if isAllowedSchemaTable(allowed, "analytics", "missing") {
		t.Fatalf("expected analytics.missing to be rejected")
	}
	if isAllowedSchemaTable(allowed, "missing", "events") {
		t.Fatalf("expected missing.events to be rejected")
	}
	if !isAllowedSchemaTable(allowed, "reporting", "daily") {
		t.Fatalf("expected trimmed reporting.daily to be allowed")
	}
}

func TestIsTrustedRequestOrigin(t *testing.T) {
	t.Parallel()

	allowedOrigins := buildAllowedOrigins([]string{
		"https://querylab.local",
		"https://app.querylab.local:8443",
	})

	tests := []struct {
		name     string
		origin   string
		referer  string
		expected bool
	}{
		{name: "matching origin", origin: "https://querylab.local", expected: true},
		{name: "matching origin with configured port", origin: "https://app.querylab.local:8443", expected: true},
		{name: "wrong scheme for allowed host", origin: "http://querylab.local", expected: false},
		{name: "non-http scheme", origin: "file://querylab.local/tmp", expected: false},
		{name: "mismatched origin", origin: "https://evil.example", expected: false},
		{name: "matching referer", referer: "https://querylab.local/schema", expected: true},
		{name: "mismatched referer", referer: "https://evil.example/schema", expected: false},
		{name: "no origin or referer", expected: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodPost, "http://querylab.local/schema", nil)
			if err != nil {
				t.Fatalf("http.NewRequest() error = %v", err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			if got := isTrustedRequestOrigin(req, allowedOrigins); got != tt.expected {
				t.Fatalf("isTrustedRequestOrigin() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestServerExecutesOnlyDiscoveredQueries(t *testing.T) {
	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list --verbose": []byte(`["known"]`),
		"./goquery --json query 'known'":  []byte(`{"rows":[{"count":1}]}`),
	}}
	server, err := NewServer(&config.Config{}, executor)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	homeResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(homeResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if homeResponse.Code != http.StatusSeeOther || homeResponse.Header().Get("Location") != "/queries" {
		t.Fatalf("home response = (%d, %q), want redirect to /queries", homeResponse.Code, homeResponse.Header().Get("Location"))
	}

	queryPageResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(queryPageResponse, httptest.NewRequest(http.MethodGet, "/queries", nil))
	if queryPageResponse.Code != http.StatusOK || !strings.Contains(queryPageResponse.Body.String(), "Run a query") {
		t.Fatalf("query page response = (%d, %q), want query page", queryPageResponse.Code, queryPageResponse.Body.String())
	}
	if strings.Contains(queryPageResponse.Body.String(), `action="/schema"`) {
		t.Fatalf("query page unexpectedly contains the schema form: %q", queryPageResponse.Body.String())
	}
	if !strings.Contains(queryPageResponse.Body.String(), `name="query"`) {
		t.Fatalf("query page is missing the query selector: %q", queryPageResponse.Body.String())
	}

	invalidRequest := httptest.NewRequest(http.MethodGet, "/queries?query=unknown", nil)
	invalidResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusOK || !strings.Contains(invalidResponse.Body.String(), "invalid query selected") {
		t.Fatalf("invalid query response = (%d, %q), want 200 and validation error", invalidResponse.Code, invalidResponse.Body.String())
	}

	for refresh := 0; refresh < 2; refresh++ {
		validResponse := httptest.NewRecorder()
		server.Handler.ServeHTTP(validResponse, httptest.NewRequest(http.MethodGet, "/queries?query=known", nil))
		if validResponse.Code != http.StatusOK || !strings.Contains(validResponse.Body.String(), "<td>1</td>") {
			t.Fatalf("valid query response = (%d, %q), want rendered query result", validResponse.Code, validResponse.Body.String())
		}
	}
}

func TestServerDisplaysAndExecutesQueryParameters(t *testing.T) {
	expectedCommand := "./goquery --json query 'parameterized' -p 'count=3' -p 'date=2026-10-19' -p 'enabled=false'"
	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list --verbose": []byte(`[{"name":"plain"},{"name":"parameterized","parameters":[{"name":"date","type":"string"},{"name":"count","type":"number"},{"name":"enabled","type":"boolean"}]}]`),
		expectedCommand:                   []byte(`{"rows":[{"ok":true}]}`),
	}}
	server, err := NewServer(&config.Config{}, executor)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	pageResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/queries", nil))
	page := pageResponse.Body.String()
	for _, expected := range []string{`name="param_date"`, `type="number"`, `Not specified`, `value="false"`, `data-query="parameterized"`} {
		if !strings.Contains(page, expected) {
			t.Errorf("query page missing %q: %s", expected, page)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/queries?query=parameterized&param_date=2026-10-19&param_count=3&param_enabled=false&param_unknown=ignored", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<td>true</td>") {
		t.Fatalf("parameterized query response = (%d, %q), want rendered query result", response.Code, response.Body.String())
	}
	if executor.lastCommand != expectedCommand {
		t.Fatalf("last command = %q, want %q", executor.lastCommand, expectedCommand)
	}
}

func TestRefreshQueries(t *testing.T) {
	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list --verbose":           []byte(`["old"]`),
		"./goquery --json query 'new' -p 'limit=5'": []byte(`[{"ok":true}]`),
	}}
	server, err := NewServer(&config.Config{}, executor)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/refresh-queries", nil)
		req.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, req)
		return response
	}

	for _, tt := range []struct {
		method, contentType string
		status              int
	}{
		{http.MethodGet, "application/json", http.StatusMethodNotAllowed},
		{http.MethodPost, "", http.StatusUnsupportedMediaType},
		{http.MethodPost, "text/plain", http.StatusUnsupportedMediaType},
	} {
		response := request(tt.method, tt.contentType)
		if response.Code != tt.status || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("refresh %s %q = (%d, %s)", tt.method, tt.contentType, response.Code, response.Body.String())
		}
	}

	executor.commandOutput["./goquery --json list --verbose"] = []byte(`[{"name":"new","parameters":[{"name":"limit","type":"number"}]}]`)
	response := request(http.MethodPost, "application/json; charset=utf-8")
	var result struct {
		Queries []queryservice.QueryMetadata `json:"queries"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		!reflect.DeepEqual(result.Queries, []queryservice.QueryMetadata{{Name: "new", Parameters: []queryservice.QueryParameter{{Name: "limit", Type: "number"}}}}) {
		t.Fatalf("refresh response = (%d, %s)", response.Code, response.Body.String())
	}

	page := httptest.NewRecorder()
	server.Handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/queries?refreshed=1", nil))
	for _, expected := range []string{`Refresh queries`, `Query list refreshed successfully.`, `value="new"`, `name="param_limit"`} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Errorf("refreshed page missing %q", expected)
		}
	}
	if strings.Contains(page.Body.String(), `value="old"`) {
		t.Error("refreshed page still contains removed query")
	}
	for _, tt := range []struct{ url, expected string }{
		{"/queries?query=new&param_limit=5", "<td>true</td>"},
		{"/queries?query=old", "invalid query selected"},
	} {
		page = httptest.NewRecorder()
		server.Handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, tt.url, nil))
		if !strings.Contains(page.Body.String(), tt.expected) {
			t.Errorf("%s: expected %q in %s", tt.url, tt.expected, page.Body.String())
		}
	}

	for _, invalid := range []string{`not json`, `[]`} {
		executor.commandOutput["./goquery --json list --verbose"] = []byte(invalid)
		response = request(http.MethodPost, "application/json")
		if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"error"`) {
			t.Fatalf("failed refresh = (%d, %s)", response.Code, response.Body.String())
		}
	}
	executor.err = errors.New("unavailable")
	response = request(http.MethodPost, "application/json")
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"error"`) {
		t.Fatalf("executor failure = (%d, %s)", response.Code, response.Body.String())
	}
	page = httptest.NewRecorder()
	server.Handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/queries", nil))
	if !strings.Contains(page.Body.String(), `value="new"`) {
		t.Fatal("failed refresh discarded the previous query list")
	}
}

type concurrentRefreshExecutor struct {
	active  atomic.Int32
	overlap atomic.Bool
}

func (e *concurrentRefreshExecutor) Execute(ctx context.Context, command string) ([]byte, error) {
	if e.active.Add(1) != 1 {
		e.overlap.Store(true)
	}
	defer e.active.Add(-1)
	select {
	case <-time.After(time.Millisecond):
		return []byte(`[{"name":"known","parameters":[{"name":"limit","type":"number"}]}]`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestConcurrentRefreshQueries(t *testing.T) {
	executor := &concurrentRefreshExecutor{}
	server, err := NewServer(&config.Config{}, executor)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodPost, "/api/refresh-queries", nil)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Errorf("refresh status = %d", response.Code)
			}
		}()
		go func() {
			defer wg.Done()
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/queries?query=missing", nil))
			if !strings.Contains(response.Body.String(), "invalid query selected") {
				t.Error("query validation failed during refresh")
			}
		}()
	}
	wg.Wait()
	if executor.overlap.Load() {
		t.Error("discovery commands ran concurrently")
	}
}

func TestSchemaAPIEnforcesOriginAndTableAllowlist(t *testing.T) {
	cfg := &config.Config{
		Poppit: config.PoppitConfig{CommandTimeoutSeconds: 1},
		Schema: config.SchemaConfig{
			AllowedOrigins: []string{"https://querylab.local"},
			AllowedTables:  []config.AllowedTable{{Dataset: "analytics", Table: "events"}},
		},
	}
	executor := &fakeExecutor{commandOutput: map[string][]byte{
		"./goquery --json list --verbose":              []byte(`["known"]`),
		"./goquery --json schema 'analytics' 'events'": []byte(`[{"name":"id","type":"INTEGER"}]`),
	}}
	server, err := NewServer(cfg, executor)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	schemaPageResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(schemaPageResponse, httptest.NewRequest(http.MethodGet, "/schema", nil))
	if schemaPageResponse.Code != http.StatusOK || !strings.Contains(schemaPageResponse.Body.String(), "Inspect a schema") {
		t.Fatalf("schema page response = (%d, %q), want schema page", schemaPageResponse.Code, schemaPageResponse.Body.String())
	}
	if strings.Contains(schemaPageResponse.Body.String(), `action="/queries"`) {
		t.Fatalf("schema page unexpectedly contains the query form: %q", schemaPageResponse.Body.String())
	}

	for refresh := 0; refresh < 2; refresh++ {
		schemaUIResponse := httptest.NewRecorder()
		server.Handler.ServeHTTP(schemaUIResponse, httptest.NewRequest(http.MethodGet, "/schema?dataset=analytics&table=events", nil))
		if schemaUIResponse.Code != http.StatusOK || !strings.Contains(schemaUIResponse.Body.String(), "<td>id</td>") {
			t.Fatalf("schema UI response = (%d, %q), want rendered schema result", schemaUIResponse.Code, schemaUIResponse.Body.String())
		}
	}

	newRequest := func(dataset, table string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/schema", strings.NewReader("dataset="+dataset+"&table="+table))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return request
	}

	untrustedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(untrustedResponse, newRequest("analytics", "events"))
	if untrustedResponse.Code != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %d, want %d", untrustedResponse.Code, http.StatusForbidden)
	}

	disallowedRequest := newRequest("analytics", "private")
	disallowedRequest.Header.Set("Origin", "https://querylab.local")
	disallowedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(disallowedResponse, disallowedRequest)
	if disallowedResponse.Code != http.StatusBadRequest {
		t.Fatalf("disallowed table status = %d, want %d", disallowedResponse.Code, http.StatusBadRequest)
	}

	allowedRequest := newRequest("analytics", "events")
	allowedRequest.Header.Set("Origin", "https://querylab.local")
	allowedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(allowedResponse, allowedRequest)
	if allowedResponse.Code != http.StatusOK || !strings.Contains(allowedResponse.Body.String(), `"name":"id"`) {
		t.Fatalf("allowed schema response = (%d, %q), want schema JSON", allowedResponse.Code, allowedResponse.Body.String())
	}
}

func TestBuildAllowedOrigins(t *testing.T) {
	t.Parallel()

	origins := buildAllowedOrigins([]string{
		"https://querylab.local",
		" https://QUERYLAB.local ",
		"ftp://querylab.local",
		"invalid",
		"",
	})

	if len(origins) != 1 {
		t.Fatalf("len(origins) = %d, want 1", len(origins))
	}
	if _, ok := origins["https://querylab.local"]; !ok {
		t.Fatalf("expected https://querylab.local in allowed origins")
	}
}

func TestBuildSchemaSelections(t *testing.T) {
	t.Parallel()

	datasets, schemaTablesByDataset := buildSchemaSelections(map[string]map[string]struct{}{
		"beta":  {"b2": {}, "b1": {}},
		"alpha": {"a1": {}},
	})

	expectedDatasets := []string{"alpha", "beta"}
	if !reflect.DeepEqual(datasets, expectedDatasets) {
		t.Fatalf("datasets = %v, want %v", datasets, expectedDatasets)
	}
	expectedTables := map[string][]string{
		"alpha": {"a1"},
		"beta":  {"b1", "b2"},
	}
	if !reflect.DeepEqual(schemaTablesByDataset, expectedTables) {
		t.Fatalf("schemaTablesByDataset = %v, want %v", schemaTablesByDataset, expectedTables)
	}
}

func TestSelectSchemaDatasetAndTable(t *testing.T) {
	t.Parallel()

	schemaDatasets := []string{"alpha", "beta"}
	schemaTablesByDataset := map[string][]string{
		"alpha": {"a1", "a2"},
		"beta":  {"b1"},
	}

	tests := []struct {
		name            string
		selectedDataset string
		selectedTable   string
		wantDataset     string
		wantTable       string
	}{
		{name: "defaults to first values", wantDataset: "alpha", wantTable: "a1"},
		{name: "keeps valid selection", selectedDataset: "alpha", selectedTable: "a2", wantDataset: "alpha", wantTable: "a2"},
		{name: "falls back invalid table", selectedDataset: "alpha", selectedTable: "missing", wantDataset: "alpha", wantTable: "a1"},
		{name: "selects default table for dataset", selectedDataset: "beta", wantDataset: "beta", wantTable: "b1"},
		{name: "empty options", selectedDataset: "missing", selectedTable: "missing", wantDataset: "missing", wantTable: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotDataset, gotTable := selectSchemaDatasetAndTable(schemaDatasets, schemaTablesByDataset, tt.selectedDataset, tt.selectedTable)
			if gotDataset != tt.wantDataset || gotTable != tt.wantTable {
				t.Fatalf("selectSchemaDatasetAndTable() = (%q, %q), want (%q, %q)", gotDataset, gotTable, tt.wantDataset, tt.wantTable)
			}
		})
	}
}
