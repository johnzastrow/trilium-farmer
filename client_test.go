package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient_setsFields(t *testing.T) {
	c := NewClient("http://localhost:8080", "test-token", "private")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL http://localhost:8080, got %s", c.baseURL)
	}
	if c.token != "test-token" {
		t.Errorf("expected token test-token, got %s", c.token)
	}
	if c.privateLabel != "private" {
		t.Errorf("expected privateLabel private, got %s", c.privateLabel)
	}
}

func TestNewClient_trailingSlash(t *testing.T) {
	c := NewClient("http://localhost:8080/", "token", "private")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected trailing slash stripped, got %s", c.baseURL)
	}
}

func TestDo_connectionRefused(t *testing.T) {
	c := NewClient("http://localhost:19999", "token", "private")
	_, err := c.do("GET", "/etapi/notes/root", nil)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if !strings.Contains(err.Error(), "cannot reach Trilium") {
		t.Errorf("expected friendly error, got: %s", err.Error())
	}
}

func TestIsPrivate_labeled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"noteId":"abc","title":"Secret","type":"text","attributes":[{"type":"label","name":"private","value":""}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token", "private")
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

	c := NewClient(srv.URL, "token", "private")
	priv, err := c.isPrivate("xyz")
	if err != nil {
		t.Fatal(err)
	}
	if priv {
		t.Error("expected note without #private label to return false")
	}
}

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

	c := NewClient(srv.URL, "token", "private")
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
