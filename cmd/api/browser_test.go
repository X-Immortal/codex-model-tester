package main

import "testing"

func TestAdminUIURL(t *testing.T) {
	got, err := adminUIURL(":8080")
	if err != nil {
		t.Fatalf("adminUIURL() error = %v", err)
	}
	if got != "http://127.0.0.1:8080/" {
		t.Fatalf("adminUIURL() = %q, want %q", got, "http://127.0.0.1:8080/")
	}

	if _, err := adminUIURL("not-an-address"); err == nil {
		t.Fatal("adminUIURL() error = nil, want invalid address error")
	}
}
