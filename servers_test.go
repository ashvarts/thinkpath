package main

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestListServers(t *testing.T) {
	t.Setenv("THINKPATH_SERVERS", t.TempDir())

	servers, err := listServers()
	if err != nil || len(servers) != 0 {
		t.Fatalf("empty registry: got %v, %v", servers, err)
	}

	now := time.Now()
	unregister, err := registerServer(ServerInfo{PID: os.Getpid(), Addr: "127.0.0.1:1", StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	// A process that has already exited stands in for a killed server
	// that never cleaned up after itself.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := registerServer(ServerInfo{PID: dead.Process.Pid, Addr: "127.0.0.1:2", StartedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	servers, err = listServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Addr != "127.0.0.1:1" {
		t.Fatalf("want only the live server, got %+v", servers)
	}

	// The stale entry should have been cleaned up, not just hidden.
	dir, _ := serversDir()
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("want 1 file left in the registry, got %d", len(entries))
	}

	unregister()
	if servers, _ = listServers(); len(servers) != 0 {
		t.Errorf("want no servers after unregister, got %+v", servers)
	}
}
