package procstore

import (
	"encoding/json"
	"testing"
)

func TestPTYRecordRoundTripAndLegacyMode(t *testing.T) {
	if got := EffectiveIOMode(""); got != IOModePipe {
		t.Fatalf("legacy empty I/O mode = %q, want pipe", got)
	}
	r := Record{ID: 1, IOMode: IOModePTY, PTYCols: 120, PTYRows: 40}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.IOMode != IOModePTY || got.PTYCols != 120 || got.PTYRows != 40 {
		t.Fatalf("PTY metadata round-trip = %+v", got)
	}
}
