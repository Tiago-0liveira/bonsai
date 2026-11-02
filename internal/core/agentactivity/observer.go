package agentactivity

import (
	"strconv"
	"strings"
	"time"
)

// maxOSCBytes bounds one OSC payload. Longer payloads are dropped whole: the
// escapes we care about are tiny, and the scanner runs on every PTY chunk.
const maxOSCBytes = 4096

// ObservationKind classifies what the OutputObserver saw.
type ObservationKind string

const (
	ObservedProgress     ObservationKind = "progress"     // OSC 9;4;n
	ObservedNotification ObservationKind = "notification" // OSC 9;text, 777;notify, 99
	ObservedBell         ObservationKind = "bell"         // BEL outside an OSC
)

// Observation is one escape the agent wrote to its terminal.
type Observation struct {
	Kind ObservationKind
	// Progress is the OSC 9;4 state: 0 clear, 1 normal, 2 error, 3
	// indeterminate, 4 paused.
	Progress int
	// Title and Body are the notification texts; OSC 9 and BEL carry only Body
	// (empty for BEL). They are raw terminal text for a provider classifier to
	// match, never to store.
	Title, Body string
}

// Signal maps the busy and clear progress states to signals. Notifications
// and bells need a provider classifier, and error/paused progress says nothing
// about the turn, so those report false.
func (o Observation) Signal(at time.Time) (Signal, bool) {
	if o.Kind != ObservedProgress {
		return Signal{}, false
	}
	switch o.Progress {
	case 1, 3:
		return Signal{Kind: TerminalBusy, At: at, Source: SourceTerminal}, true
	case 0:
		return Signal{Kind: TerminalClear, At: at, Source: SourceTerminal}, true
	}
	return Signal{}, false
}

type scanState uint8

const (
	scanGround scanState = iota
	scanEsc              // saw ESC
	scanOSC              // inside OSC payload
	scanOSCEsc           // saw ESC inside an OSC: ST (ESC \) or a new sequence
)

// OutputObserver is a stateful byte scanner over the PTY stream. Sequences may
// be split across Observe calls. It is pure (no I/O) and not safe for
// concurrent use; the PTY read loop is its only caller.
type OutputObserver struct {
	state    scanState
	payload  []byte
	overflow bool
}

// Observe scans p and returns what it found, in order. It never keeps p.
func (o *OutputObserver) Observe(p []byte) []Observation {
	var out []Observation
	for _, c := range p {
		switch o.state {
		case scanGround:
			switch c {
			case 0x1b:
				o.state = scanEsc
			case 0x07:
				out = append(out, Observation{Kind: ObservedBell})
			}
		case scanEsc:
			o.state = scanGround
			switch c {
			case ']':
				o.startOSC()
			case 0x1b:
				o.state = scanEsc
			}
		case scanOSC:
			switch c {
			case 0x07: // BEL terminates an OSC and is not a bell of its own
				out = o.finishOSC(out)
			case 0x1b:
				o.state = scanOSCEsc
			case 0x18, 0x1a: // CAN and SUB abort the sequence
				o.reset()
			default:
				o.push(c)
			}
		case scanOSCEsc:
			switch c {
			case '\\': // ST
				out = o.finishOSC(out)
			case ']':
				// An OSC opened inside an unterminated one: the older payload is
				// garbage, the new sequence starts clean.
				o.startOSC()
			case 0x1b:
				// ESC ESC: the first aborts the OSC, the second opens a sequence.
				o.reset()
				o.state = scanEsc
			default:
				// Any other escape aborts the OSC.
				o.reset()
			}
		}
	}
	return out
}

func (o *OutputObserver) startOSC() {
	o.state = scanOSC
	o.payload = o.payload[:0]
	o.overflow = false
}

func (o *OutputObserver) reset() {
	o.state = scanGround
	o.payload = o.payload[:0]
	o.overflow = false
}

func (o *OutputObserver) push(c byte) {
	if o.overflow {
		return
	}
	if len(o.payload) >= maxOSCBytes {
		o.overflow = true
		o.payload = o.payload[:0]
		return
	}
	o.payload = append(o.payload, c)
}

func (o *OutputObserver) finishOSC(out []Observation) []Observation {
	overflow := o.overflow
	payload := string(o.payload)
	o.reset()
	if overflow {
		return out
	}
	if obs, ok := parseOSC(payload); ok {
		out = append(out, obs)
	}
	return out
}

// parseOSC understands the three notification dialects agents use plus the
// OSC 9;4 progress bar. Everything else (titles, hyperlinks, colours) is not
// ours.
func parseOSC(payload string) (Observation, bool) {
	code, rest, _ := strings.Cut(payload, ";")
	switch code {
	case "9":
		if sub, tail, found := strings.Cut(rest, ";"); sub == "4" {
			if !found {
				return Observation{}, false
			}
			n, _, _ := strings.Cut(tail, ";")
			state, err := strconv.Atoi(n)
			if err != nil || state < 0 || state > 4 {
				return Observation{}, false
			}
			return Observation{Kind: ObservedProgress, Progress: state}, true
		}
		return Observation{Kind: ObservedNotification, Body: validText(rest)}, true
	case "777":
		parts := strings.SplitN(rest, ";", 3)
		if len(parts) < 2 || parts[0] != "notify" {
			return Observation{}, false
		}
		obs := Observation{Kind: ObservedNotification, Title: validText(parts[1])}
		if len(parts) == 3 {
			obs.Body = validText(parts[2])
		}
		return obs, true
	case "99":
		// Kitty: 99;key=value:key=value;payload. The payload is the title unless
		// p=body. Chunked notifications (d=0) are not reassembled: the first
		// chunk is enough for a classifier to match on.
		meta, text, _ := strings.Cut(rest, ";")
		obs := Observation{Kind: ObservedNotification}
		if strings.Contains(":"+meta+":", ":p=body:") {
			obs.Body = validText(text)
		} else {
			obs.Title = validText(text)
		}
		return obs, true
	}
	return Observation{}, false
}

func validText(s string) string { return strings.ToValidUTF8(s, "") }
