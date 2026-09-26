//go:build !windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	creackpty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type unixBackend struct {
	cols   int
	rows   int
	master *os.File
	fd     int

	wakeR *os.File
	wakeW *os.File

	stateMu  sync.Mutex
	closing  bool
	readers  int
	readCond *sync.Cond

	closeOnce sync.Once
	closeErr  error
}

func newBackend(cols, rows int) (backend, error) {
	wakeR, wakeW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p := &unixBackend{
		cols: cols, rows: rows,
		fd: -1,
		wakeR: wakeR,
		wakeW: wakeW,
	}
	p.readCond = sync.NewCond(&p.stateMu)
	return p, nil
}

func (p *unixBackend) Start(cmd *exec.Cmd) error {
	if p.master != nil {
		return fmt.Errorf("PTY already started")
	}
	master, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{
		Rows: uint16(p.rows),
		Cols: uint16(p.cols),
	})
	if err != nil {
		return err
	}
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = master.Close()
		return err
	}
	p.master = master
	p.fd = fd
	return nil
}

func (p *unixBackend) beginRead() (int, bool) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.closing || p.fd < 0 {
		return -1, false
	}
	p.readers++
	return p.fd, true
}

func (p *unixBackend) endRead() {
	p.stateMu.Lock()
	p.readers--
	if p.readers == 0 {
		p.readCond.Broadcast()
	}
	p.stateMu.Unlock()
}

func (p *unixBackend) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	fd, ok := p.beginRead()
	if !ok {
		return 0, os.ErrClosed
	}
	defer p.endRead()

	wakeFD := int32(p.wakeR.Fd())
	fds := []unix.PollFd{
		{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
		{Fd: wakeFD, Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR},
	}
	for {
		fds[0].Revents = 0
		fds[1].Revents = 0
		if _, err := unix.Poll(fds, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, err
		}
		if fds[1].Revents != 0 {
			return 0, os.ErrClosed
		}
		if fds[0].Revents == 0 {
			continue
		}
		n, err := unix.Read(fd, buf)
		if n > 0 {
			return n, nil
		}
		if err == nil {
			return 0, io.EOF
		}
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			continue
		}
		return 0, err
	}
}

func (p *unixBackend) Write(buf []byte) (int, error) {
	if p.master == nil {
		return 0, os.ErrInvalid
	}
	return p.master.Write(buf)
}

func (p *unixBackend) Resize(cols, rows int) error {
	if p.master == nil {
		return os.ErrInvalid
	}
	if err := creackpty.Setsize(p.master, &creackpty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}); err != nil {
		return err
	}
	p.cols, p.rows = cols, rows
	return nil
}

func (p *unixBackend) Size() (int, int, error) {
	if p.master == nil {
		return 0, 0, os.ErrInvalid
	}
	rows, cols, err := creackpty.Getsize(p.master)
	if err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

func (p *unixBackend) Close() error {
	p.closeOnce.Do(func() {
		p.stateMu.Lock()
		p.closing = true
		p.stateMu.Unlock()

		// Wake any reader blocked in poll before closing the PTY descriptor.
		// Waiting for readers to leave prevents fd reuse from racing a raw read.
		if p.wakeW != nil {
			_, _ = p.wakeW.Write([]byte{1})
		}

		p.stateMu.Lock()
		for p.readers > 0 {
			p.readCond.Wait()
		}
		p.stateMu.Unlock()

		if p.master != nil {
			p.closeErr = p.master.Close()
		}
		if p.wakeR != nil {
			if err := p.wakeR.Close(); p.closeErr == nil {
				p.closeErr = err
			}
		}
		if p.wakeW != nil {
			if err := p.wakeW.Close(); p.closeErr == nil {
				p.closeErr = err
			}
		}
	})
	return p.closeErr
}

func waitProcess(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		var killErr error
		if cmd.Process != nil {
			killErr = cmd.Process.Kill()
		}
		<-done // always reap
		if killErr != nil {
			return errors.Join(ctx.Err(), killErr)
		}
		return ctx.Err()
	}
}

var _ io.ReadWriteCloser = (*unixBackend)(nil)
