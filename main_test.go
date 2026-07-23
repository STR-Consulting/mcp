package main

import (
	"context"
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

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	registerTools(srv)
}

// TestRegisterTools_ExpectedTools asserts every tool this server ever advertised
// is still registered. They remain in tools/list — each now returns the
// retirement notice — so a stale client config resolves the tool and gets the
// redirect message instead of an "unknown tool" error. Drives the MCP protocol
// via an in-memory transport so we exercise the same listTools path real clients
// use.
func TestRegisterTools_ExpectedTools(t *testing.T) {
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	registerTools(srv)

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
		"get_portfolio_billed_units",
		"get_billed_units_by_rm",
		"get_managed_units_by_rm",
		"list_portfolio_units",
		"list_portfolio_reservations",
		"list_portfolio_new_listings",
		"list_portfolio_integrations",
		"get_portfolio_integration_secrets",
		"set_portfolio_integration_secrets",
		"create_portfolio_integration",
		"get_portfolio_pacing",
		"get_portfolio_metrics_ytd",
		"get_portfolio_market_metrics",
		"guesty_pricing_config",
		"guesty_reservation_promotions",
		"get_pricelabs_notes",
		"get_pricelabs_tags",
		"get_pricelabs_overrides",
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

// TestEveryToolReturnsRetirementNotice is the contract for this retired server:
// no matter which tool a stale client calls, it gets an error result telling it
// to switch to the in-process Pacer connector — never a network call, an HTML
// login page, or silently-empty data.
func TestEveryToolReturnsRetirementNotice(t *testing.T) {
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	registerTools(srv)

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

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("no tools registered")
	}

	for _, tool := range listed.Tools {
		res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name})
		if err != nil {
			t.Fatalf("CallTool(%q): %v", tool.Name, err)
		}
		if !res.IsError {
			t.Errorf("CallTool(%q): IsError = false, want a retirement error", tool.Name)
		}
		var text strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text.WriteString(tc.Text)
			}
		}
		lower := strings.ToLower(text.String())
		if !strings.Contains(lower, "retired") ||
			!strings.Contains(lower, "in-process") {
			t.Errorf("CallTool(%q): message %q does not point to the in-process connector", tool.Name, text.String())
		}
	}
}

// TestVersionMarkedRetired guards against a build silently shipping as if it
// were the live server again.
func TestVersionMarkedRetired(t *testing.T) {
	if !strings.Contains(version, "retired") {
		t.Errorf("version = %q, want it to carry a 'retired' marker", version)
	}
	// Sanity: the public retirement copy must not leak internal hostnames beyond
	// portal.pacerrev.io (this repo is public).
	for _, host := range []string{"mc.pacerrev.io", "mc2.pacerrev.io"} {
		if strings.Contains(retirementMessage, host) || strings.Contains(serverInstructions, host) {
			t.Errorf("retirement copy leaks internal host %q into the public repo", host)
		}
	}
}
