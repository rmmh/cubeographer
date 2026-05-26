package region

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSplitZipPath(t *testing.T) {
	tests := []struct {
		input     string
		wantZip   string
		wantInner string
		wantIs    bool
	}{
		{"/path/to/world.zip", "/path/to/world.zip", "", true},
		{"/path/to/world.zip/region", "/path/to/world.zip", "region", true},
		{"/path/to/world.zip/region/r.0.0.mca", "/path/to/world.zip", "region/r.0.0.mca", true},
		{"/path/to/normal/directory", "", "", false},
	}

	for _, tt := range tests {
		z, inner, ok := SplitZipPath(tt.input)
		if z != tt.wantZip || inner != tt.wantInner || ok != tt.wantIs {
			t.Errorf("SplitZipPath(%q) = (%q, %q, %t); want (%q, %q, %t)",
				tt.input, z, inner, ok, tt.wantZip, tt.wantInner, tt.wantIs)
		}
	}
}

func TestZipCacheAndHelpers(t *testing.T) {
	// Create an in-memory zip file
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add files to the archive
	files := []struct {
		name, body string
	}{
		{"level.dat", "level data"},
		{"region/r.0.0.mca", "region data"},
		{"region/poi/r.0.0.mca", "poi data"},
	}

	for _, file := range files {
		f, err := zw.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write([]byte(file.body))
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// Write zip to a temporary file in the workspace directory
	zipPath := "./test_world.zip"
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(zipPath)

	// Test ReadDir
	entries, err := ReadDir(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 root entries in zip, got %d", len(entries))
	}

	// Test ReadDir inside a subfolder
	regionEntries, err := ReadDir(zipPath + "/region")
	if err != nil {
		t.Fatal(err)
	}
	// "r.0.0.mca" and "poi" directory
	if len(regionEntries) != 2 {
		t.Errorf("expected 2 entries under region, got %d", len(regionEntries))
	}

	// Test Stat of a file
	info, err := Stat(zipPath + "/region/r.0.0.mca")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name() != "r.0.0.mca" || info.Size() != int64(len("region data")) || info.IsDir() {
		t.Errorf("unexpected stat info: name=%s, size=%d, isDir=%t", info.Name(), info.Size(), info.IsDir())
	}

	// Test Stat of a folder
	dirInfo, err := Stat(zipPath + "/region")
	if err != nil {
		t.Fatal(err)
	}
	if !dirInfo.IsDir() {
		t.Error("expected stat of /region to be a directory")
	}

	// Test OpenFile
	rc, err := OpenFile(zipPath + "/region/r.0.0.mca")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "region data" {
		t.Errorf("expected 'region data', got %q", string(content))
	}
}
