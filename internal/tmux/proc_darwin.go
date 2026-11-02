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

// groupMember is a live (non-zombie) process in process group pgid.
func groupMember(pgid int) (int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return 0, err
	}
	for _, p := range procs {
		if p.Proc.P_stat != 5 { // SZOMB
			return int(p.Proc.P_pid), nil
		}
	}
	return 0, unix.ESRCH
}

// flushInput discards keys typed on the terminal but not yet read.
func flushInput(fd int) error { return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH) }

// waitExit sleeps until process pid exits, using a kqueue exit event.
func waitExit(pid int) error {
	kq, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(kq) }()
	var ev unix.Kevent_t
	unix.SetKevent(&ev, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	ev.Fflags = unix.NOTE_EXIT
	changes := []unix.Kevent_t{ev}
	out := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, changes, out, nil)
		if err == unix.EINTR {
			changes = nil // already registered
			continue
		}
		if err == nil && n == 1 && out[0].Flags&unix.EV_ERROR != 0 {
			return unix.Errno(out[0].Data) // e.g. ESRCH: already gone
		}
		return err
	}
}
