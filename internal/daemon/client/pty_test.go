package client

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func newPTYAttachmentPair(t *testing.T) (*ptyAttachment, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	a := &ptyAttachment{
		conn:   clientConn,
		enc:    protocol.NewEncoder(clientConn),
		dec:    protocol.NewDecoder(clientConn),
		events: make(chan PTYEvent, 64),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go a.readLoop()
	return a, serverConn
}

func waitPTYAttachmentClosed(t *testing.T, a *ptyAttachment) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(time.Second):
		t.Fatal("PTY attachment read loop did not finish")
	}
}

func TestPTYAttachmentAbruptEOFEmitsTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	peer.Close()

	ev, ok := <-a.Events()
	if !ok {
		t.Fatal("attachment closed without transport error event")
	}
	if ev.Kind != protocol.KindPTYError || !strings.HasPrefix(ev.Error, "PTY transport:") {
		t.Fatalf("event = %#v, want PTY transport error", ev)
	}
	waitPTYAttachmentClosed(t, a)
	if _, ok := <-a.Events(); ok {
		t.Fatal("event channel should close after transport error")
	}
}

func TestPTYAttachmentMalformedFrameEmitsTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	defer peer.Close()
	if _, err := peer.Write([]byte("{not-json}\n")); err != nil {
		t.Fatal(err)
	}

	ev, ok := <-a.Events()
	if !ok {
		t.Fatal("attachment closed without malformed-frame error event")
	}
	if ev.Kind != protocol.KindPTYError || !strings.Contains(ev.Error, "PTY transport:") {
		t.Fatalf("event = %#v, want PTY transport error", ev)
	}
	waitPTYAttachmentClosed(t, a)
}

func TestPTYAttachmentOutputPrecedesAbruptTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	enc := protocol.NewEncoder(peer)
	if err := enc.WriteResponse(&protocol.Response{
		OK: true, Kind: protocol.KindPTYOutput, Seq: 9, Data: []byte("before-drop"),
	}); err != nil {
		t.Fatal(err)
	}
	peer.Close()

	out, ok := <-a.Events()
	if !ok || out.Kind != protocol.KindPTYOutput || out.Seq != 9 || string(out.Data) != "before-drop" {
		t.Fatalf("first event = %#v, %v; want output before-drop", out, ok)
	}
	errEvent, ok := <-a.Events()
	if !ok || errEvent.Kind != protocol.KindPTYError || !strings.HasPrefix(errEvent.Error, "PTY transport:") {
		t.Fatalf("second event = %#v, %v; want transport error", errEvent, ok)
	}
	waitPTYAttachmentClosed(t, a)
}

func TestPTYAttachmentIntentionalCloseDoesNotEmitTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	defer peer.Close()
	reqCh := make(chan *protocol.Request, 1)
	errCh := make(chan error, 1)
	go func() {
		req, err := protocol.NewDecoder(peer).ReadRequest()
		if err != nil {
			errCh <- err
			return
		}
		reqCh <- req
	}()

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatalf("read detach: %v", err)
	case req := <-reqCh:
		if req.Kind != protocol.KindPTYDetach {
			t.Fatalf("close request kind = %q, want %q", req.Kind, protocol.KindPTYDetach)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for detach request")
	}
	waitPTYAttachmentClosed(t, a)
	if ev, ok := <-a.Events(); ok {
		t.Fatalf("intentional close emitted unexpected event: %#v", ev)
	}
}

func TestPTYAttachmentNormalExitDoesNotEmitTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	defer peer.Close()
	if err := protocol.NewEncoder(peer).WriteResponse(&protocol.Response{
		OK: true, Kind: protocol.KindPTYExit, ExitCode: 0, EOF: true,
	}); err != nil {
		t.Fatal(err)
	}

	ev, ok := <-a.Events()
	if !ok || ev.Kind != protocol.KindPTYExit || ev.ExitCode != 0 {
		t.Fatalf("exit event = %#v, %v; want successful ptyExit", ev, ok)
	}
	waitPTYAttachmentClosed(t, a)
	if extra, ok := <-a.Events(); ok {
		t.Fatalf("normal exit emitted extra event: %#v", extra)
	}
}

func TestPTYAttachmentProtocolErrorEOFIsNotDuplicatedAsTransportError(t *testing.T) {
	a, peer := newPTYAttachmentPair(t)
	defer peer.Close()
	if err := protocol.NewEncoder(peer).WriteResponse(&protocol.Response{
		OK: false, Kind: protocol.KindPTYError, Error: "server-error", EOF: true,
	}); err != nil {
		t.Fatal(err)
	}

	ev, ok := <-a.Events()
	if !ok || ev.Kind != protocol.KindPTYError || ev.Error != "server-error" {
		t.Fatalf("error event = %#v, %v; want server-error", ev, ok)
	}
	waitPTYAttachmentClosed(t, a)
	if extra, ok := <-a.Events(); ok {
		t.Fatalf("protocol EOF error emitted duplicate event: %#v", extra)
	}
}
