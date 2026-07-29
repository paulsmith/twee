package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateValidBundle(t *testing.T) {
	path := writeSyntheticTrace(t)
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !result.Valid {
		t.Fatalf("valid = false, issues = %v", result.Issues)
	}
	if result.Events != 5 { // 2 output + 1 input + 1 resize + 1 exit
		t.Errorf("events = %d, want 5", result.Events)
	}
	if len(result.Issues) != 0 {
		t.Errorf("issues = %v, want none", result.Issues)
	}
}

func TestValidateMissingFileIsIO(t *testing.T) {
	_, err := Validate(filepath.Join(t.TempDir(), "missing.twee"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
	var le *LoadError
	if !errors.As(err, &le) || le.Kind != ErrIO {
		t.Fatalf("error = %v, want *LoadError{Kind: ErrIO}", err)
	}
}

func TestValidateTruncatedZipReportsIssueNotError(t *testing.T) {
	// A real zip, truncated partway through — not even a parseable zip
	// container anymore.
	full := writeSyntheticTrace(t)
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated.twee")
	if err := os.WriteFile(truncated, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Validate(truncated)
	if err != nil {
		t.Fatalf("Validate: %v, want a reported issue instead of a hard error", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a truncated zip")
	}
	if !hasIssueContaining(result.Issues, "invalid zip") {
		t.Errorf("issues = %v, want one mentioning an invalid zip", result.Issues)
	}
}

func TestValidateGarbageManifest(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{not valid json`,
		"events.jsonl":  "",
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a garbage manifest")
	}
	if !hasIssueContaining(result.Issues, "manifest.json") {
		t.Errorf("issues = %v, want one mentioning manifest.json", result.Issues)
	}
}

func TestValidateUnsupportedVersion(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl":  "",
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for an unsupported version")
	}
	if !hasIssueContaining(result.Issues, "unsupported bundle version 2") {
		t.Errorf("issues = %v, want one naming the unsupported version", result.Issues)
	}
}

func TestValidateMissingManifestOrEvents(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"events.jsonl": "",
	})
	result, _ := Validate(path)
	if result.Valid || !hasIssueContaining(result.Issues, "missing manifest.json") {
		t.Errorf("issues = %v, want missing manifest.json", result.Issues)
	}

	path2 := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
	})
	result2, _ := Validate(path2)
	if result2.Valid || !hasIssueContaining(result2.Issues, "missing events.jsonl") {
		t.Errorf("issues = %v, want missing events.jsonl", result2.Issues)
	}
}

func TestValidateCorruptEventsLine(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"output"}`,
			`{this is not json`,
			`{"t_ms":10,"type":"exit","code":0}`,
		}, "\n"),
	})
	result, err := Validate(path)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Valid {
		t.Fatal("valid = true, want false for a corrupt events line")
	}
	if !hasIssueContaining(result.Issues, "events.jsonl line 2") {
		t.Errorf("issues = %v, want one naming line 2", result.Issues)
	}
	// The two lines that did parse should still be counted.
	if result.Events != 2 {
		t.Errorf("events = %d, want 2 (the malformed line doesn't count)", result.Events)
	}
}

func TestValidateUnknownEventType(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl":  `{"t_ms":0,"type":"teleport"}`,
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false for an unknown event type")
	}
	if !hasIssueContaining(result.Issues, `unknown event type "teleport"`) {
		t.Errorf("issues = %v, want one naming the unknown type", result.Issues)
	}
}

func TestValidateOutOfOrderTimestamps(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":1,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":100,"type":"output"}`,
			`{"t_ms":50,"type":"output"}`,
		}, "\n"),
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false for out-of-order timestamps")
	}
	if !hasIssueContaining(result.Issues, "timestamp 50 before previous 100") {
		t.Errorf("issues = %v, want one naming the out-of-order timestamps", result.Issues)
	}
}

// TestValidateCollectsMultipleIssues pins down that Validate doesn't
// stop at the first problem: a bundle with several independent defects
// should report all of them in one call.
func TestValidateCollectsMultipleIssues(t *testing.T) {
	path := writeRawZip(t, map[string]string{
		"manifest.json": `{"version":2,"cols":10,"rows":3}`,
		"events.jsonl": strings.Join([]string{
			`{"t_ms":0,"type":"teleport"}`,
			`{bad json`,
		}, "\n"),
	})
	result, _ := Validate(path)
	if result.Valid {
		t.Fatal("valid = true, want false")
	}
	if len(result.Issues) < 3 {
		t.Fatalf("issues = %v, want at least 3 (version, unknown type, bad json line)", result.Issues)
	}
}

func hasIssueContaining(issues []string, substr string) bool {
	for _, i := range issues {
		if strings.Contains(i, substr) {
			return true
		}
	}
	return false
}
