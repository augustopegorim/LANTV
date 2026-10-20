//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// Se o lantv morrer, o kernel encerra os ffmpeg junto.
func configurarProcesso(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func aposIniciar(cmd *exec.Cmd) {}
