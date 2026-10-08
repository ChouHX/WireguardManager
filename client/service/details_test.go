package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusDetailsArePublicAndClearOnDisconnect(t *testing.T) {
	p := profileForTest(t, "test")
	for i := range p.Config.PrivateKey {
		p.Config.PrivateKey[i] = byte(i + 31)
		p.Config.PresharedKey[i] = byte(i + 71)
	}
	p.Targets = "192.168.9.100"
	b := &fakeBackend{}
	m := NewManager(b)
	if err := m.Connect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	status := m.Status(context.Background())
	if status.Details == nil || !strings.Contains(status.Details.AllowedIPs, "192.168.9.100/32") || status.Details.Endpoint != p.Config.Endpoint || status.Details.PublicKey == "" {
		t.Fatal("missing effective tunnel details")
	}
	raw, _ := json.Marshal(status)
	for _, secret := range [][32]byte{p.Config.PrivateKey, p.Config.PresharedKey} {
		if strings.Contains(string(raw), base64.StdEncoding.EncodeToString(secret[:])) {
			t.Fatal("private key material reached status bridge")
		}
	}
	status.Details.Endpoint = "modified by caller"
	if m.Status(context.Background()).Details.Endpoint != p.Config.Endpoint {
		t.Fatal("status exposes mutable internal data")
	}
	if err := m.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if m.Status(context.Background()).Details != nil {
		t.Fatal("old tenant details survived disconnect")
	}
}
