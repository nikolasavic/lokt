//go:build !windows

package demo

import (
	"os"
	"syscall"
	"unsafe"
)

type termState struct {
	termios syscall.Termios
}

func makeRaw(fd int) (*termState, error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, &old); err != nil {
		return nil, err
	}

	raw := old
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Cflag |= syscall.CS8
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := ioctl(fd, syscall.TIOCSETA, &raw); err != nil {
		return nil, err
	}
	return &termState{termios: old}, nil
}

func restoreTerminal(fd int, state *termState) {
	_ = ioctl(fd, syscall.TIOCSETA, &state.termios)
}

func isTerminal(fd int) bool {
	var t syscall.Termios
	return ioctl(fd, syscall.TIOCGETA, &t) == nil
}

func terminalFd() int {
	return int(os.Stdin.Fd())
}

func ioctl(fd int, req uint, arg *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(arg))) //nolint:gosec // terminal ioctl
	if errno != 0 {
		return errno
	}
	return nil
}
