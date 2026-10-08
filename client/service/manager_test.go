package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeSession struct {
	log       *[]string
	name      string
	failClose bool
	counts    Counters
}

func (s *fakeSession) Close() error {
	*s.log = append(*s.log, "close "+s.name)
	if s.failClose {
		return errors.New("injected cleanup error")
	}
	return nil
}
func (s *fakeSession) Sample(context.Context) (Counters, error) { return s.counts, nil }

type fakeBackend struct {
	log      []string
	failOpen bool
	session  *fakeSession
}

func (b *fakeBackend) Adapters() ([]AdapterInfo, error) { return nil, nil }
func (b *fakeBackend) Open(_ context.Context, p Profile) (Session, error) {
	b.log = append(b.log, "open "+p.ID)
	b.session = &fakeSession{log: &b.log, name: p.ID}
	if b.failOpen {
		return b.session, errors.New("injected route error")
	}
	return b.session, nil
}
func profileForTest(t *testing.T, id string) Profile {
	t.Helper()
	cfg, err := ParseConfig(fixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	return Profile{ID: id, Config: cfg}
}
func TestSwitchClosesBeforeOpening(t *testing.T) {
	b := &fakeBackend{}
	m := NewManager(b)
	for _, id := range []string{"A", "B"} {
		if err := m.Connect(context.Background(), profileForTest(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(b.log, []string{"open A", "close A", "open B"}) {
		t.Fatal(b.log)
	}
	if m.Active() != "B" {
		t.Fatal("wrong active session")
	}
}
func TestFailedCleanupBlocksSwitchAndCanRetry(t *testing.T) {
	b := &fakeBackend{}
	m := NewManager(b)
	if err := m.Connect(context.Background(), profileForTest(t, "A")); err != nil {
		t.Fatal(err)
	}
	b.session.failClose = true
	if err := m.Connect(context.Background(), profileForTest(t, "B")); err == nil {
		t.Fatal("cleanup error ignored")
	}
	if m.Active() != "A" || !reflect.DeepEqual(b.log, []string{"open A", "close A"}) {
		t.Fatal(b.log)
	}
	b.session.failClose = false
	if err := m.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if m.Active() != "" {
		t.Fatal("not disconnected")
	}
}
func TestFailedConnectRollsBackAndIsDisconnected(t *testing.T) {
	b := &fakeBackend{failOpen: true}
	m := NewManager(b)
	if err := m.Connect(context.Background(), profileForTest(t, "A")); err == nil {
		t.Fatal("expected failure")
	}
	if !reflect.DeepEqual(b.log, []string{"open A", "close A"}) || m.Active() != "" {
		t.Fatal(b.log)
	}
}
func TestStatusRequiresHandshakeAndResetsCounters(t *testing.T) {
	b := &fakeBackend{}
	m := NewManager(b)
	ctx := context.Background()
	if err := m.Connect(ctx, profileForTest(t, "A")); err != nil {
		t.Fatal(err)
	}
	if m.Status(ctx).State != "handshaking" {
		t.Fatal("adapter up is not a handshake")
	}
	b.session.counts = Counters{Rx: 1024, Tx: 512, Handshake: time.Now(), LatencyMS: -1}
	if m.Status(ctx).State != "connected" {
		t.Fatal("missing handshake status")
	}
	if err := m.Connect(ctx, profileForTest(t, "B")); err != nil {
		t.Fatal(err)
	}
	status := m.Status(ctx)
	if status.RxBPS != 0 || status.TxBPS != 0 || status.RxBytes != 0 {
		t.Fatal("statistics leaked between sites")
	}
}
