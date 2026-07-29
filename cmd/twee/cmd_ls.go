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
List running daemons. Each entry has the same shape as "twee status".
Entries whose socket cannot be reached are silently omitted.`)
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
	var (
		mu  sync.Mutex
		out = []result{}
		wg  sync.WaitGroup
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sock")
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			path := filepath.Join(dir, name+".sock")
			c, err := dialUnixSocketTimeout(path, 500*time.Millisecond)
			if err != nil {
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
		}(name)
	}
	wg.Wait()
	emitOK(out)
}
