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
	kq, dir  int
	stop     [2]int // a pipe: a byte down it wakes Wait to end
	stopOnce sync.Once
}

// WatchPath starts watching the folder or file at path.
func WatchPath(path string) (*Watch, error) {
	dir, err := unix.Open(path, unix.O_EVTONLY, 0)
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
	var change, stop unix.Kevent_t
	unix.SetKevent(&change, dir, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
	change.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE | unix.NOTE_RENAME
	unix.SetKevent(&stop, w.stop[0], unix.EVFILT_READ, unix.EV_ADD)
	if _, err := unix.Kevent(kq, []unix.Kevent_t{change, stop}, nil, nil); err != nil {
		w.closeAll()
		return nil, err
	}
	return w, nil
}

// Wait sleeps until the folder or file changes, saying true, or until Close, saying
// false: then it lets everything go, so a Wait must follow every Close.
func (w *Watch) Wait() bool {
	out := make([]unix.Kevent_t, 2)
	for {
		n, err := unix.Kevent(w.kq, nil, out, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			w.closeAll()
			return false
		}
		for _, ev := range out[:n] {
			if int(ev.Ident) == w.stop[0] {
				w.closeAll()
				return false
			}
		}
		return true
	}
}

// Close ends the watch: the Wait asleep (or the next) wakes and says false.
func (w *Watch) Close() {
	w.stopOnce.Do(func() { _, _ = unix.Write(w.stop[1], []byte{0}) })
}

func (w *Watch) closeAll() {
	for _, fd := range []int{w.kq, w.dir, w.stop[0], w.stop[1]} {
		_ = unix.Close(fd)
	}
}
