package agenthost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Launcher starts a holder for spec and returns once it serves its socket.
type Launcher func(Paths, Spec) error

// readyLine is the holder's one stdout message.
type readyLine struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Launch starts `bonsai __agent-host` detached (new session, no terminal), so
// it outlives the caller, passes it spec on stdin and waits up to five
// seconds for it to report ready. BONSAI_DAEMON_BIN overrides the binary.
func Launch(p Paths, spec Spec) error {
	bin := os.Getenv("BONSAI_DAEMON_BIN")
	if bin == "" {
		var err error
		if bin, err = os.Executable(); err != nil {
			return err
		}
		base := filepath.Base(bin)
		if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe") {
			return errors.New("cannot launch an agent host from a test binary")
		}
	}
	cmd := exec.Command(bin, "__agent-host", "--records", p.Records, "--sockets", p.Sockets)
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd.Stderr = devnull
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	setDetach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	encoded, _ := json.Marshal(spec)
	_, _ = stdin.Write(append(encoded, '\n'))
	_ = stdin.Close()
	answer := make(chan readyLine, 1)
	go func() {
		var line readyLine
		raw, err := bufio.NewReader(io.LimitReader(stdout, 64<<10)).ReadBytes('\n')
		if err != nil || json.Unmarshal(raw, &line) != nil {
			line = readyLine{Error: "agent host exited before it was ready"}
		}
		answer <- line
	}()
	select {
	case line := <-answer:
		// Reap the holder whenever it exits.
		go func() { _ = cmd.Wait() }()
		if !line.OK {
			return errors.New(line.Error)
		}
		return nil
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-answer
		_ = cmd.Wait()
		return fmt.Errorf("agent host did not start in time")
	}
}

// ReadSpec reads a holder's spec from its stdin.
func ReadSpec(r io.Reader) (Spec, error) {
	var spec Spec
	raw, err := bufio.NewReader(io.LimitReader(r, 1<<20)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return spec, err
	}
	err = json.Unmarshal(raw, &spec)
	return spec, err
}

// Ready reports the holder's start to its launcher on stdout, then points
// stdout at /dev/null: the launcher stops reading, and the descriptor must
// not be reused by a socket a stray print could reach.
func Ready(err error) {
	line := readyLine{OK: err == nil}
	if err != nil {
		line.Error = err.Error()
	}
	encoded, _ := json.Marshal(line)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	releaseStdout()
}
