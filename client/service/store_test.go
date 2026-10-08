package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type testCipher struct{ fail bool }

func (c *testCipher) Protect(b []byte) ([]byte, error) {
	if c.fail {
		return nil, errors.New("injected encryption failure")
	}
	result := append([]byte("test-only:"), b...)
	for i := 10; i < len(result); i++ {
		result[i] ^= 0xaa
	}
	return result, nil
}
func (c *testCipher) Unprotect(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, []byte("test-only:")) {
		return nil, errors.New("bad cipher")
	}
	r := append([]byte{}, b[10:]...)
	for i := range r {
		r[i] ^= 0xaa
	}
	return r, nil
}
func TestStoreProtectsAndDoesNotOverwriteOnFailure(t *testing.T) {
	cipher := &testCipher{}
	s := Store{Path: filepath.Join(t.TempDir(), "profiles.dpapi"), Cipher: cipher}
	p := profileForTest(t, "A")
	p.Targets = "192.168.0.100/32"
	if err := s.Save([]Profile{p}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PrivateKey")) {
		t.Fatal("plaintext persisted")
	}
	loaded, err := s.Load()
	if err != nil || len(loaded) != 1 || loaded[0].Config.PrivateKey != p.Config.PrivateKey || loaded[0].Targets != p.Targets {
		t.Fatal("round trip failed", err)
	}
	cipher.fail = true
	if err = s.Save(nil); err == nil {
		t.Fatal("expected error")
	}
	after, err := os.ReadFile(s.Path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("failed save damaged existing profiles")
	}
}
