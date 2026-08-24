package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/paulsmith/twee/internal/rpc"
)

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestManagedContextMalformedDepthRejects(t *testing.T) {
	t.Setenv("TWEE_MANAGED", "")
	t.Setenv("TWEE_NESTING_DEPTH", "zero")
	if _, err := managedContextFromEnv(); err == nil || launchErrorCode(err) != rpc.CodeNestedSession {
		t.Fatalf("managedContextFromEnv() = %v, want NESTED_SESSION", err)
	}
}

func TestManagedContextEmptyDepthRejects(t *testing.T) {
	t.Setenv("TWEE_MANAGED", "1")
	t.Setenv("TWEE_NESTING_DEPTH", "")
	t.Setenv("TWEE_CAPACITY_DIR", t.TempDir())
	if _, err := managedContextFromEnv(); err == nil || launchErrorCode(err) != rpc.CodeNestedSession {
		t.Fatalf("managedContextFromEnv() = %v, want NESTED_SESSION", err)
	}
}

func TestPrepareManagedChildRejectsNestedLaunchWithoutOverride(t *testing.T) {
	t.Setenv("TWEE_MANAGED", "1")
	t.Setenv("TWEE_NESTING_DEPTH", "1")
	t.Setenv("TWEE_CAPACITY_DIR", t.TempDir())
	if _, err := prepareManagedChild(false, "ephemeral"); err == nil || launchErrorCode(err) != rpc.CodeNestedSession {
		t.Fatalf("prepareManagedChild() = %v, want NESTED_SESSION", err)
	}
}

func TestPrepareManagedChildRejectsDepthLimit(t *testing.T) {
	t.Setenv("TWEE_MANAGED", "1")
	t.Setenv("TWEE_NESTING_DEPTH", strconv.Itoa(maxManagedNestingDepth))
	t.Setenv("TWEE_CAPACITY_DIR", t.TempDir())
	if _, err := prepareManagedChild(true, "ephemeral"); err == nil || launchErrorCode(err) != rpc.CodeNestedSession {
		t.Fatalf("prepareManagedChild() = %v, want NESTED_SESSION", err)
	}
}

func TestPrepareManagedChildKeepsInheritedCapacityDomain(t *testing.T) {
	domain := t.TempDir()
	t.Setenv("TWEE_MANAGED", "1")
	t.Setenv("TWEE_NESTING_DEPTH", "1")
	t.Setenv("TWEE_CAPACITY_DIR", domain)
	child, err := prepareManagedChild(true, "ephemeral")
	if err != nil {
		t.Fatal(err)
	}
	if child.Depth != 2 || child.ParentSession != "ephemeral" || child.CapacityDir != domain {
		t.Fatalf("prepareManagedChild() = %#v", child)
	}
}

func TestPrepareManagedChildResolvesRootCapacityDomain(t *testing.T) {
	stateDir := t.TempDir()
	unsetEnv(t, "TWEE_MANAGED")
	t.Setenv("TWEE_NESTING_DEPTH", "")
	t.Setenv("TWEE_CAPACITY_DIR", "")
	t.Setenv("TWEE_STATE_DIR", stateDir)
	child, err := prepareManagedChild(false, "outer")
	if err != nil {
		t.Fatal(err)
	}
	if child.Depth != 1 || child.CapacityDir != stateDir || !filepath.IsAbs(child.CapacityDir) {
		t.Fatalf("prepareManagedChild() = %#v", child)
	}
}
