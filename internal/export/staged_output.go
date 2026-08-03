package export

import (
	"os"
	"path/filepath"
	"strings"
)

// stagedOutput writes an artifact beside its destination so Commit can replace
// the destination atomically on filesystems supported by os.Rename.
type stagedOutput struct {
	destination string
	temporary   string
}

// newStagedOutput creates a temporary artifact beside destination. suffix is
// retained after the temporary marker, allowing external encoders to infer the
// output format from the temporary filename.
func newStagedOutput(destination, suffix string) (*stagedOutput, *os.File, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return nil, nil, err
	}
	base := strings.TrimSuffix(filepath.Base(abs), suffix)
	f, err := os.CreateTemp(filepath.Dir(abs), "."+base+".*.tmp"+suffix)
	if err != nil {
		return nil, nil, err
	}
	return &stagedOutput{destination: abs, temporary: f.Name()}, f, nil
}

func (s *stagedOutput) commit() error {
	if err := preserveDestinationMode(s.temporary, s.destination); err != nil {
		return err
	}
	if err := os.Rename(s.temporary, s.destination); err != nil {
		return err
	}
	s.temporary = ""
	return nil
}

func (s *stagedOutput) abort() {
	if s.temporary != "" {
		_ = os.Remove(s.temporary)
		s.temporary = ""
	}
}
