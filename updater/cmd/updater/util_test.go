package main

import "testing"

func TestEnvOr(t *testing.T) {
	t.Setenv("SNIKKET_TEST_STR", "")
	if got := envOr("SNIKKET_TEST_STR", "fb"); got != "fb" {
		t.Fatalf("empty=%q", got)
	}
	t.Setenv("SNIKKET_TEST_STR", "  v  ")
	if got := envOr("SNIKKET_TEST_STR", "fb"); got != "v" {
		t.Fatalf("set=%q", got)
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("SNIKKET_TEST_BOOL", "")
	if !envBool("SNIKKET_TEST_BOOL", true) || envBool("SNIKKET_TEST_BOOL", false) {
		t.Fatal("unset must return fallback")
	}
	t.Setenv("SNIKKET_TEST_BOOL", "off")
	if envBool("SNIKKET_TEST_BOOL", true) {
		t.Fatal("off must be false")
	}
	t.Setenv("SNIKKET_TEST_BOOL", "YES")
	if !envBool("SNIKKET_TEST_BOOL", false) {
		t.Fatal("YES must be true")
	}
}
