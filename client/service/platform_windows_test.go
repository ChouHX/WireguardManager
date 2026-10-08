//go:build windows

package service

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestWindowsDPAPI(t *testing.T) {
	cipher := DPAPI{}
	plain := []byte("private configuration material")
	encrypted, err := cipher.Protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, plain) {
		t.Fatal("DPAPI returned plaintext")
	}
	decoded, err := cipher.Unprotect(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, decoded) {
		t.Fatal("DPAPI round trip failed")
	}
	encrypted[len(encrypted)/2] ^= 0xff
	if _, err = cipher.Unprotect(encrypted); err == nil {
		t.Fatal("DPAPI accepted corrupt data")
	}
}
func TestWindowsStoreReplacement(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "profiles.dpapi"), Cipher: DPAPI{}}
	for _, id := range []string{"A", "B"} {
		if err := store.Save([]Profile{profileForTest(t, id)}); err != nil {
			t.Fatal(err)
		}
	}
	profiles, err := store.Load()
	if err != nil || len(profiles) != 1 || profiles[0].ID != "B" {
		t.Fatal("atomic replacement failed", err)
	}
}
