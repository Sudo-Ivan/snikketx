package prosody

import (
	"path/filepath"
	"testing"
)

func TestOAuthCredentialsPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, oauthCredentialsFile)

	client := New("http://example.test", "example.test", "test")
	client.SetCredentialsPath(path)

	if err := client.saveStoredCredentials("cid-1", "secret-1"); err != nil {
		t.Fatal(err)
	}

	loaded := New("http://example.test", "example.test", "test")
	loaded.SetCredentialsPath(path)
	if err := loaded.LoadStoredCredentials(); err != nil {
		t.Fatal(err)
	}
	if !loaded.IsClientRegistered() {
		t.Fatal("expected credentials to load")
	}
	id, secret, ok := loaded.clientCredentials()
	if !ok || id != "cid-1" || secret != "secret-1" {
		t.Fatalf("got %q %q ok=%v", id, secret, ok)
	}
}

func TestLoadStoredCredentialsMissingFile(t *testing.T) {
	client := New("http://example.test", "example.test", "test")
	client.SetCredentialsPath(filepath.Join(t.TempDir(), "missing.json"))
	if err := client.LoadStoredCredentials(); err != nil {
		t.Fatal(err)
	}
	if client.IsClientRegistered() {
		t.Fatal("expected empty credentials")
	}
}
