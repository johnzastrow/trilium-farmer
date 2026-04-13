# trilium-farmer — Design Spec

**Date:** 2026-04-13
**Status:** Approved
**Repo:** https://github.com/johnzastrow/trilium-farmer

---

## Overview

`trilium-farmer` is a Go MCP server that gives Claude Code read/write access to a Trilium Notes instance via the ETAPI. Claude can navigate the note tree conversationally, search notes, read content, and create or update notes under the right parent.

This fills the same role as MemPalace but uses Trilium — the user's existing PKM system — as the backing store. Notes are accessible across all machines because Trilium handles its own sync.

---

## Architecture

**Pattern:** Local binary, stdio MCP transport. Claude Code spawns the binary as a subprocess; communication is over stdin/stdout. No persistent process required.

```
Claude Code → spawns (stdio) → trilium-farmer binary → HTTP → Trilium ETAPI (192.168.1.102:8080)
```

**Language:** Go

**Dependencies:**
- `github.com/mark3labs/mcp-go` — MCP protocol handling (stdio transport, tool registration)
- `net/http` (stdlib) — all Trilium ETAPI calls

**File structure:**
```
trilium-farmer/
├── main.go         — MCP server setup, tool registration, tool handlers
├── client.go       — Trilium ETAPI HTTP client (pure HTTP, no MCP concerns)
├── go.mod
└── README.md
```

---

## Privacy & Access Control

Trilium notes often contain sensitive personal information. All note content retrieved by the MCP server is passed to Claude's context and through the Anthropic API. To limit exposure:

### `#private` Label Filtering

Any note carrying a `#private` label — or descended from a note that carries `#private` — is silently excluded from all tool responses. This applies to:

- `list_root_notes` — `#private` notes omitted from root listing
- `get_children` — `#private` children omitted
- `get_note` — returns an error if the note is `#private`
- `search_notes` — `#private` notes omitted from results
- `create_note` / `update_note` — blocked if target parent is `#private`

**How to use:** In Trilium, add the label `#private` to any note or subtree you want Claude to never see. Labels inherit through children, so tagging a root branch protects the entire subtree.

**Implementation:** Before returning any note, the server calls `GET /etapi/notes/{noteId}` and checks the `attributes` array for a label named `private`. If found, the note is excluded.

### `TRILIUM_ALLOWED_ROOTS` Allowlist

An optional env var that restricts which top-level notes Claude can navigate into. When set, `list_root_notes` returns only the specified note IDs. This is a coarse root-level gate — it prevents Claude from ever seeing or traversing into excluded top-level branches.

```
TRILIUM_ALLOWED_ROOTS=noteId1,noteId2
```

**Scope:** Applies to `list_root_notes` only. `get_children` on non-root notes is not restricted by the allowlist — the assumption is that if Claude navigated to a note, it was via an allowed root. `search_notes` is also not allowlist-filtered (use `#private` on any notes you want excluded from search).

**When unset:** All root notes are visible (subject to `#private` filtering).

**How the two layers interact:**

| Scenario | Result |
|---|---|
| Note in allowed root, not `#private` | Visible |
| Note in allowed root, tagged `#private` | Hidden |
| Note in non-allowed root | Hidden from navigation (not in `list_root_notes`) |
| Note in non-allowed root, returned by search | Visible in search (use `#private` to also hide from search) |

### Lazy MemPalace Bridging

No bulk sync between Trilium and MemPalace. Instead:
- When Claude reads a Trilium note worth remembering across sessions, it calls `mempalace_add_drawer` in the same turn.
- When Claude saves something to MemPalace that belongs in your permanent PKM, it calls `create_note` in Trilium.

Each system stays clean. The bridge is intentional, not automatic.

---

## Configuration

**Environment variables:**

| Variable | Example | Required |
|---|---|---|
| `TRILIUM_URL` | `http://192.168.1.102:8080` | Yes |
| `TRILIUM_TOKEN` | `<etapi token>` | Yes |
| `TRILIUM_ALLOWED_ROOTS` | `abc123,def456` | No — if unset, all roots visible |

Generate the ETAPI token in Trilium: **Options → API tokens → Create token**.
One token works from all machines since they all hit the same Trilium instance.

**Claude Code registration** — add to `~/.claude/settings.local.json`:

```json
{
  "mcpServers": {
    "trilium": {
      "command": "trilium-farmer",
      "env": {
        "TRILIUM_URL": "http://192.168.1.102:8080",
        "TRILIUM_TOKEN": "your-token-here"
      }
    }
  }
}
```

**Installation** (each machine, one command):
```bash
go install github.com/johnzastrow/trilium-farmer@latest
```

---

## MCP Tools

Six tools total. Each tool maps to one or two Trilium ETAPI calls.

### `list_root_notes`
- **Purpose:** Entry point for tree navigation. Returns direct children of root.
- **ETAPI:** `GET /etapi/notes/root/children`
- **Returns:** Array of `{noteId, title, type}` objects

### `get_children`
- **Purpose:** Drill into a branch of the tree.
- **Input:** `noteId` (string)
- **ETAPI:** `GET /etapi/notes/{noteId}/children`
- **Returns:** Array of `{noteId, title, type}` objects

### `get_note`
- **Purpose:** Read a note's title and full content.
- **Input:** `noteId` (string)
- **ETAPI:** `GET /etapi/notes/{noteId}` + `GET /etapi/notes/{noteId}/content`
- **Returns:** `{noteId, title, type, content}`

### `search_notes`
- **Purpose:** Full-text search across all notes.
- **Input:** `query` (string)
- **ETAPI:** `GET /etapi/notes?search={query}`
- **Returns:** Array of `{noteId, title, type}` — up to 20 results

### `create_note`
- **Purpose:** Create a new child note under a specified parent.
- **Input:** `parentNoteId` (string), `title` (string), `content` (string), `type` (string, default: `"markdown"`)
- **ETAPI:** `POST /etapi/create-note`
- **Returns:** `{noteId, title}` of the newly created note
- **Duplicate handling:** See below.

### `update_note`
- **Purpose:** Overwrite an existing note's content.
- **Input:** `noteId` (string), `content` (string)
- **ETAPI:** `PUT /etapi/notes/{noteId}/content`
- **Returns:** Success confirmation

---

## Tree Navigation Pattern

Claude navigates the tree interactively before creating a note:

1. Call `list_root_notes` — present top-level branches to user
2. User identifies the relevant branch
3. Call `get_children` on that branch — present sub-notes
4. Repeat until the right parent is found or user says to create here
5. Call `create_note` with the confirmed parent

**Example interaction:**
```
Claude: I see these top-level notes: Programming, Home, Work, Journal.
        Which branch fits "Go HTTP clients"?
User:   Programming
Claude: Under Programming I see: Go, Python, JavaScript.
        Under Go?
User:   Yes
Claude: Under Go I see: Patterns, Libraries, Snippets.
        Under Libraries?
User:   Yes, create it there.
Claude: [calls create_note(parentId=<Libraries id>, title="Go HTTP clients", content="...")]
```

---

## Duplicate Handling

Trilium does not enforce unique note titles. To prevent silent duplicates:

- Before calling `create_note`, Claude checks the already-retrieved children list for a title match (case-insensitive).
- If a match is found: Claude surfaces it — *"A note called 'X' already exists here. Update it or create a new one?"*
- If update: call `update_note` with the existing note's ID.
- If create anyway: proceed with `create_note`.

This check uses data already in context from the navigation step — no extra API call needed.

---

## Error Handling

Three failure modes with explicit messages:

| Condition | ETAPI response | Message to Claude |
|---|---|---|
| Trilium unreachable | connection refused / timeout | `Cannot reach Trilium at <TRILIUM_URL> — is it running?` |
| Invalid token | `401 Unauthorized` | `TRILIUM_TOKEN is invalid or expired — generate a new one in Trilium Options → API tokens` |
| Note not found | `404 Not Found` | `Note <noteId> not found` |

All other errors return the raw error message. No over-engineering for a personal tool.

---

## Out of Scope

- Note deletion (destructive — omitted intentionally)
- Moving notes between parents
- Managing note attributes or relations (beyond reading `#private`)
- Multi-instance Trilium support
- Note sharing or publishing
- Bulk Trilium → MemPalace ingestion
