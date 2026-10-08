//go:build windows

package service

import (
	"encoding/binary"
	"golang.org/x/sys/windows"
	"net/netip"
	"runtime"
	"unsafe"
)

var ipHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var icmpCreate = ipHelper.NewProc("IcmpCreateFile")
var icmpClose = ipHelper.NewProc("IcmpCloseHandle")
var icmpEcho = ipHelper.NewProc("IcmpSendEcho2Ex")

func icmpLatency(source, target netip.Addr) float64 {
	h, _, _ := icmpCreate.Call()
	if h == 0 || h == ^uintptr(0) {
		return -1
	}
	defer icmpClose.Call(h)
	src, dst := source.As4(), target.As4()
	request := []byte("WGM latency")
	reply := make([]byte, 256)
	count, _, _ := icmpEcho.Call(h, 0, 0, 0, uintptr(binary.LittleEndian.Uint32(src[:])), uintptr(binary.LittleEndian.Uint32(dst[:])), uintptr(unsafe.Pointer(&request[0])), uintptr(len(request)), 0, uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), 700)
	runtime.KeepAlive(request)
	if count == 0 || binary.LittleEndian.Uint32(reply[4:8]) != 0 {
		return -1
	}
	return float64(binary.LittleEndian.Uint32(reply[8:12]))
}
