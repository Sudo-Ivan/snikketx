package webui

import "testing"

func TestFormatFloatDereferencesPointer(t *testing.T) {
	t.Parallel()
	v := 1.25
	got := FormatFloat(&v)
	if got != "1.25" {
		t.Fatalf("got %q", got)
	}
	if FormatFloat((*float64)(nil)) != "n/a" {
		t.Fatal("nil pointer should be n/a")
	}
}

func TestFormatAgoUnix(t *testing.T) {
	t.Parallel()
	if FormatAgoUnix(nil) != "never" {
		t.Fatal("nil should be never")
	}
	sec := int64(1)
	if FormatAgoUnix(&sec) == "never" {
		t.Fatal("expected relative time")
	}
}

func TestFormatRFC3339Ago(t *testing.T) {
	t.Parallel()
	if FormatRFC3339Ago("") != "never" {
		t.Fatal("empty should be never")
	}
	got := FormatRFC3339Ago("2020-01-01T00:00:00Z")
	if got == "never" || got == "" {
		t.Fatalf("got %q", got)
	}
}
