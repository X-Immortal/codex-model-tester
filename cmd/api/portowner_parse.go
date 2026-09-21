package main

import (
	"encoding/csv"
	"net"
	"strconv"
	"strings"
)

// parseLsofOwners reads `lsof -Fpc` field output, where "p" starts a process
// record and "c" names that process.
func parseLsofOwners(output string) []portOccupant {
	var owners []portOccupant
	current := portOccupant{}
	flush := func() {
		if current.PID > 0 || current.Name != "" {
			owners = append(owners, current)
		}
		current = portOccupant{}
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			flush()
			pid, err := strconv.Atoi(line[1:])
			if err != nil {
				continue
			}
			current.PID = pid
		case 'c':
			current.Name = line[1:]
		}
	}
	flush()
	return owners
}

// parseNetstatListeners returns the process IDs of TCP listeners bound to port
// in `netstat -ano -p tcp` output. The state column stays English even on
// localized Windows builds; when it cannot be read the caller falls back to a
// generic message rather than guessing.
func parseNetstatListeners(output, port string) []int {
	var pids []int
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		if netstatPort(fields[1]) != port {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil || pid <= 0 || containsInt(pids, pid) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// netstatPort reads the port from a netstat local address such as
// "0.0.0.0:8080" or "[::]:8080".
func netstatPort(address string) string {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	return port
}

// parseTasklistCSV maps process IDs to image names in
// `tasklist /FO CSV /NH` output.
func parseTasklistCSV(output string) map[int]string {
	names := make(map[int]string)
	reader := csv.NewReader(strings.NewReader(output))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return names
	}
	for _, record := range records {
		if len(record) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil || pid <= 0 {
			continue
		}
		names[pid] = strings.TrimSpace(record[0])
	}
	return names
}

func containsInt(values []int, candidate int) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
