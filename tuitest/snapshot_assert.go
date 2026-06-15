package tuitest

import (
	"flag"
	"path/filepath"

	"github.com/paulsmith/twee/internal/snapshot"
)

var updateSnapshots = flag.Bool("tuitest-update", false,
	"update tuitest snapshots instead of comparing")

// ExpectTextSnapshot compares the current visible text against
// testdata/snapshots/<test>/<name>.txt. With -tuitest-update the file
// is overwritten.
func (te *Term) ExpectTextSnapshot(name string) {
	t := te.requireTestingTB("ExpectTextSnapshot")
	testName := filepath.Base(t.Name())
	path := filepath.Join("testdata", "snapshots", testName, name+".txt")
	actual := te.VisibleText()
	res, err := snapshot.CompareText(path, actual, *updateSnapshots)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Updated {
		t.Logf("snapshot updated: %s", res.Path)
	}
}
