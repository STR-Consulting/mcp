package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRegisterTools_NoPanic is the regression test for the launch-time panic
// (issue biy-mdp). Tool registration must succeed for every tool — a single
// malformed registration in main() takes the whole stdio server down before
// any client can connect.
func TestRegisterTools_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerTools panicked: %v", r)
		}
	}()

	s := &server{coreURL: "https://example.invalid", httpClient: http.DefaultClient}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	registerTools(srv, s)
}

// TestRegisterTools_ExpectedTools asserts every tool we advertise in CLAUDE.md
// and the server Instructions is actually registered. Catches accidental
// renames and silently-dropped tools. Drives the MCP protocol via an
// in-memory transport so we exercise the same listTools path real clients use.
func TestRegisterTools_ExpectedTools(t *testing.T) {
	ctx := context.Background()
	s := &server{coreURL: "https://example.invalid", httpClient: http.DefaultClient}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	registerTools(srv, s)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clientSession.Close() }()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}

	want := []string{
		"health_check",
		"list_briefable_portfolios",
		"list_portfolio_teams",
		"get_portfolio_team",
		"list_portfolio_units",
		"list_portfolio_reservations",
		"list_portfolio_new_listings",
		"get_portfolio_pacing",
		"get_portfolio_metrics_ytd",
		"get_portfolio_market_metrics",
		"guesty_pricing_config",
		"guesty_reservation_promotions",
		"get_client_health_brief",
		"upsert_client_health_brief",
		"list_client_health_briefs",
		"get_client_health_scoring_config",
		"upsert_intel_brief",
		"list_managed_keydata_units",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestNewServer_DefaultURL(t *testing.T) {
	t.Setenv(coreURLEnvVar, "")
	t.Setenv(coreTokenEnvVar, "")

	s := newServer()
	if s.coreURL != defaultCoreURL {
		t.Errorf("coreURL = %q, want %q", s.coreURL, defaultCoreURL)
	}
	if s.coreToken != "" {
		t.Errorf("coreToken = %q, want empty", s.coreToken)
	}
}

func TestNewServer_EnvOverrides(t *testing.T) {
	t.Setenv(coreURLEnvVar, "https://custom.example.com")
	t.Setenv(coreTokenEnvVar, "pat_abc123")

	s := newServer()
	if s.coreURL != "https://custom.example.com" {
		t.Errorf("coreURL = %q, want custom URL", s.coreURL)
	}
	if s.coreToken != "pat_abc123" {
		t.Errorf("coreToken = %q, want pat_abc123", s.coreToken)
	}
}

func TestPortfolioPath_EscapesSpecialChars(t *testing.T) {
	cases := []struct {
		portfolio string
		suffix    string
		want      string
	}{
		{"alpha", "/team", "/api/v1/portfolios/alpha/team"},
		{"two words", "/units", "/api/v1/portfolios/two%20words/units"},
		{"slash/here", "/pacing", "/api/v1/portfolios/slash%2Fhere/pacing"},
		{"42", "", "/api/v1/portfolios/42"},
	}
	for _, tc := range cases {
		got := portfolioPath(tc.portfolio, tc.suffix)
		if got != tc.want {
			t.Errorf("portfolioPath(%q, %q) = %q, want %q", tc.portfolio, tc.suffix, got, tc.want)
		}
	}
}

func TestDoGET_SendsBearerTokenAndQuery(t *testing.T) {
	var gotAuth, gotQuery, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := &server{coreURL: ts.URL, coreToken: "pat_xyz", httpClient: ts.Client()}
	q := map[string][]string{"month": {"2026-05"}, "flat": {"true"}}
	body, err := s.doGET(context.Background(), "/api/v1/portfolios/foo/promos", q)
	if err != nil {
		t.Fatalf("doGET: %v", err)
	}

	if gotAuth != "Bearer pat_xyz" {
		t.Errorf("Authorization = %q, want Bearer pat_xyz", gotAuth)
	}
	if gotPath != "/api/v1/portfolios/foo/promos" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "month=2026-05") || !strings.Contains(gotQuery, "flat=true") {
		t.Errorf("query = %q", gotQuery)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s", string(body))
	}
}

func TestDoGET_OmitsAuthHeaderWhenNoToken(t *testing.T) {
	var sawAuth bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	s := &server{coreURL: ts.URL, httpClient: ts.Client()}
	if _, err := s.doGET(context.Background(), "/x", nil); err != nil {
		t.Fatalf("doGET: %v", err)
	}
	if sawAuth {
		t.Error("Authorization header sent despite empty token")
	}
}

func TestDoGET_SurfacesErrorBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad pat"}`))
	}))
	defer ts.Close()

	s := &server{coreURL: ts.URL, coreToken: "pat_bad", httpClient: ts.Client()}
	_, err := s.doGET(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("doGET succeeded, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "401") || !strings.Contains(msg, "bad pat") {
		t.Errorf("error %q missing status code or body", msg)
	}
}

func TestDoPOSTJSON_SendsJSONBody(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	var gotContentType, gotAuth string
	var gotBody payload
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"created"}`))
	}))
	defer ts.Close()

	s := &server{coreURL: ts.URL, coreToken: "pat_xyz", httpClient: ts.Client()}
	resp, err := s.doPOSTJSON(context.Background(), "/api/v1/things", payload{Name: "x", N: 7})
	if err != nil {
		t.Fatalf("doPOSTJSON: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotAuth != "Bearer pat_xyz" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Name != "x" || gotBody.N != 7 {
		t.Errorf("received body = %+v", gotBody)
	}
	if string(resp) != `{"id":"created"}` {
		t.Errorf("response = %s", string(resp))
	}
}

func TestHealthCheck_ReachableNoToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != healthProbePath {
			t.Errorf("probed %q, want %q", r.URL.Path, healthProbePath)
		}
		w.WriteHeader(http.StatusUnauthorized) // 401 still counts as reachable
	}))
	defer ts.Close()

	s := &server{coreURL: ts.URL, httpClient: ts.Client()}
	_, result, err := s.healthCheck(context.Background(), nil, healthCheckArgs{})
	if err != nil {
		t.Fatalf("healthCheck err: %v", err)
	}
	if !result.Reachable {
		t.Error("Reachable = false, want true for 401")
	}
	if result.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", result.StatusCode)
	}
	if result.TokenSet {
		t.Error("TokenSet = true with empty token")
	}
	if result.CoreURL != ts.URL {
		t.Errorf("CoreURL = %q", result.CoreURL)
	}
}

func TestHealthCheck_UnreachableServer(t *testing.T) {
	s := &server{coreURL: "http://127.0.0.1:1", httpClient: http.DefaultClient}
	_, result, err := s.healthCheck(context.Background(), nil, healthCheckArgs{})
	if err != nil {
		t.Fatalf("healthCheck err: %v", err)
	}
	if result.Reachable {
		t.Error("Reachable = true, want false for connection refused")
	}
	if result.Error == "" {
		t.Error("Error empty, want a transport error message")
	}
}
