# trilium-farmer

An MCP server that gives Claude Code read/write access to [Trilium Notes](https://github.com/zadam/trilium) via the ETAPI.

Claude can navigate your note tree, search notes, read content, and create new notes under the right parent — all through natural conversation.

## Installation

```bash
go install github.com/johnzastrow/trilium-farmer@latest
```

## Configuration

Add to your Claude Code MCP settings:

```json
{
  "mcpServers": {
    "trilium": {
      "command": "trilium-farmer",
      "env": {
        "TRILIUM_URL": "http://192.168.1.102:8080",
        "TRILIUM_TOKEN": "your-etapi-token-here"
      }
    }
  }
}
```

Generate your ETAPI token in Trilium: **Options → API tokens → Create token**

## Tools

| Tool | Description |
|---|---|
| `list_root_notes` | List top-level notes |
| `get_children` | Get children of a note (for tree navigation) |
| `get_note` | Read a note's title and content |
| `search_notes` | Full-text search across all notes |
| `create_note` | Create a new child note under a parent |
| `update_note` | Update an existing note's content |

## Transport

stdio — Claude Code spawns the binary as a subprocess. No persistent process required.
