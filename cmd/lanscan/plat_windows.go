//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi         = windows.NewLazySystemDLL("iphlpapi.dll")
	procSendARP      = iphlpapi.NewProc("SendARP")
	procIcmpCreate   = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpClose    = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho = iphlpapi.NewProc("IcmpSendEcho")
)

// arpMAC pergunta "quem tem este IP?" na rede local (não precisa de administrador).
func arpMAC(ip netip.Addr) (string, bool) {
	var mac [8]byte
	n := uint32(len(mac))
	dest := binary.LittleEndian.Uint32(ip.AsSlice())
	r, _, _ := procSendARP.Call(uintptr(dest), 0, uintptr(unsafe.Pointer(&mac[0])), uintptr(unsafe.Pointer(&n)))
	if r != 0 || n < 6 {
		return "", false
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]), true
}

type icmpEchoReply struct {
	Address     uint32
	Status      uint32
	RTT         uint32
	DataSize    uint16
	Reserved    uint16
	Data        uintptr
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// pingICMP usa a API de ping do Windows (sem administrador). Devolve o tempo e o TTL.
func pingICMP(ip netip.Addr, limite time.Duration) (time.Duration, int, bool) {
	h, _, _ := procIcmpCreate.Call()
	if h == 0 || h == ^uintptr(0) {
		return 0, 0, false
	}
	defer procIcmpClose.Call(h)
	dados := []byte("lanscan-ping")
	resp := make([]byte, 512)
	dest := binary.LittleEndian.Uint32(ip.AsSlice())
	n, _, _ := procIcmpSendEcho.Call(h, uintptr(dest), uintptr(unsafe.Pointer(&dados[0])), uintptr(len(dados)),
		0, uintptr(unsafe.Pointer(&resp[0])), uintptr(len(resp)), uintptr(limite.Milliseconds()))
	if n == 0 {
		return 0, 0, false
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&resp[0]))
	if r.Status != 0 {
		return 0, 0, false
	}
	return time.Duration(r.RTT) * time.Millisecond, int(r.TTL), true
}

func abrirNavegador(url string) {
	c := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	c.Start()
}
