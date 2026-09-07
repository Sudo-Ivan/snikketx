package audit

import "testing"

func TestStoreRecordAndSearch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := Open(dir, 8)
	if err != nil {
		t.Fatal(err)
	}

	store.Record(Event{Actor: "alice@example.test", Action: "invite.create", Target: "inv1", Detail: "For Carol", IP: "203.0.113.10", UserAgent: "TestAgent/1.0"})
	store.Record(Event{Actor: "alice@example.test", Action: "user.lock", Target: "bob"})
	store.Record(Event{Actor: "bob@example.test", Action: "login", Detail: "success"})

	hits := store.Recent(10, "invite")
	if len(hits) != 1 || hits[0].Action != "invite.create" {
		t.Fatalf("invite search = %+v", hits)
	}
	byIP := store.Recent(10, "203.0.113")
	if len(byIP) != 1 || byIP[0].IP != "203.0.113.10" {
		t.Fatalf("ip search = %+v", byIP)
	}

	all := store.Recent(10, "")
	if len(all) != 3 {
		t.Fatalf("all = %d", len(all))
	}
	if all[0].Action != "login" {
		t.Fatalf("newest = %s", all[0].Action)
	}

	reopened, err := Open(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	persisted := reopened.Recent(10, "user.lock")
	if len(persisted) != 1 {
		t.Fatalf("persisted = %+v", persisted)
	}
}
