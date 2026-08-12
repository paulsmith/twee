package main

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnixSocketHelpersHandleLongDirectory(t *testing.T) {
	dir := t.TempDir()
	for len(filepath.Join(dir, "test.sock")) <= maxUnixSocketPathLen+20 {
		dir = filepath.Join(dir, strings.Repeat("a", 20))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir long dir: %v", err)
	}
	sock := filepath.Join(dir, "test.sock")

	l, err := listenUnixSocket(sock)
	if err != nil {
		t.Fatalf("listenUnixSocket: %v", err)
	}
	defer func() { _ = l.Close() }()

	done := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		_ = c.Close()
		done <- nil
	}()

	c, err := dialUnixSocketTimeout(sock, time.Second)
	if err != nil {
		t.Fatalf("dialUnixSocketTimeout: %v", err)
	}
	_ = c.Close()

	if err := <-done; err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatalf("accept: %v", err)
	}
}
