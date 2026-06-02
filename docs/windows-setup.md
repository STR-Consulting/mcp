# Windows setup

This page is written to be **pasted directly into an AI assistant** (Claude Code, Claude Desktop, Cursor, etc.) running on Windows. The AI will execute the PowerShell commands and walk you through it.

Before you start, ask whoever sent you this for a **Pacer access token** — it looks like `pat_xxxxxxxx`. Don't proceed without one.

---

## Paste the block below into your AI assistant

> I want to install `pacer-mcp` on my Windows machine and wire it up as an MCP server for Claude Code (or Claude Desktop). I'm not a programmer — please run the commands for me in PowerShell and tell me what to do at each step.
>
> ### 1. Install via Scoop
>
> If Scoop isn't installed yet, install it first:
>
> ```powershell
> Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
> irm get.scoop.sh | iex
> ```
>
> Then add the STR-Consulting bucket and install:
>
> ```powershell
> scoop bucket add str-consulting https://github.com/STR-Consulting/scoop-bucket
> scoop install pacer-mcp
> ```
>
> Verify it's on PATH:
>
> ```powershell
> pacer-mcp --version
> ```
>
> ### 2. Pacer access token
>
> I have a token from the Pacer team that starts with `pat_`. Ask me for it before continuing — don't make one up.
>
> ### 3. Wire it into Claude
>
> - **Claude Code (CLI):** edit `%USERPROFILE%\.claude.json` (or run `claude mcp add`).
> - **Claude Desktop:** edit `%APPDATA%\Claude\claude_desktop_config.json`.
>
> Add this under `mcpServers` — **merge** with anything already there, don't overwrite the file:
>
> ```json
> {
>   "mcpServers": {
>     "pacer": {
>       "command": "pacer-mcp",
>       "env": {
>         "PACER_CORE_URL": "https://mc.pacerrev.io",
>         "PACER_CORE_TOKEN": "pat_REPLACE_ME"
>       }
>     }
>   }
> }
> ```
>
> ### 4. Restart Claude and verify
>
> Fully quit Claude (check the system tray), reopen it, and ask it to run the `health_check` tool. If it returns OK, we're done. If not, show me the error.
>
> Tools available once connected:
>
> - `health_check`
> - `guesty_pricing_config`
> - `guesty_reservation_promotions`

---

## Updating later

```powershell
scoop update pacer-mcp
```

## Troubleshooting

- **`pacer-mcp` not found after install** — open a new PowerShell window so it picks up the updated PATH.
- **`health_check` returns 401** — the `PACER_CORE_TOKEN` is wrong or expired; ask for a new PAT.
- **Tools don't show up in Claude** — make sure Claude was fully quit (system tray) before reopening; config is only read at startup.
