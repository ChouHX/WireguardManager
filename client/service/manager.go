package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

type AdapterInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}
type Counters struct {
	Rx        uint64
	Tx        uint64
	Handshake time.Time
	LatencyMS float64
}
type Status struct {
	ProfileID string  `json:"profileID"`
	State     string  `json:"state"`
	RxBytes   uint64  `json:"rxBytes"`
	TxBytes   uint64  `json:"txBytes"`
	RxBPS     float64 `json:"rxBps"`
	TxBPS     float64 `json:"txBps"`
	LatencyMS float64 `json:"latencyMS"`
	Handshake string  `json:"handshake"`
	Error     string  `json:"error"`
}
type Session interface {
	Close() error
	Sample(context.Context) (Counters, error)
}

// Open must return a session when cleanup is incomplete, including on failure.
type Backend interface {
	Open(context.Context, Profile) (Session, error)
	Adapters() ([]AdapterInfo, error)
}
type Manager struct {
	mu       sync.Mutex
	backend  Backend
	session  Session
	active   string
	previous Counters
	sampled  time.Time
	fault    string
}

func NewManager(b Backend) *Manager { return &Manager{backend: b} }
func (m *Manager) Connect(ctx context.Context, p Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := p.Routes(); err != nil {
		return err
	}
	if err := m.disconnect(); err != nil {
		return err
	}
	session, err := m.backend.Open(ctx, p)
	m.session = session
	if session != nil {
		m.active = p.ID
	}
	if err != nil {
		m.fault = err.Error()
		if session != nil {
			if cleanup := m.disconnect(); cleanup != nil {
				return errors.Join(err, cleanup)
			}
		}
		return err
	}
	if session == nil {
		return errors.New("驱动未返回隧道")
	}
	m.fault = ""
	return nil
}
func (m *Manager) Disconnect() error { m.mu.Lock(); defer m.mu.Unlock(); return m.disconnect() }
func (m *Manager) disconnect() error {
	if m.session != nil {
		if err := m.session.Close(); err != nil {
			m.fault = "清理未完成：" + err.Error()
			return errors.New(m.fault)
		}
	}
	m.session = nil
	m.active = ""
	m.sampled = time.Time{}
	m.previous = Counters{}
	m.fault = ""
	return nil
}
func (m *Manager) Active() string { m.mu.Lock(); defer m.mu.Unlock(); return m.active }
func (m *Manager) Status(ctx context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{State: "disconnected", LatencyMS: -1, Error: m.fault, ProfileID: m.active}
	if m.session == nil {
		return status
	}
	if m.fault != "" {
		status.State = "error"
		return status
	}
	status.State = "handshaking"
	c, err := m.session.Sample(ctx)
	if err != nil {
		status.State = "error"
		status.Error = err.Error()
		return status
	}
	now := time.Now()
	status.RxBytes = c.Rx
	status.TxBytes = c.Tx
	status.LatencyMS = c.LatencyMS
	if !c.Handshake.IsZero() {
		status.Handshake = c.Handshake.Format(time.RFC3339)
		if now.Sub(c.Handshake) < 3*time.Minute {
			status.State = "connected"
		} else {
			status.State = "stale"
		}
	}
	if !m.sampled.IsZero() {
		seconds := now.Sub(m.sampled).Seconds()
		if seconds > 0 && c.Rx >= m.previous.Rx && c.Tx >= m.previous.Tx {
			status.RxBPS = float64(c.Rx-m.previous.Rx) / seconds
			status.TxBPS = float64(c.Tx-m.previous.Tx) / seconds
		}
	}
	m.sampled = now
	m.previous = c
	return status
}
