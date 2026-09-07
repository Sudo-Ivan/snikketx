package prosody

import (
	"encoding/json"
	"testing"
)

func TestAdminGroupInfoMembersAsObject(t *testing.T) {
	t.Parallel()

	const body = `{"id":"default","name":"default","members":{"alice":true,"bob":true},"chats":{"c1":{"id":"c1","jid":"c1@groups.example.test","name":"Family"}}}`
	var group AdminGroupInfo
	if err := json.Unmarshal([]byte(body), &group); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if group.ID != "default" || group.Name != "default" {
		t.Fatalf("identity = %+v", group)
	}
	if len(group.Members) != 2 {
		t.Fatalf("members = %v", group.Members)
	}
	if len(group.Chats) != 1 || group.Chats[0].Name != "Family" {
		t.Fatalf("chats = %+v", group.Chats)
	}
}

func TestAdminGroupInfoMembersAsArray(t *testing.T) {
	t.Parallel()

	const body = `[{"id":"g1","name":"Family","members":["alice"],"chats":[{"id":"c1","jid":"c1@groups.example.test","name":"Chat","deleted":true},{"id":"c2","jid":"c2@groups.example.test","name":"Live"}]}]`
	var groups []AdminGroupInfo
	if err := json.Unmarshal([]byte(body), &groups); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len = %d", len(groups))
	}
	if len(groups[0].Chats) != 1 || groups[0].Chats[0].ID != "c2" {
		t.Fatalf("chats = %+v", groups[0].Chats)
	}
}
