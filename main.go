// Package main implements pacer-mcp, an MCP server that exposes pacer/core
// API endpoints as native Claude Code tools over stdio.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	CoreURL     string `json:"core_url"`
	TokenSet    bool   `json:"token_set"`
	Reachable   bool   `json:"reachable"`
	StatusCode  int    `json:"status_code,omitempty"`
	Error       string `json:"error,omitempty"`
	ServerVer   string `json:"server_version"`
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
	defer resp.Body.Close()

	result.Reachable = resp.StatusCode < 500
	result.StatusCode = resp.StatusCode
	return nil, result, nil
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

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

