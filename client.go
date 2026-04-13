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
	baseURL      string
	token        string
	privateLabel string // label name that marks notes as private (default: "private")
	http         *http.Client
}

// NewClient creates a new Trilium ETAPI client.
// privateLabel is the Trilium label name used to mark notes as private (e.g. "private", "hidden").
func NewClient(baseURL, token, privateLabel string) *Client {
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		token:        token,
		privateLabel: privateLabel,
		http:         &http.Client{Timeout: 10 * time.Second},
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
// Callers are responsible for closing resp.Body on success (200/201/204).
func (c *Client) checkStatus(resp *http.Response, noteID string) error {
	switch resp.StatusCode {
	case 200, 201, 204:
		return nil
	case 401:
		resp.Body.Close()
		return fmt.Errorf("TRILIUM_TOKEN is invalid or expired — generate a new one in Trilium Options → API tokens")
	case 404:
		resp.Body.Close()
		return fmt.Errorf("note %s not found", noteID)
	default:
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return fmt.Errorf("ETAPI error %d: %s", resp.StatusCode, string(body))
	}
}

// decodeJSON decodes a JSON response body into v.
func decodeJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// isPrivate returns true if the note has the configured private label.
func (c *Client) isPrivate(noteID string) (bool, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID, nil)
	if err != nil {
		return false, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
		return false, err
	}
	var note NoteDetail
	if err := decodeJSON(resp, &note); err != nil {
		return false, err
	}
	for _, attr := range note.Attributes {
		if attr.Type == "label" && attr.Name == c.privateLabel {
			return true, nil
		}
	}
	return false, nil
}

// GetChildren returns the visible (non-private) child notes of noteID.
func (c *Client) GetChildren(noteID string) ([]Note, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID+"/children", nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
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

// GetNote returns a note's metadata and full content.
// Returns an error if the note has the configured private label.
func (c *Client) GetNote(noteID string) (*NoteDetail, error) {
	resp, err := c.do("GET", "/etapi/notes/"+noteID, nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(resp, noteID); err != nil {
		return nil, err
	}
	var note NoteDetail
	if err := decodeJSON(resp, &note); err != nil {
		return nil, err
	}
	for _, attr := range note.Attributes {
		if attr.Type == "label" && attr.Name == c.privateLabel {
			return nil, fmt.Errorf("note %s is marked #%s and cannot be accessed", noteID, c.privateLabel)
		}
	}
	contentResp, err := c.do("GET", "/etapi/notes/"+noteID+"/content", nil)
	if err != nil {
		return nil, err
	}
	if err := c.checkStatus(contentResp, noteID); err != nil {
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

// Ensure url is imported (used in SearchNotes added in a later task).
var _ = url.QueryEscape
