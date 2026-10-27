package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
)

// ServerInfo describes one running `thinkpath serve` process. Each server
// writes one of these files on startup so that `thinkpath servers list` can
// find it without having to scan ports.
type ServerInfo struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	Data      string    `json:"data"`
	StartedAt time.Time `json:"started_at"`
}

// serversDir is where servers register themselves. It follows
// THINKPATH_SERVERS if set, which keeps tests out of the real home
// directory; otherwise it is ~/.thinkpath/servers, next to the default data
// file.
func serversDir() (string, error) {
	if d := os.Getenv("THINKPATH_SERVERS"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".thinkpath", "servers"), nil
}

// registerServer records info in the registry and returns a function that
// removes the record again. A server that is killed without running it
// leaves a stale file behind; listServers drops those by checking the PID.
func registerServer(info ServerInfo) (unregister func(), err error) {
	dir, err := serversDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	b, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, strconv.Itoa(info.PID)+".json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return nil, err
	}
	return func() { os.Remove(path) }, nil
}

// listServers returns the servers that are still running, oldest first.
func listServers() ([]ServerInfo, error) {
	dir, err := serversDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var servers []ServerInfo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var info ServerInfo
		if json.Unmarshal(b, &info) != nil {
			continue
		}
		if !processAlive(info.PID) {
			os.Remove(path)
			continue
		}
		servers = append(servers, info)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].StartedAt.Before(servers[j].StartedAt) })
	return servers, nil
}

// processAlive reports whether a process with this PID exists. Signal 0
// sends nothing; it only checks. EPERM means the process exists but belongs
// to someone else.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

func runServerList() error {
	servers, err := listServers()
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		fmt.Println("no thinkpath servers running")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tADDRESS\tSTARTED\tDATA")
	for _, s := range servers {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", s.PID, s.Addr, s.StartedAt.Local().Format("2006-01-02 15:04:05"), s.Data)
	}
	return tw.Flush()
}
