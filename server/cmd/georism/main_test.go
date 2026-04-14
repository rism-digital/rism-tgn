package main

import "testing"

func TestWithDefaultSSLModeDisableURL(t *testing.T) {
	got := withDefaultSSLModeDisable("postgres://u:p@localhost:5432/db")
	want := "postgres://u:p@localhost:5432/db?sslmode=disable"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithDefaultSSLModeDisableURLKeepsSSLMode(t *testing.T) {
	in := "postgres://u:p@localhost:5432/db?sslmode=require"
	got := withDefaultSSLModeDisable(in)
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}

func TestWithDefaultSSLModeDisableKeyword(t *testing.T) {
	got := withDefaultSSLModeDisable("host=localhost port=5432 dbname=mydb user=me")
	want := "host=localhost port=5432 dbname=mydb user=me sslmode=disable"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWithDefaultSSLModeDisableKeywordKeepsSSLMode(t *testing.T) {
	in := "host=localhost dbname=mydb sslmode=require"
	got := withDefaultSSLModeDisable(in)
	if got != in {
		t.Fatalf("got %q, want %q", got, in)
	}
}
