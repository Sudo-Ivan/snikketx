package credstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return store, dir
}

func TestEnsureUserIDStable(t *testing.T) {
	store, _ := testStore(t)
	first, err := store.EnsureUserID("alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != userIDBytes {
		t.Fatalf("handle length %d, want %d", len(first), userIDBytes)
	}
	second, err := store.EnsureUserID("alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("user handle changed between calls")
	}
	other, err := store.EnsureUserID("bob@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(other) {
		t.Fatal("distinct accounts share a user handle")
	}
}

func TestPasskeyLifecycle(t *testing.T) {
	store, _ := testStore(t)
	jid := "alice@example.test"
	cred := webauthn.Credential{ID: []byte("cred-1"), PublicKey: []byte("pk")}

	if err := store.AddPasskey(jid, "laptop", cred); err != nil {
		t.Fatal(err)
	}
	acct := store.Get(jid)
	if acct == nil || len(acct.Passkeys) != 1 {
		t.Fatal("passkey not stored")
	}
	if !acct.NeedsSecondFactor() {
		t.Fatal("account with a passkey must require a second factor")
	}
	if acct.Passkeys[0].Name != "laptop" {
		t.Fatalf("name = %q", acct.Passkeys[0].Name)
	}

	// Re-adding the same credential replaces instead of duplicating.
	if err := store.AddPasskey(jid, "again", cred); err != nil {
		t.Fatal(err)
	}
	if got := len(store.Get(jid).Passkeys); got != 1 {
		t.Fatalf("passkeys = %d, want 1", got)
	}

	removed, err := store.RemovePasskey(jid, []byte("cred-1"))
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	removed, err = store.RemovePasskey(jid, []byte("cred-1"))
	if err != nil || removed {
		t.Fatalf("second remove removed=%v err=%v", removed, err)
	}
	if store.Get(jid).NeedsSecondFactor() {
		t.Fatal("empty account must not require a second factor")
	}
}

func TestTOTPSealedAtRest(t *testing.T) {
	store, dir := testStore(t)
	jid := "alice@example.test"
	const secret = "JBSWY3DPEHPK3PXP"

	if err := store.SetTOTP(jid, secret); err != nil {
		t.Fatal(err)
	}
	got, enabled, err := store.TOTPSecret(jid)
	if err != nil || !enabled || got != secret {
		t.Fatalf("secret=%q enabled=%v err=%v", got, enabled, err)
	}

	// The document must not carry the raw secret.
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("TOTP secret stored in plain text")
	}

	// A store with a different key cannot read it.
	other, err := Open(dir, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatal(err)
	}
	if _, enabled, err := other.TOTPSecret(jid); err == nil && enabled {
		t.Fatal("secret readable under the wrong key")
	}

	if err := store.ClearTOTP(jid); err != nil {
		t.Fatal(err)
	}
	if _, enabled, _ := store.TOTPSecret(jid); enabled {
		t.Fatal("secret still readable after clear")
	}
}

func TestReloadPersists(t *testing.T) {
	store, dir := testStore(t)
	jid := "alice@example.test"
	if err := store.AddPasskey(jid, "key", webauthn.Credential{ID: []byte("c"), PublicKey: []byte("p")}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTOTP(jid, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(dir, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	acct := reloaded.Get(jid)
	if acct == nil || len(acct.Passkeys) != 1 || !acct.TOTPEnabled {
		t.Fatal("state did not survive a reload")
	}
	secret, enabled, err := reloaded.TOTPSecret(jid)
	if err != nil || !enabled || secret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("secret=%q enabled=%v err=%v", secret, enabled, err)
	}
}
