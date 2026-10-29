package agentterminal

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// RunInStream runs prepared in its own PTY and process group, sends its output
// to stream and binds the stream's input and resize to it until it exits.
// Canceling ctx sends SIGTERM to the process group, then SIGKILL after two
// seconds; descendants are killed when the process exits. It returns the exit
// code (-1 when the process never ran).
func RunInStream[S any](ctx context.Context, stream *Stream[S], prepared agents.PreparedSession, cols, rows int) (int, error) {
	return runInStream(ctx, stream, prepared, cols, rows, nil, nil)
}

// runInStream is RunInStream with started, called once the process runs, and
// output, called with each chunk the process writes before the stream gets
// it. An error from started stops the process and is returned. output must
// neither keep nor alter its chunk.
func runInStream[S any](ctx context.Context, stream *Stream[S], prepared agents.PreparedSession, cols, rows int, started func() error, output func([]byte)) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	proc, err := launch(prepared, cols, rows)
	if err != nil {
		return -1, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream.bind(proc)
	var startErr error
	if started != nil {
		if startErr = started(); startErr != nil {
			cancel()
		}
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 16384)
		for {
			n, readErr := proc.file.Read(buf)
			if n > 0 {
				if output != nil {
					output(buf[:n])
				}
				_, _ = stream.Write(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()
	waited := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		select {
		case <-ctx.Done():
			select {
			case <-waited:
				return
			default:
			}
			proc.signal(false)
			select {
			case <-waited:
			case <-time.After(2 * time.Second):
				proc.signal(true)
			}
		case <-waited:
		}
	}()
	code, err := proc.wait()
	close(waited)
	<-monitorDone
	// Descendants must not retain the slave terminal after the process exits.
	proc.signal(true)
	select {
	case <-drained:
	case <-time.After(time.Second):
		_ = proc.file.Close()
		<-drained
	}
	stream.bind(nil)
	_ = proc.file.Close()
	if startErr != nil {
		return code, startErr
	}
	return code, err
}
