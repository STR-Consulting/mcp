// Package main implements pacer-mcp, an MCP server that exposes pacer/core
// API endpoints as native Claude Code tools over stdio.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version         = "0.0.1"
	defaultCoreURL  = "https://mc.pacerrev.io"
	coreURLEnvVar   = "PACER_CORE_URL"
	coreTokenEnvVar = "PACER_CORE_TOKEN"
	httpTimeout     = 30 * time.Second
	// Lightweight PAT-auth-protected endpoint used to verify connectivity.
	healthProbePath = "/api/v1/portfolios/briefable"
)

type server struct {
	coreURL    string
	coreToken  string
	httpClient *http.Client
}

func newServer() *server {
	coreURL := os.Getenv(coreURLEnvVar)
	if coreURL == "" {
		coreURL = defaultCoreURL
	}
	return &server{
		coreURL:    coreURL,
		coreToken:  os.Getenv(coreTokenEnvVar),
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

type healthCheckArgs struct{}

type healthCheckResult struct {
	CoreURL    string `json:"core_url"`
	TokenSet   bool   `json:"token_set"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	ServerVer  string `json:"server_version"`
}

func (s *server) healthCheck(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ healthCheckArgs,
) (*mcp.CallToolResult, healthCheckResult, error) {
	result := healthCheckResult{
		CoreURL:   s.coreURL,
		TokenSet:  s.coreToken != "",
		ServerVer: version,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+healthProbePath, nil)
	if err != nil {
		result.Error = err.Error()
		return nil, result, nil
	}
	if s.coreToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.coreToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		result.Error = err.Error()
		return nil, result, nil
	}
	defer func() { _ = resp.Body.Close() }()

	result.Reachable = resp.StatusCode < 500
	result.StatusCode = resp.StatusCode
	return nil, result, nil
}

// doGET performs an authenticated GET against the core API and returns the
// raw response body. Non-2xx responses are surfaced as errors with the body
// embedded so the caller can see core's error envelope.
func (s *server) doGET(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	u := s.coreURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if s.coreToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.coreToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("core API %d: %s", resp.StatusCode, string(body))
	}
	return json.RawMessage(body), nil
}

type guestyPricingConfigArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name or ID"`
}

func (s *server) guestyPricingConfig(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyPricingConfigArgs,
) (*mcp.CallToolResult, json.RawMessage, error) {
	if args.Portfolio == "" {
		return nil, nil, errors.New("portfolio is required")
	}
	path := "/api/v1/portfolios/" + url.PathEscape(args.Portfolio) + "/pricing-config"
	body, err := s.doGET(ctx, path, nil)
	if err != nil {
		return nil, nil, err
	}
	return nil, body, nil
}

type guestyReservationPromotionsArgs struct {
	Portfolio string `json:"portfolio" jsonschema:"portfolio name or ID"`
	Month     string `json:"month" jsonschema:"month in YYYY-MM format"`
	Flat      bool   `json:"flat,omitempty" jsonschema:"if true, return one row per reservation (aggregated); default false returns one row per promo line"`
}

func (s *server) guestyReservationPromotions(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	args guestyReservationPromotionsArgs,
) (*mcp.CallToolResult, json.RawMessage, error) {
	if args.Portfolio == "" {
		return nil, nil, errors.New("portfolio is required")
	}
	if args.Month == "" {
		return nil, nil, errors.New("month is required (YYYY-MM)")
	}
	q := url.Values{}
	q.Set("month", args.Month)
	if args.Flat {
		q.Set("flat", "true")
	}
	path := "/api/v1/portfolios/" + url.PathEscape(args.Portfolio) + "/reservation-promotions"
	body, err := s.doGET(ctx, path, q)
	if err != nil {
		return nil, nil, err
	}
	return nil, body, nil
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}

	s := newServer()

	impl := &mcp.Implementation{Name: "pacer-mcp", Version: version}
	srv := mcp.NewServer(impl, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "health_check",
		Description: "Check connectivity to the pacer/core API and report config status.",
	}, s.healthCheck)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_pricing_config",
		Description: "Get per-unit PMS pricing configuration for a portfolio: " +
			"base price, cleaning fee, weekend pricing, min/max nights, weekly/monthly " +
			"factors, extra-person fee, security deposit, attached promotion IDs, " +
			"per-channel settings, last-synced timestamp. Sourced from Guesty.",
	}, s.guestyPricingConfig)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "guesty_reservation_promotions",
		Description: "Get channel-applied promotions (Airbnb, Vrbo, etc.) for a " +
			"portfolio's reservations in a given month. By default returns one row " +
			"per (reservation × promo line); pass flat=true for one row per " +
			"reservation with aggregated promo_titles[] and total_discount.",
	}, s.guestyReservationPromotions)

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
