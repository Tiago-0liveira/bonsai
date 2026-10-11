// Package portowner names the process listening on a loopback TCP port, so a
// port conflict can say "pid 4242, node" instead of only "in use". Lookups are
// best effort: when the OS hides the owner (another user, missing tools), Find
// reports false and callers fall back to a generic message.
package portowner

import (
	"bufio"
	"encoding/csv"
	"strconv"
	"strings"
)

// Owner is the process holding a listening socket.
type Owner struct {
	PID  int
	Name string
}

// Find returns the process listening on TCP port, if the OS lets us see it.
func Find(port int) (Owner, bool) {
	if port < 1 || port > 65535 {
		return Owner{}, false
	}
	return find(port)
}

// parseProcNetTCP returns the socket inodes listening on port in the content
// of /proc/net/tcp or /proc/net/tcp6.
func parseProcNetTCP(content string, port int) []string {
	var inodes []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// sl local_address rem_address st tx:rx tr:when retrnsmt uid timeout inode
		if len(fields) < 10 || fields[3] != "0A" { // 0A = TCP_LISTEN
			continue
		}
		colon := strings.LastIndexByte(fields[1], ':')
		if colon < 0 {
			continue
		}
		p, err := strconv.ParseInt(fields[1][colon+1:], 16, 32)
		if err != nil || int(p) != port {
			continue
		}
		if fields[9] != "0" {
			inodes = append(inodes, fields[9])
		}
	}
	return inodes
}

// parseLsof reads `lsof -Fpc` output: "p<pid>" then "c<command>" lines.
func parseLsof(output string) (Owner, bool) {
	var owner Owner
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			if owner.PID != 0 {
				return owner, true
			}
			pid, err := strconv.Atoi(line[1:])
			if err != nil {
				continue
			}
			owner.PID = pid
		case 'c':
			if owner.PID != 0 && owner.Name == "" {
				owner.Name = line[1:]
			}
		}
	}
	return owner, owner.PID != 0
}

// parseNetstat finds the PID listening on port in `netstat -ano -p TCP`.
func parseNetstat(output string, port int) (int, bool) {
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 || !strings.EqualFold(fields[0], "TCP") || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if !strings.HasSuffix(fields[1], suffix) {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err == nil && pid > 0 {
			return pid, true
		}
	}
	return 0, false
}

// parseTasklist reads the image name from `tasklist /FO CSV /NH` output.
func parseTasklist(output string) string {
	records, err := csv.NewReader(strings.NewReader(strings.TrimSpace(output))).ReadAll()
	if err != nil || len(records) == 0 || len(records[0]) < 2 {
		return ""
	}
	return strings.TrimSuffix(records[0][0], ".exe")
}
