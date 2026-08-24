package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/paulsmith/twee/internal/rpc"
)

const (
	envManaged      = "TWEE_MANAGED"
	envNestingDepth = "TWEE_NESTING_DEPTH"
	envParent       = "TWEE_PARENT_SESSION"
	envCapacityDir  = "TWEE_CAPACITY_DIR"

	maxManagedNestingDepth = 3
)

type managedContext struct {
	managed     bool
	depth       int
	parent      string
	capacityDir string
}

type preparedManagedChild struct {
	Depth         int
	ParentSession string
	CapacityDir   string
}

type launchError struct {
	code    string
	message string
	details map[string]any
}

func (e *launchError) Error() string { return e.message }

func launchErrorCode(err error) string {
	var launchErr *launchError
	if errors.As(err, &launchErr) {
		return launchErr.code
	}
	return rpc.CodeIO
}

func managedContextFromEnv() (managedContext, error) {
	_, managed := os.LookupEnv(envManaged)
	if !managed {
		return managedContext{}, nil
	}
	ctx := managedContext{managed: true, depth: 1, parent: os.Getenv(envParent), capacityDir: os.Getenv(envCapacityDir)}
	if raw, ok := os.LookupEnv(envNestingDepth); ok {
		depth, err := strconv.Atoi(raw)
		if err != nil || depth <= 0 {
			return managedContext{}, nestedContextError("invalid managed nesting depth", ctx)
		}
		ctx.depth = depth
	}
	if ctx.capacityDir == "" || !filepath.IsAbs(ctx.capacityDir) {
		return managedContext{}, nestedContextError("invalid managed capacity directory", ctx)
	}
	return ctx, nil
}

func prepareManagedChild(allowNested bool, childParent string) (preparedManagedChild, error) {
	ctx, err := managedContextFromEnv()
	if err != nil {
		return preparedManagedChild{}, err
	}
	depth := 1
	capacityDir := ""
	if ctx.managed {
		if !allowNested {
			return preparedManagedChild{}, nestedContextError("refusing to start a nested session", ctx)
		}
		if ctx.depth >= maxManagedNestingDepth {
			return preparedManagedChild{}, nestedContextError("managed nesting depth limit reached", ctx)
		}
		depth = ctx.depth + 1
		capacityDir = ctx.capacityDir
	} else {
		capacityDir, err = stateDir()
		if err != nil {
			return preparedManagedChild{}, &launchError{code: rpc.CodeIO, message: err.Error()}
		}
		capacityDir, err = filepath.Abs(capacityDir)
		if err != nil {
			return preparedManagedChild{}, &launchError{code: rpc.CodeIO, message: fmt.Sprintf("resolve capacity directory: %v", err)}
		}
	}
	return preparedManagedChild{Depth: depth, ParentSession: childParent, CapacityDir: capacityDir}, nil
}

func nestedContextError(message string, ctx managedContext) error {
	details := map[string]any{"depth": ctx.depth}
	if ctx.parent != "" {
		details["parent"] = ctx.parent
	}
	return &launchError{code: rpc.CodeNestedSession, message: message, details: details}
}
