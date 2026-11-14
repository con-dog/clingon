package config

import (
	"sync"

	"golang.org/x/sys/unix"
)

// Watch wakes when a folder's entries change (a file in it added, removed or
// replaced, as writeWhole replaces one) or a file is written to. It's how the
// menu hears the pack form and tmux and the shell, other processes, as they
// say what's changed: no polling, a kqueue event the moment it's written.
// Writes while nothing waits are kept, all of them as one: none is lost.
type Watch struct {
	kq, dir int
	stop    [2]int // a pipe: a byte down it wakes Wait to end

	mu      sync.Mutex
	waiting bool // a Wait is asleep
	closed  bool // Close has been called
	freed   bool // its descriptors have been let go
}

// WatchPath starts watching the folder or file at path.
func WatchPath(path string) (*Watch, error) {
	dir, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	kq, err := unix.Kqueue()
	if err != nil {
		_ = unix.Close(dir)
		return nil, err
	}
	w := &Watch{kq: kq, dir: dir}
	if err := unix.Pipe(w.stop[:]); err != nil {
		_ = unix.Close(kq)
		_ = unix.Close(dir)
		return nil, err
	}
	unix.CloseOnExec(w.stop[0]) // none of it is for the programs clingon starts
	unix.CloseOnExec(w.stop[1])
	var change, stop unix.Kevent_t
	unix.SetKevent(&change, dir, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
	change.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE | unix.NOTE_RENAME
	unix.SetKevent(&stop, w.stop[0], unix.EVFILT_READ, unix.EV_ADD)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{change, stop}, nil, nil); err != nil {
		w.free()
		return nil, err
	}
	return w, nil
}

// Wait sleeps until the folder or file changes, saying true, or until Close,
// saying false (as it does from then on). One Wait at a time.
func (w *Watch) Wait() bool {
	w.mu.Lock()
	if w.closed {
		w.free()
		w.mu.Unlock()
		return false
	}
	w.waiting = true
	w.mu.Unlock()
	changed := w.sleep()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waiting = false
	if !changed || w.closed { // closed as it woke: let go here, Close having left it to this
		w.free()
		return false
	}
	return true
}

// sleep is Wait's sleep: true for a change, false for Close's byte or an
// error.
func (w *Watch) sleep() bool {
	out := make([]unix.Kevent_t, 2)
	for {
		n, err := unix.Kevent(w.kq, nil, out, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false
		}
		for _, ev := range out[:n] {
			if int(ev.Ident) == w.stop[0] {
				return false
			}
		}
		return true
	}
}

// Close ends the watch and lets go of everything it holds: at once if no
// Wait is asleep, or else by waking the one that is, which says false and
// lets go itself (its descriptors can't be closed under it). Closing twice
// is fine.
func (w *Watch) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	if w.waiting {
		_, _ = unix.Write(w.stop[1], []byte{0})
		return
	}
	w.free()
}

// free closes the watch's descriptors, once. Called with mu held (or before
// the watch is anyone else's).
func (w *Watch) free() {
	if w.freed {
		return
	}
	w.freed = true
	for _, fd := range []int{w.kq, w.dir, w.stop[0], w.stop[1]} {
		_ = unix.Close(fd)
	}
}
