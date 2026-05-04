package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const maxUnixSocketPathLen = len(syscall.RawSockaddrUnix{}.Path) - 1

var unixSocketCWDMu sync.Mutex

func listenUnixSocket(path string) (net.Listener, error) {
	var l net.Listener
	err := withUnixSocketAddr(path, func(addr string) error {
		var err error
		l, err = net.Listen("unix", addr)
		return err
	})
	return l, err
}

func dialUnixSocket(path string) (net.Conn, error) {
	var c net.Conn
	err := withUnixSocketAddr(path, func(addr string) error {
		var err error
		c, err = net.Dial("unix", addr)
		return err
	})
	return c, err
}

func dialUnixSocketTimeout(path string, timeout time.Duration) (net.Conn, error) {
	var c net.Conn
	err := withUnixSocketAddr(path, func(addr string) error {
		var err error
		c, err = net.DialTimeout("unix", addr, timeout)
		return err
	})
	return c, err
}

func withUnixSocketAddr(path string, fn func(addr string) error) error {
	if len(path) <= maxUnixSocketPathLen {
		return fn(path)
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("unix socket path too long: %s", path)
	}
	dir, base := filepath.Split(path)
	if base == "" || len(base) > maxUnixSocketPathLen {
		return fmt.Errorf("unix socket path too long: %s", path)
	}

	unixSocketCWDMu.Lock()
	defer unixSocketCWDMu.Unlock()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("chdir %s: %w", dir, err)
	}
	defer os.Chdir(cwd)

	return fn(base)
}
