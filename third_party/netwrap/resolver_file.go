package netwrap

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func createResolverSource(dir string, contents []byte) (string, func(), error) {
	file, err := os.CreateTemp(dir, "netwrap-resolv-*")
	if err != nil {
		return "", nil, fmt.Errorf("create private resolver file: %w", err)
	}
	path := file.Name()
	remove := func() { _ = os.Remove(path) }
	fail := func(action string, err error) (string, func(), error) {
		return "", nil, errors.Join(
			fmt.Errorf("%s private resolver file: %w", action, err),
			file.Close(),
			os.Remove(path),
		)
	}
	if err := file.Chmod(0o600); err != nil {
		return fail("protect", err)
	}
	written, err := file.Write(contents)
	if err != nil {
		return fail("write", err)
	}
	if written != len(contents) {
		return fail("write", io.ErrShortWrite)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("close private resolver file: %w", err)
	}
	return path, remove, nil
}
