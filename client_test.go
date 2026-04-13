package main

import (
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
