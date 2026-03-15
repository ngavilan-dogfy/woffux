---
name: woffux
description: Check work status, clock in/out, manage telework/vacation requests, and view schedule via the woffux CLI. Trigger when the user asks about signing, clocking in/out, work mode (remote/office), telework, vacation, absences, work schedule, or Woffu.
argument-hint: "[status|sign|calendar|requests|events|schedule]"
allowed-tools: Bash, Read
---

You have access to the `woffux` CLI for Woffu time tracking. Use it to answer questions about the user's work status, schedule, and requests.

## Available commands

### Read-only (execute without asking)
- `woffux status --json` — Today's sign status, work mode (office/remote), coordinates
- `woffux today --json` — Detailed day info with sign slots
- `woffux events --json` — Available vacation days, hours pool, etc.
- `woffux requests --json` — User's submitted requests (telework, vacation, absences)
- `woffux calendar --json` — Monthly calendar with day types and events
- `woffux holidays --json` — Company holidays
- `woffux history --json` — Sign history (clock in/out records)
- `woffux schedule --json` — Auto-sign schedule configuration
- `woffux whoami --json` — User profile
- `woffux auto` — Auto-signing status (active/disabled)

### Write actions (ALWAYS confirm with user first)
- `woffux sign` — Clock in/out. Ask "Do you want me to sign now?" before executing.
- `woffux sign --force` — Sign even on non-working days. Double-confirm.
- `woffux request` — Interactive request creation (launches TUI, not suitable for non-interactive use)

## How to respond

1. Run the appropriate `--json` command via Bash
2. Parse the JSON output
3. Present the information **conversationally** in the user's language — never dump raw JSON
4. For status checks, be concise: "You're working from home today, signed in at 08:32"
5. If the user asks something ambiguous, combine multiple commands (e.g., status + events)

## Examples

User: "Have I signed today?"
→ Run `woffux status --json`, check sign status, reply: "Yes, you clocked in at 08:32. You're in office mode."

User: "How many vacation days do I have left?"
→ Run `woffux events --json`, find vacation entry, reply: "You have 12 vacation days available."

User: "Am I remote today?"
→ Run `woffux status --json`, check mode field, reply: "No, today you're in office."

User: "Sign me out"
→ Ask confirmation first, then run `woffux sign`

User: "What's my week looking like?"
→ Run `woffux calendar --json` and `woffux requests --json`, combine and summarize.

## $ARGUMENTS handling

If the user passes arguments directly:
- `/woffux` or `/woffux status` → Run status check
- `/woffux sign` → Confirm and sign
- `/woffux calendar` → Show calendar summary
- `/woffux events` → Show available events
- `/woffux requests` → Show pending requests
- `/woffux schedule` → Show auto-sign schedule

If no arguments: run `woffux status --json` and give a quick status summary.
