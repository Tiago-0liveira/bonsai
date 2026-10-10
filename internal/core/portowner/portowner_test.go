package portowner

import (
	"net"
	"os"
	"runtime"
	"testing"
)

func TestParseProcNetTCP(t *testing.T) {
	const table = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1B59 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 424242 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1B59 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 515151 1 0000000000000000 20 4 30 10 -1
   2: 0100007F:1B5A 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 616161 1 0000000000000000 100 0 0 10 0
`
	got := parseProcNetTCP(table, 7001)
	if len(got) != 1 || got[0] != "424242" {
		t.Fatalf("inodes = %v", got)
	}
	if got := parseProcNetTCP(table, 7003); len(got) != 0 {
		t.Fatalf("unexpected inodes for free port: %v", got)
	}
}

func TestParseLsof(t *testing.T) {
	owner, ok := parseLsof("p4242\ncnode\nf23\n")
	if !ok || owner.PID != 4242 || owner.Name != "node" {
		t.Fatalf("owner = %+v, %v", owner, ok)
	}
	if _, ok := parseLsof(""); ok {
		t.Fatal("empty output produced an owner")
	}
}

func TestParseNetstatAndTasklist(t *testing.T) {
	const netstat = `
Active Connections

  Proto  Local Address          Foreign Address        State           PID
  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1000
  TCP    127.0.0.1:7001         127.0.0.1:51234        ESTABLISHED     5555
  TCP    127.0.0.1:7001         0.0.0.0:0              LISTENING       4242
  TCP    127.0.0.1:17001        0.0.0.0:0              LISTENING       9999
`
	pid, ok := parseNetstat(netstat, 7001)
	if !ok || pid != 4242 {
		t.Fatalf("pid = %d, %v", pid, ok)
	}
	if name := parseTasklist(`"node.exe","4242","Console","1","52,124 K"` + "\r\n"); name != "node" {
		t.Fatalf("name = %q", name)
	}
	if name := parseTasklist("INFO: No tasks are running which match the specified criteria.\r\n"); name != "" {
		t.Fatalf("name for no match = %q", name)
	}
}

func TestFindOwnListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	owner, ok := Find(ln.Addr().(*net.TCPAddr).Port)
	if !ok {
		if runtime.GOOS == "linux" {
			t.Fatal("own listener not found via /proc")
		}
		t.Skip("port owner lookup unavailable on this runner")
	}
	if owner.PID != os.Getpid() {
		t.Fatalf("owner = %+v, want pid %d", owner, os.Getpid())
	}
}
