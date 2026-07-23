// Package main implements pacer-mcp, the original standalone MCP server that
// exposed pacer/core's API as stdio tools authenticated with a personal access
// token (PAT).
//
// It is RETIRED. Every tool now returns retirementMessage instead of reaching
// core. The server used to call the API over the network at
// `portal.pacerrev.io`; a restructure re-homed the API and `portal.pacerrev.io`
// now 303-redirects unauthenticated API calls to an HTML login page — which this
// binary stuffed into a json.RawMessage, producing the
// "invalid character '<' looking for beginning of value" errors that broke the
// integration/cred-management tools (jig 6dq-315).
//
// The replacement is the in-process ("inline") Pacer MCP connector built into
// core: it dispatches to the same /api/v1 handlers in-process with no network
// hop, so it can't fall out of sync with the API host, and it ships as the
// hosted "Pacer" connector on claude.ai / Claude Cowork. Point clients there and
// remove this standalone server from your MCP client config.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version carries a "retired" marker so a stale build can't masquerade as the
// live server.
const version = "1.0.0-retired"

// retirementMessage is returned by every tool. It must not name internal hosts
// beyond portal.pacerrev.io — this repo is public.
const retirementMessage = "This standalone pacer-mcp server (PAT over stdio) is " +
	"RETIRED and no longer reaches the Pacer API. Use the in-process Pacer MCP " +
	"connector built into core instead — it is offered as the hosted \"Pacer\" " +
	"connector on claude.ai / Claude Cowork (tools appear as mcp__…Pacer__…). " +
	"Remove this standalone server from your MCP client config."

const serverInstructions = `pacer-mcp is RETIRED.

This standalone PAT-over-stdio server no longer reaches the Pacer API. Every
tool returns a notice to switch to the in-process ("inline") Pacer MCP connector
built into core, offered as the hosted "Pacer" connector on claude.ai / Claude
Cowork. Do not rely on any tool here for live data, and remove this server from
your MCP client config.`

// retiredArgs is the empty argument set for every retired tool: no input is read.
type retiredArgs struct{}

// retired backs every advertised tool. It ignores its arguments and returns
// retirementMessage as an error result, so a client that keys off
// structuredContent sees the failure (and the redirect) rather than silently
// reading absent data as "zero rows".
func retired(
	_ context.Context,
	_ *mcp.CallToolRequest,
	_ retiredArgs,
) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		StructuredContent: map[string]any{
			"error": map[string]any{"message": retirementMessage},
		},
		Content: []mcp.Content{&mcp.TextContent{Text: retirementMessage}},
	}, nil, nil
}

// retiredTools is every tool this server historically advertised. They stay in
// tools/list — each wired to retired — so a stale client config resolves the
// tool and gets the redirect message instead of an "unknown tool" error.
var retiredTools = []string{
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

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}

	impl := &mcp.Implementation{Name: "pacer-mcp", Version: version}
	srv := mcp.NewServer(impl, &mcp.ServerOptions{Instructions: serverInstructions})
	registerTools(srv)

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// registerTools advertises every historical tool, each wired to the retired
// handler. Extracted from main so tests can drive it without a stdio transport.
func registerTools(srv *mcp.Server) {
	for _, name := range retiredTools {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        name,
			Description: "RETIRED. " + retirementMessage,
		}, retired)
	}
}
