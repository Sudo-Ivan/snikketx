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
