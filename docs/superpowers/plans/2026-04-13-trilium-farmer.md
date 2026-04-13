# trilium-farmer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go MCP server that gives Claude Code read/write access to Trilium Notes via ETAPI, with `#private` label filtering and tree navigation support.

**Architecture:** stdio MCP binary wrapping Trilium's ETAPI REST interface. `client.go` handles all HTTP communication; `main.go` registers MCP tools and wires them to the client. Each tool call maps to one or two ETAPI HTTP requests.

**Tech Stack:** Go 1.21+, `github.com/mark3labs/mcp-go`, stdlib `net/http`, `net/http/httptest` (tests)

---

## Pre-flight: Verify ETAPI Endpoints

Before writing any code, open `http://192.168.1.102:8080/etapi-docs` in a browser and confirm:

1. **Children endpoint** — expected `GET /etapi/notes/{noteId}/children`; verify it exists and returns an array of note objects
2. **Note object shape** — confirm `attributes` field is present and contains `{type, name, value}` objects
3. **Note type values** — confirm `"markdown"` is a valid type (may be `"text"` — check the create-note schema)
4. **Search endpoint** — confirm `GET /etapi/notes?search=query` exists and the query parameter name

Note any discrepancies and adjust the code in the relevant task.

---

## File Map

```
trilium-farmer/
├── go.mod              — module definition and dependencies
├── go.sum              — dependency checksums
├── client.go           — Trilium ETAPI HTTP client, all types, privacy filter
├── client_test.go      — unit tests for all client methods (uses httptest)
├── main.go             — MCP server setup, tool registration, handlers
└── docs/
    └── superpowers/
        ├── specs/2026-04-13-trilium-farmer-design.md  (exists)
        └── plans/2026-04-13-trilium-farmer.md         (this file)
```

---

### Task 1: Initialize Go module

**Files:**
- Create: `go.mod`, `go.sum`

- [ ] **Step 1: Initialize module**

```bash
cd /home/jcz/Github/trilium-farmer
go mod init github.com/johnzastrow/trilium-farmer
```

Expected: `go.mod` created with `module github.com/johnzastrow/trilium-farmer` and `go 1.21` (or current version)

- [ ] **Step 2: Add mcp-go dependency**

```bash
go get github.com/mark3labs/mcp-go@latest
```

Expected: `go.mod` and `go.sum` updated with mcp-go and its transitive dependencies

- [ ] **Step 3: Tidy and verify**

```bash
go mod tidy
cat go.mod
```

Expected: `require` block contains `github.com/mark3labs/mcp-go`

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: initialize Go module with mcp-go dependency"
git push
```

---

### Task 2: Core types and HTTP client

**Files:**
- Create: `client.go`
- Create: `client_test.go`

- [ ] **Step 1: Write failing tests for NewClient**

Create `client_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestNewClient_setsFields(t *testing.T) {
	c := NewClient("http://localhost:8080", "test-token")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL http://localhost:8080, got %s", c.baseURL)
	}
	if c.token != "test-token" {
		t.Errorf("expected token test-token, got %s", c.token)
	}
}

func TestNewClient_trailingSlash(t *testing.T) {
	c := NewClient("http://localhost:8080/", "token")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected trailing slash stripped, got %s", c.baseURL)
	}
}

func TestDo_connectionRefused(t *testing.T) {
	c := NewClient("http://localhost:19999", "token") // nothing running here
	_, err := c.do("GET", "/etapi/notes/root", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "cannot reach Trilium") {
		t.Errorf("expected friendly error, got: %s", err.Error())
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

```bash
cd /home/jcz/Github/trilium-farmer
go test ./... 2>&1 | head -20
```

Expected: compile error — `NewClient` and `do` undefined

- [ ] **Step 3: Create client.go with types and base client**

Create `client.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Note is a lightweight note reference returned by list and search operations.
type Note struct {
	NoteID string `json:"noteId"`
	Title  string `json:"title"`
	Type   string `json:"type"`
}

// NoteDetail is a full note with content and attributes.
type NoteDetail struct {
	NoteID     string      `json:"noteId"`
	Title      string      `json:"title"`
	Type       string      `json:"type"`
	Attributes []Attribute `json:"attributes"`
	Content    string      // fetched via a separate /content call
}

// Attribute is a Trilium note attribute (label or relation).
type Attribute struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CreateNoteRequest is the request body for POST /etapi/create-note.
type CreateNoteRequest struct {
	ParentNoteID string `json:"parentNoteId"`
	Title        string `json:"title"`
	Type         string `json:"type"`
	Content      string `json:"content"`
}

// createNoteResponse is the response body from POST /etapi/create-note.
type createNoteResponse struct {
	Note Note `json:"note"`
}

// Client is an HTTP client for the Trilium ETAPI.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a new Trilium ETAPI client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// do executes an authenticated HTTP request against the ETAPI.
func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach Trilium at %s — is it running?", c.baseURL)
	}
	return resp, nil
}

// checkStatus converts ETAPI HTTP error codes to friendly messages.
func (c *Client) checkStatus(resp *http.Response, noteID string) error {
	switch resp.StatusCode {
	case 200, 201, 204:
		return nil
	case 401:
		return fmt.Errorf("TRILIUM_TOKEN is invalid or expired — generate a new one in Trilium Options → API tokens")
	case 404:
		return fmt.Errorf("note %s not found", noteID)
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ETAPI error %d: %s", resp.StatusCode, string(body))
	}
}

// decodeJSON decodes a JSON response body into v.
func decodeJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// Ensure url is imported (used in SearchNotes).
var _ = url.QueryEscape
```

- [ ] **Step 4: Run tests**

```bash
go test ./... -v -run "TestNewClient|TestDo"
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: add Trilium ETAPI client with types and error handling"
git push
```

---

### Task 3: Privacy filter

**Files:**
- Modify: `client.go`
- Modify: `client_test.go`

- [ ] **Step 1: Write failing tests**

Add to `client_test.go`:

```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func TestIsPrivate_labeled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"noteId":"abc","title":"Secret","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	priv, err := c.isPrivate("abc")
	if err != nil {
		t.Fatal(err)
	}
	if !priv {
		t.Error("expected note with #private label to return true")
	}
}

func TestIsPrivate_unlabeled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"noteId":"xyz","title":"Public","type":"text","attributes":[]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	priv, err := c.isPrivate("xyz")
	if err != nil {
		t.Fatal(err)
	}
	if priv {
		t.Error("expected note without #private label to return false")
	}
}
```

**Note:** The `import` block in `client_test.go` should be merged — Go requires one import block per file. Merge all imports at the top of `client_test.go`.

- [ ] **Step 2: Run to verify failure**

```bash
go test ./... -v -run TestIsPrivate 2>&1 | head -20
```

Expected: compile error — `isPrivate` undefined

- [ ] **Step 3: Implement isPrivate in client.go**

Add after `decodeJSON` in `client.go`:

```go
// isPrivate returns true if the note has a #private label.
func (c *Client) isPrivate(noteID string) (bool, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID, nil)
	if err != nil {
		return false, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
		resp.Body.Close()
		return false, err
	}
	var note NoteDetail
	if err := decodeJSON(resp, &note); err != nil {
		return false, err
	}
	for _, attr := range note.Attributes {
		if attr.Type == "label" && attr.Name == "private" {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./... -v -run TestIsPrivate
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: implement #private label filter"
git push
```

---

### Task 4: GetChildren and ListRootNotes

**Files:**
- Modify: `client.go`
- Modify: `client_test.go`

- [ ] **Step 1: Write failing test**

Add to `client_test.go`:

```go
func TestGetChildren_filtersPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/etapi/notes/parent1/children":
			fmt.Fprintln(w, `[{"noteId":"pub1","title":"Public Note","type":"text"},{"noteId":"priv1","title":"Secret","type":"text"}]`)
		case "/etapi/notes/pub1":
			fmt.Fprintln(w, `{"noteId":"pub1","title":"Public Note","type":"text","attributes":[]}`)
		case "/etapi/notes/priv1":
			fmt.Fprintln(w, `{"noteId":"priv1","title":"Secret","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	notes, err := c.GetChildren("parent1")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("expected 1 visible note (private filtered), got %d", len(notes))
	}
	if notes[0].NoteID != "pub1" {
		t.Errorf("expected pub1, got %s", notes[0].NoteID)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./... -v -run TestGetChildren 2>&1 | head -20
```

Expected: compile error — `GetChildren` undefined

- [ ] **Step 3: Implement GetChildren and ListRootNotes in client.go**

Add to `client.go`:

```go
// GetChildren returns the visible (non-private) child notes of noteID.
func (c *Client) GetChildren(noteID string) ([]Note, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID+"/children", nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
		resp.Body.Close()
		return nil, err
	}
	var notes []Note
	if err := decodeJSON(resp, &notes); err != nil {
		return nil, err
	}
	var visible []Note
	for _, n := range notes {
		priv, err := c.isPrivate(n.NoteID)
		if err != nil {
			continue // skip notes we can't check
		}
		if !priv {
			visible = append(visible, n)
		}
	}
	return visible, nil
}

// ListRootNotes returns the visible top-level notes (children of root).
func (c *Client) ListRootNotes() ([]Note, error) {
	return c.GetChildren("root")
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: implement GetChildren and ListRootNotes"
git push
```

---

### Task 5: GetNote

**Files:**
- Modify: `client.go`
- Modify: `client_test.go`

- [ ] **Step 1: Write failing tests**

Add to `client_test.go`:

```go
func TestGetNote_returnsContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/etapi/notes/note1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"noteId":"note1","title":"My Note","type":"text","attributes":[]}`)
		case "/etapi/notes/note1/content":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "# My Note\n\nSome content.")
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	note, err := c.GetNote("note1")
	if err != nil {
		t.Fatal(err)
	}
	if note.Title != "My Note" {
		t.Errorf("expected title 'My Note', got '%s'", note.Title)
	}
	if note.Content != "# My Note\n\nSome content." {
		t.Errorf("unexpected content: %q", note.Content)
	}
}

func TestGetNote_blocksPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"noteId":"priv1","title":"Secret","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	_, err := c.GetNote("priv1")
	if err == nil {
		t.Fatal("expected error for private note")
	}
	if !strings.Contains(err.Error(), "private") {
		t.Errorf("expected privacy error, got: %s", err.Error())
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./... -v -run TestGetNote 2>&1 | head -20
```

Expected: compile error — `GetNote` undefined

- [ ] **Step 3: Implement GetNote in client.go**

Add to `client.go`:

```go
// GetNote returns a note's metadata and full content.
// Returns an error if the note has the #private label.
func (c *Client) GetNote(noteID string) (*NoteDetail, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID, nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
		resp.Body.Close()
		return nil, err
	}
	var note NoteDetail
	if err := decodeJSON(resp, &note); err != nil {
		return nil, err
	}
	for _, attr := range note.Attributes {
		if attr.Type == "label" && attr.Name == "private" {
			return nil, fmt.Errorf("note %s is marked #private and cannot be accessed", noteID)
		}
	}
	contentResp, err := c.do("GET", "/etapi/notes/"+noteID+"/content", nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(contentResp, noteID); err != nil {
		contentResp.Body.Close()
		return nil, err
	}
	defer contentResp.Body.Close()
	content, err := io.ReadAll(contentResp.Body)
	if err != nil {
		return nil, err
	}
	note.Content = string(content)
	return &note, nil
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: implement GetNote with content and privacy guard"
git push
```

---

### Task 6: SearchNotes

**Files:**
- Modify: `client.go`
- Modify: `client_test.go`

- [ ] **Step 1: Write failing test**

Add to `client_test.go`:

```go
func TestSearchNotes_filtersPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/etapi/notes":
			fmt.Fprintln(w, `[{"noteId":"pub2","title":"Go Tips","type":"text"},{"noteId":"priv2","title":"Passwords","type":"text"}]`)
		case "/etapi/notes/pub2":
			fmt.Fprintln(w, `{"noteId":"pub2","title":"Go Tips","type":"text","attributes":[]}`)
		case "/etapi/notes/priv2":
			fmt.Fprintln(w, `{"noteId":"priv2","title":"Passwords","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.SearchNotes("golang")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (private filtered), got %d", len(results))
	}
	if results[0].NoteID != "pub2" {
		t.Errorf("expected pub2, got %s", results[0].NoteID)
	}
}
```

- [ ] **Step 2: Run to verify failure**

```bash
go test ./... -v -run TestSearchNotes 2>&1 | head -20
```

Expected: compile error — `SearchNotes` undefined

- [ ] **Step 3: Implement SearchNotes in client.go**

Add to `client.go`:

```go
// SearchNotes performs a full-text search and returns up to 20 visible results.
func (c *Client) SearchNotes(query string) ([]Note, error) {
	resp, err := c.do("GET", "/etapi/notes?search="+url.QueryEscape(query)+"&limit=20", nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, ""); err != nil {
		resp.Body.Close()
		return nil, err
	}
	var notes []Note
	if err := decodeJSON(resp, &notes); err != nil {
		return nil, err
	}
	var visible []Note
	for _, n := range notes {
		priv, err := c.isPrivate(n.NoteID)
		if err != nil || priv {
			continue
		}
		visible = append(visible, n)
	}
	return visible, nil
}
```

Remove the `var _ = url.QueryEscape` placeholder line added in Task 2 since `url` is now genuinely used.

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: implement SearchNotes with privacy filtering"
git push
```

---

### Task 7: CreateNote and UpdateNote

**Files:**
- Modify: `client.go`
- Modify: `client_test.go`

- [ ] **Step 1: Write failing tests**

Add to `client_test.go`:

```go
func TestCreateNote_success(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/etapi/notes/parent1":
			fmt.Fprintln(w, `{"noteId":"parent1","title":"Parent","type":"text","attributes":[]}`)
		case "/etapi/create-note":
			receivedBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintln(w, `{"note":{"noteId":"new1","title":"New Note","type":"markdown"}}`)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	note, err := c.CreateNote("parent1", "New Note", "# New Note\n\nContent.", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if note.NoteID != "new1" {
		t.Errorf("expected noteId new1, got %s", note.NoteID)
	}
	var req CreateNoteRequest
	if err := json.Unmarshal(receivedBody, &req); err != nil {
		t.Fatal(err)
	}
	if req.ParentNoteID != "parent1" {
		t.Errorf("expected parentNoteId parent1, got %s", req.ParentNoteID)
	}
	if req.Type != "markdown" {
		t.Errorf("expected type markdown, got %s", req.Type)
	}
}

func TestCreateNote_blocksPrivateParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"noteId":"priv1","title":"Secret","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	_, err := c.CreateNote("priv1", "New Note", "content", "markdown")
	if err == nil {
		t.Fatal("expected error when creating under a private parent")
	}
}

func TestUpdateNote_sendsContent(t *testing.T) {
	var receivedContent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/etapi/notes/note1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"noteId":"note1","title":"Note","type":"text","attributes":[]}`)
		case "/etapi/notes/note1/content":
			receivedContent, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	if err := c.UpdateNote("note1", "Updated content."); err != nil {
		t.Fatal(err)
	}
	if string(receivedContent) != "Updated content." {
		t.Errorf("unexpected content sent: %q", string(receivedContent))
	}
}
```

Add `"encoding/json"` and `"io"` to the import block in `client_test.go`.

- [ ] **Step 2: Run to verify failure**

```bash
go test ./... -v -run "TestCreateNote|TestUpdateNote" 2>&1 | head -20
```

Expected: compile error — `CreateNote` and `UpdateNote` undefined

- [ ] **Step 3: Implement CreateNote and UpdateNote in client.go**

Add to `client.go`:

```go
// CreateNote creates a child note under parentNoteID.
// Returns an error if the parent is marked #private.
func (c *Client) CreateNote(parentNoteID, title, content, noteType string) (*Note, error) {
	priv, err := c.isPrivate(parentNoteID)
	if err != nil {
		return nil, err
	}
	if priv {
		return nil, fmt.Errorf("cannot create note under %s — parent is marked #private", parentNoteID)
	}
	body := CreateNoteRequest{
		ParentNoteID: parentNoteID,
		Title:        title,
		Type:         noteType,
		Content:      content,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.do("POST", "/etapi/create-note", strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, parentNoteID); err != nil {
		resp.Body.Close()
		return nil, err
	}
	var result createNoteResponse
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result.Note, nil
}

// UpdateNote overwrites the content of an existing note.
// Returns an error if the note is marked #private.
func (c *Client) UpdateNote(noteID, content string) error {
	priv, err := c.isPrivate(noteID)
	if err != nil {
		return err
	}
	if priv {
		return fmt.Errorf("cannot update note %s — it is marked #private", noteID)
	}
	resp, err := c.do("PUT", "/etapi/notes/"+noteID+"/content", strings.NewReader(content))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.checkStatus(resp, noteID)
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat: implement CreateNote and UpdateNote with privacy guards"
git push
```

---

### Task 8: MCP server, tool registration, and handlers

**Files:**
- Create: `main.go`

- [ ] **Step 1: Create main.go**

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	triliumURL := os.Getenv("TRILIUM_URL")
	if triliumURL == "" {
		log.Fatal("TRILIUM_URL environment variable is required")
	}
	token := os.Getenv("TRILIUM_TOKEN")
	if token == "" {
		log.Fatal("TRILIUM_TOKEN environment variable is required")
	}

	c := NewClient(triliumURL, token)

	// Parse optional root allowlist: TRILIUM_ALLOWED_ROOTS=noteId1,noteId2
	var allowedRoots map[string]bool
	if roots := os.Getenv("TRILIUM_ALLOWED_ROOTS"); roots != "" {
		allowedRoots = make(map[string]bool)
		for _, id := range strings.Split(roots, ",") {
			if id = strings.TrimSpace(id); id != "" {
				allowedRoots[id] = true
			}
		}
	}

	s := server.NewMCPServer("trilium-farmer", "1.0.0")

	s.AddTool(mcp.NewTool("list_root_notes",
		mcp.WithDescription("List the top-level notes in Trilium. Use this as the starting point for tree navigation before creating a note. Notes marked #private are excluded."),
	), listRootNotesHandler(c, allowedRoots))

	s.AddTool(mcp.NewTool("get_children",
		mcp.WithDescription("Get the child notes of a specific note. Use this to drill down the tree to find the right parent before creating a note. Notes marked #private are excluded."),
		mcp.WithString("noteId",
			mcp.Required(),
			mcp.Description("The ID of the parent note whose children to list"),
		),
	), getChildrenHandler(c))

	s.AddTool(mcp.NewTool("get_note",
		mcp.WithDescription("Read a note's title and full content. Returns an error if the note is marked #private."),
		mcp.WithString("noteId",
			mcp.Required(),
			mcp.Description("The ID of the note to read"),
		),
	), getNoteHandler(c))

	s.AddTool(mcp.NewTool("search_notes",
		mcp.WithDescription("Search all notes by text. Returns up to 20 results. Notes marked #private are excluded."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("The search query"),
		),
	), searchNotesHandler(c))

	s.AddTool(mcp.NewTool("create_note",
		mcp.WithDescription("Create a new child note under a parent note. IMPORTANT: Before calling this, use list_root_notes and get_children to navigate the tree and confirm the right parent. Check if a note with the same title already exists among the parent's children — if so, offer to update it with update_note instead of creating a duplicate."),
		mcp.WithString("parentNoteId",
			mcp.Required(),
			mcp.Description("The ID of the parent note"),
		),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description("The title of the new note"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("The markdown content of the new note"),
		),
		mcp.WithString("type",
			mcp.Description("Note type: 'markdown', 'text', or 'code'. Defaults to 'markdown'"),
		),
	), createNoteHandler(c))

	s.AddTool(mcp.NewTool("update_note",
		mcp.WithDescription("Update the content of an existing note. Replaces all existing content. Returns an error if the note is marked #private."),
		mcp.WithString("noteId",
			mcp.Required(),
			mcp.Description("The ID of the note to update"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("The new content for the note (replaces existing content entirely)"),
		),
	), updateNoteHandler(c))

	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}

func listRootNotesHandler(c *Client, allowedRoots map[string]bool) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		notes, err := c.ListRootNotes()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(allowedRoots) > 0 {
			var filtered []Note
			for _, n := range notes {
				if allowedRoots[n.NoteID] {
					filtered = append(filtered, n)
				}
			}
			notes = filtered
		}
		return mcp.NewToolResultText(formatNoteList(notes)), nil
	}
}

func getChildrenHandler(c *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		noteID, ok := req.Params.Arguments["noteId"].(string)
		if !ok || noteID == "" {
			return mcp.NewToolResultError("noteId is required"), nil
		}
		notes, err := c.GetChildren(noteID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(formatNoteList(notes)), nil
	}
}

func getNoteHandler(c *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		noteID, ok := req.Params.Arguments["noteId"].(string)
		if !ok || noteID == "" {
			return mcp.NewToolResultError("noteId is required"), nil
		}
		note, err := c.GetNote(noteID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result := fmt.Sprintf("# %s\nID: %s | Type: %s\n\n%s", note.Title, note.NoteID, note.Type, note.Content)
		return mcp.NewToolResultText(result), nil
	}
}

func searchNotesHandler(c *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, ok := req.Params.Arguments["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}
		notes, err := c.SearchNotes(query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(notes) == 0 {
			return mcp.NewToolResultText("No notes found for: " + query), nil
		}
		return mcp.NewToolResultText(formatNoteList(notes)), nil
	}
}

func createNoteHandler(c *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		parentNoteID, ok := req.Params.Arguments["parentNoteId"].(string)
		if !ok || parentNoteID == "" {
			return mcp.NewToolResultError("parentNoteId is required"), nil
		}
		title, ok := req.Params.Arguments["title"].(string)
		if !ok || title == "" {
			return mcp.NewToolResultError("title is required"), nil
		}
		content, _ := req.Params.Arguments["content"].(string)
		noteType, ok := req.Params.Arguments["type"].(string)
		if !ok || noteType == "" {
			noteType = "markdown"
		}
		note, err := c.CreateNote(parentNoteID, title, content, noteType)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Created note '%s' (ID: %s)", note.Title, note.NoteID)), nil
	}
}

func updateNoteHandler(c *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		noteID, ok := req.Params.Arguments["noteId"].(string)
		if !ok || noteID == "" {
			return mcp.NewToolResultError("noteId is required"), nil
		}
		content, ok := req.Params.Arguments["content"].(string)
		if !ok {
			return mcp.NewToolResultError("content is required"), nil
		}
		if err := c.UpdateNote(noteID, content); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Updated note %s", noteID)), nil
	}
}

// formatNoteList renders a slice of notes as a readable list with IDs.
func formatNoteList(notes []Note) string {
	if len(notes) == 0 {
		return "No notes found."
	}
	var sb strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&sb, "[%s] %s (%s)\n", n.NoteID, n.Title, n.Type)
	}
	return strings.TrimRight(sb.String(), "\n")
}
```

- [ ] **Step 2: Build to verify compilation**

```bash
go build ./...
```

If the mcp-go API differs slightly from the code above (e.g., `mcp.Description` vs `mcp.WithDescription` for property options, or `server.ToolHandlerFunc` not exported), check the installed library source:

```bash
cat $(go env GOPATH)/pkg/mod/github.com/mark3labs/mcp-go@*/server/*.go | grep -A3 "ToolHandler"
```

Adjust signatures to match the actual library API.

- [ ] **Step 3: Run all tests**

```bash
go test ./... -v
```

Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: add MCP server with all 6 tools and handlers"
git push
```

---

### Task 9: Install binary and register with Claude Code

**Files:**
- Modify: `~/.claude/settings.local.json`

- [ ] **Step 1: Tag the release**

```bash
cd /home/jcz/Github/trilium-farmer
git tag v1.0.0
git push origin v1.0.0
```

- [ ] **Step 2: Install the binary**

```bash
go install github.com/johnzastrow/trilium-farmer@v1.0.0
```

Verify:
```bash
which trilium-farmer
```

Expected: path like `/home/jcz/go/bin/trilium-farmer` or `/home/jcz/.local/go/bin/trilium-farmer`

- [ ] **Step 3: Generate your ETAPI token**

In Trilium: **Options → API tokens → Create token**. Copy the token — you will not see it again.

- [ ] **Step 4: Add MCP server to Claude Code settings**

Edit `~/.claude/settings.local.json`. Merge the `mcpServers` key into the existing JSON:

```json
{
  "mcpServers": {
    "trilium": {
      "command": "trilium-farmer",
      "env": {
        "TRILIUM_URL": "http://192.168.1.102:8080",
        "TRILIUM_TOKEN": "<paste-token-here>",
        "TRILIUM_ALLOWED_ROOTS": "<noteId1>,<noteId2>"
      }
    }
  },
  "permissions": {
    "...existing permissions..."
  }
}
```

To find the note IDs for `TRILIUM_ALLOWED_ROOTS`: in Trilium, click a top-level note and look at the URL — it contains the noteId (e.g. `#root/abc123def456`). Copy the ID portion after the last `/`.

- [ ] **Step 5: Verify MCP server is registered**

```bash
claude mcp list
```

Expected: `trilium` listed as active

- [ ] **Step 6: Smoke test**

Start a new Claude Code session and ask:

> "List my root Trilium notes"

Expected: Claude calls `list_root_notes` and displays your top-level note titles with their IDs.

- [ ] **Step 7: Test privacy filtering**

In Trilium, add the `#private` label to one of your notes. Then ask Claude:

> "List children of [that note's parent]"

Expected: the `#private` note is absent from the results.

---

## Self-Review

**Spec coverage:**
- ✅ 6 MCP tools: `list_root_notes`, `get_children`, `get_note`, `search_notes`, `create_note`, `update_note` (Tasks 4–8)
- ✅ Tree navigation pattern via `list_root_notes` + `get_children` (Tasks 4, 8)
- ✅ `#private` label filtering on all read and write operations (Tasks 3–7)
- ✅ `TRILIUM_ALLOWED_ROOTS` allowlist filtering on `list_root_notes` (Task 8)
- ✅ Duplicate check instruction baked into `create_note` tool description (Task 8)
- ✅ Three friendly error messages: unreachable, invalid token, note not found (Task 2)
- ✅ Go binary, stdio transport, `TRILIUM_URL`/`TRILIUM_TOKEN` env vars (Tasks 1, 8, 9)
- ✅ `go install` one-command install (Task 9)
- ✅ MemPalace lazy bridging — no new code needed; Claude uses both MCPs naturally
- ✅ Pre-flight ETAPI verification note at top of plan

**Placeholder scan:** None found.

**Type consistency:**
- `Note` → used in `GetChildren`, `ListRootNotes`, `SearchNotes`, `CreateNote` return
- `NoteDetail` → used in `GetNote`, `isPrivate`
- `Attribute` → field of `NoteDetail`
- `CreateNoteRequest` → used in `CreateNote` and test
- All handler functions return `server.ToolHandlerFunc` and access `req.Params.Arguments` consistently
