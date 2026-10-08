package service

import (
	"crypto/ecdh"
	"encoding/base64"
)

// Public tunnel information only. Never expose Config or its private/PSK bytes.
type TunnelDetails struct {
	Address       string `json:"address"`
	PublicKey     string `json:"publicKey"`
	PeerPublicKey string `json:"peerPublicKey"`
	Endpoint      string `json:"endpoint"`
	AllowedIPs    string `json:"allowedIPs"`
	ListenPort    uint16 `json:"listenPort"`
	MTU           uint32 `json:"mtu"`
	Keepalive     uint16 `json:"keepalive"`
}

func publicDetails(p Profile) *TunnelDetails {
	routes, _ := p.Routes()
	d := &TunnelDetails{Address: p.Config.Address.String(), PeerPublicKey: base64.StdEncoding.EncodeToString(p.Config.PublicKey[:]), Endpoint: p.Config.Endpoint, AllowedIPs: JoinPrefixes(routes), MTU: p.Config.MTU, Keepalive: p.Config.Keepalive}
	if key, err := ecdh.X25519().NewPrivateKey(p.Config.PrivateKey[:]); err == nil {
		d.PublicKey = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	}
	return d
}
