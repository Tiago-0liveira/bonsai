package agym

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
)

const (
	MaxEventBytes = 256 * 1024 // 256 KiB per NDJSON frame
)

// StreamEvents launches the agym events follow process and emits events over a channel.
func (c *ExecClient) StreamEvents(ctx context.Context, runID string, after uint64) (<-chan Event, <-chan error) {
	eventsCh := make(chan Event, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventsCh)
		defer close(errCh)

		bin := c.binaryPath
		if bin == "" {
			var err error
			bin, err = ResolveBinary("")
			if err != nil {
				errCh <- err
				return
			}
		}

		args := []string{
			"integration", "run", "events",
			"--id", runID,
			"--after", strconv.FormatUint(after, 10),
			"--follow",
			"--protocol", strconv.Itoa(SupportedProtocolMajor),
			"--ndjson",
		}

		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), "NO_COLOR=1")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			errCh <- fmt.Errorf("%w: failed to create stdout pipe: %v", ErrTransport, err)
			return
		}
		cmd.Stderr = io.Discard

		if err := cmd.Start(); err != nil {
			errCh <- fmt.Errorf("%w: failed to start agym events stream: %v", ErrTransport, err)
			return
		}

		// Ensure process is cleaned up when context completes
		go func() {
			<-ctx.Done()
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}()

		sc := bufio.NewScanner(stdout)
		buf := make([]byte, MaxEventBytes)
		sc.Buffer(buf, MaxEventBytes)

		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}

			// Each NDJSON frame can be an Event directly or wrapped
			var ev Event
			if err := json.Unmarshal(line, &ev); err != nil {
				// Try unwrapping envelope if present
				var wrapped struct {
					Event *Event `json:"event"`
					Data  *Event `json:"data"`
				}
				if jsonErr := json.Unmarshal(line, &wrapped); jsonErr == nil && (wrapped.Event != nil || wrapped.Data != nil) {
					if wrapped.Event != nil {
						ev = *wrapped.Event
					} else {
						ev = *wrapped.Data
					}
				} else {
					continue // Ignore unparseable or unknown frames
				}
			}

			select {
			case <-ctx.Done():
				return
			case eventsCh <- ev:
			}
		}

		if err := sc.Err(); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("%w: reading ndjson stream: %v", ErrTransport, err)
		}

		_ = cmd.Wait()
	}()

	return eventsCh, errCh
}

// ParseOutputPayload extracts stdout/stderr text from an event payload.
func ParseOutputPayload(ev Event) (OutputPayload, error) {
	var payload OutputPayload
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return OutputPayload{}, err
	}
	return payload, nil
}
