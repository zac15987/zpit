package sessionsync

import (
	"encoding/json"
	"reflect"
	"runtime"
	"testing"
)

// sampleManifest returns a fully-populated Manifest with all fields set,
// suitable for round-trip testing.
func sampleManifest() *Manifest {
	return &Manifest{
		FormatVersion:    "1",
		SourceOS:         "linux",
		SourceCwd:        "/home/alice/projects/myapp",
		SourceEncodedCwd: "-home-alice-projects-myapp",
		Sessions:         []string{"session-abc123", "session-def456"},
		ExportedAt:       "2026-04-26T10:00:00Z",
		IncludeMemory:    true,
	}
}

// TestManifestRoundTrip verifies that marshalling a fully-populated Manifest
// and then unmarshalling the result produces a value that is field-for-field
// identical to the original.
func TestManifestRoundTrip(t *testing.T) {
	original := sampleManifest()

	data, err := MarshalManifest(original)
	if err != nil {
		t.Fatalf("MarshalManifest: unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("MarshalManifest: returned empty bytes")
	}

	got, err := UnmarshalManifest(data)
	if err != nil {
		t.Fatalf("UnmarshalManifest: unexpected error: %v", err)
	}

	if !reflect.DeepEqual(original, got) {
		t.Errorf("round-trip mismatch:\n  want: %+v\n   got: %+v", original, got)
	}
}

// TestUnmarshalRejectsInvalidJSON confirms that UnmarshalManifest returns a
// non-nil error when given syntactically invalid JSON bytes.
func TestUnmarshalRejectsInvalidJSON(t *testing.T) {
	garbage := []byte(`{this is not valid json}`)
	m, err := UnmarshalManifest(garbage)
	if err == nil {
		t.Fatalf("expected an error for invalid JSON, got nil (manifest: %+v)", m)
	}
}

// TestDetectSourceOS asserts that DetectSourceOS returns one of the three
// expected canonical values or, for an unknown GOOS, matches runtime.GOOS
// directly.
func TestDetectSourceOS(t *testing.T) {
	got := DetectSourceOS()
	if got == "" {
		t.Fatal("DetectSourceOS returned empty string")
	}

	knownValues := map[string]bool{
		"windows": true,
		"linux":   true,
		"darwin":  true,
	}

	if knownValues[runtime.GOOS] {
		// On a known platform the return must be the canonical value.
		if got != runtime.GOOS {
			t.Errorf("DetectSourceOS = %q, want %q", got, runtime.GOOS)
		}
	} else {
		// On an unknown platform the verbatim GOOS value must be returned.
		if got != runtime.GOOS {
			t.Errorf("DetectSourceOS = %q for unknown GOOS %q, want verbatim GOOS", got, runtime.GOOS)
		}
	}
}

// TestMarshalManifestProducesValidJSON verifies that MarshalManifest output is
// valid JSON containing all seven expected keys defined by the manifest schema.
func TestMarshalManifestProducesValidJSON(t *testing.T) {
	m := sampleManifest()

	data, err := MarshalManifest(m)
	if err != nil {
		t.Fatalf("MarshalManifest: unexpected error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput:\n%s", err, data)
	}

	expectedKeys := []string{
		"format_version",
		"source_os",
		"source_cwd",
		"source_encoded_cwd",
		"sessions",
		"exported_at",
		"include_memory",
	}

	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q not found in marshalled JSON\noutput:\n%s", key, data)
		}
	}
}
