//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"wireguardmanager/client/service"
)

func checkTransportCommand(args []string) int {
	if len(args) != 1 {
		return 2
	}
	report := struct {
		HandshakeConfirmed  bool   `json:"handshakeConfirmed"`
		LocalRoutesVerified bool   `json:"localRoutesVerified"`
		Error               string `json:"error,omitempty"`
	}{}
	release, err := service.AcquireInstance()
	if err == nil {
		defer release()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err = service.CheckLoopbackTransport(ctx)
	}
	if err == nil {
		report.HandshakeConfirmed = true
		report.LocalRoutesVerified = true
	} else {
		report.Error = err.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil || os.WriteFile(args[0], data, 0600) != nil {
		return 2
	}
	if !report.HandshakeConfirmed {
		return 1
	}
	return 0
}
