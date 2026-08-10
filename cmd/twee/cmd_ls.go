package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/paulsmith/twee/internal/rpc"
)

func init() {
	register("ls", runLs)
	registerUsage("ls", `twee ls
List running daemons. Each live entry has the same shape as "twee
status" plus "name". A session whose daemon died without cleaning up
(e.g. killed with SIGKILL) is listed too, instead of being silently
omitted, as {"name": ..., "running": false, "stale": true}.`)
}

func runLs(args []string) {
	var opts struct{}
	if err := parseArg("ls", &opts, args); err != nil {
		fatalUsage("ls: %v", err)
	}
	dir, err := stateDir()
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		emitError(rpc.CodeIO, err.Error(), nil, 1)
	}
	type result struct {
		Name string `json:"name"`
		rpc.StatusData
	}
	type staleResult struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
		Stale   bool   `json:"stale"`
	}
	var (
		mu  sync.Mutex
		out = []any{}
		wg  sync.WaitGroup
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sock")
		wg.Go(func() {
			path := filepath.Join(dir, name+".sock")
			c, err := dialUnixSocketTimeout(path, 500*time.Millisecond)
			if err != nil {
				// The socket file exists (we're iterating its dir
				// entry) but nothing answers. If the lock proves no
				// live daemon owns it, report it rather than silently
				// dropping it.
				if isSessionStale(name) {
					mu.Lock()
					out = append(out, staleResult{Name: name, Running: false, Stale: true})
					mu.Unlock()
				}
				return
			}
			_ = c.Close()
			resp, err := callDaemon(name, rpc.OpStatus, nil)
			if err != nil || !resp.OK {
				return
			}
			var sd rpc.StatusData
			if err := json.Unmarshal(resp.Data, &sd); err != nil {
				return
			}
			mu.Lock()
			out = append(out, result{Name: name, StatusData: sd})
			mu.Unlock()
		})
	}
	wg.Wait()
	emitOK(out)
}
