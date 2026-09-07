package prosody

import (
	"encoding/json"
	"testing"
)

func TestDecodeJSONListEmptyObject(t *testing.T) {
	got, err := decodeJSONList[ArchiveUser](json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestDecodeJSONListArray(t *testing.T) {
	got, err := decodeJSONList[ArchiveUser](json.RawMessage(`[{"username":"alice","mam":1,"offline":0}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Username != "alice" {
		t.Fatalf("got %#v", got)
	}
}
