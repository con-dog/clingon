package tmux

import "golang.org/x/sys/unix"

// foregroundGroup is the process group running in the terminal that pid
// (a shell) belongs to — the shell itself when it's at its prompt.
func foregroundGroup(pid int) (int, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, err
	}
	return int(k.Eproc.Tpgid), nil
}

// flushInput discards keys typed on the terminal but not yet read.
func flushInput(fd int) error { return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH) }
