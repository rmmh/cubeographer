package region

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	zipCache   = make(map[string]*zip.ReadCloser)
	zipCacheMu sync.RWMutex
)

// SplitZipPath splits a path into the zip archive path and the internal path inside the zip,
// returning isZip=true if the path refers to a file or folder inside a zip archive.
func SplitZipPath(path string) (zipPath string, internalPath string, isZip bool) {
	// Clean the path to standard format
	path = filepath.ToSlash(path)
	idx := strings.Index(path, ".zip")
	if idx == -1 {
		return "", "", false
	}
	endIdx := idx + 4
	if endIdx == len(path) {
		return path, "", true
	}
	nextChar := path[endIdx]
	if nextChar == '/' {
		return path[:endIdx], path[endIdx+1:], true
	}
	return "", "", false
}

// GetZipReader returns a cached zip.ReadCloser for the given zip file path.
// It is thread-safe and opens each zip file at most once.
func GetZipReader(zipPath string) (*zip.ReadCloser, error) {
	zipCacheMu.RLock()
	rc, ok := zipCache[zipPath]
	zipCacheMu.RUnlock()
	if ok {
		return rc, nil
	}

	zipCacheMu.Lock()
	defer zipCacheMu.Unlock()
	// Double-check
	if rc, ok = zipCache[zipPath]; ok {
		return rc, nil
	}

	opened, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	zipCache[zipPath] = opened
	return opened, nil
}

// OpenZipFile opens a file inside a zip archive and returns an io.ReadCloser.
func OpenZipFile(zipPath string, internalPath string) (io.ReadCloser, error) {
	rc, err := GetZipReader(zipPath)
	if err != nil {
		return nil, err
	}
	internalPath = filepath.ToSlash(internalPath)
	internalPath = strings.TrimPrefix(internalPath, "/")

	for _, f := range rc.File {
		if filepath.ToSlash(f.Name) == internalPath {
			return f.Open()
		}
	}
	return nil, os.ErrNotExist
}

// OpenFile opens a file which might be inside a zip archive.
func OpenFile(path string) (io.ReadCloser, error) {
	zipPath, internalPath, isZip := SplitZipPath(path)
	if !isZip {
		return os.Open(path)
	}
	return OpenZipFile(zipPath, internalPath)
}

// OpenRegionFile opens a region file (which might be inside a zip) and returns an io.ReadSeeker and io.Closer.
func OpenRegionFile(path string) (io.ReadSeeker, io.Closer, error) {
	zipPath, internalPath, isZip := SplitZipPath(path)
	if !isZip {
		f, err := os.Open(path)
		return f, f, err
	}

	rc, err := OpenZipFile(zipPath, internalPath)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, err
	}

	return bytes.NewReader(data), io.NopCloser(nil), nil
}

type virtualDirEntry struct {
	name    string
	isDir   bool
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (v *virtualDirEntry) Name() string               { return v.name }
func (v *virtualDirEntry) IsDir() bool                { return v.isDir }
func (v *virtualDirEntry) Type() fs.FileMode          { return v.mode.Type() }
func (v *virtualDirEntry) Info() (fs.FileInfo, error) { return v, nil }

func (v *virtualDirEntry) Size() int64        { return v.size }
func (v *virtualDirEntry) Mode() fs.FileMode  { return v.mode }
func (v *virtualDirEntry) ModTime() time.Time { return v.modTime }
func (v *virtualDirEntry) Sys() any           { return nil }

// ReadDir is a zip-aware replacement for os.ReadDir
func ReadDir(dirPath string) ([]fs.DirEntry, error) {
	zipPath, internalPath, isZip := SplitZipPath(dirPath)
	if !isZip {
		return os.ReadDir(dirPath)
	}

	rc, err := GetZipReader(zipPath)
	if err != nil {
		return nil, err
	}

	zipStat, err := os.Stat(zipPath)
	var zipModTime time.Time
	if err == nil {
		zipModTime = zipStat.ModTime()
	}

	internalPath = filepath.ToSlash(internalPath)
	internalPath = strings.TrimPrefix(internalPath, "/")
	if internalPath != "" && !strings.HasSuffix(internalPath, "/") {
		internalPath += "/"
	}

	seen := make(map[string]bool)
	var entries []fs.DirEntry

	for _, f := range rc.File {
		name := filepath.ToSlash(f.Name)
		if !strings.HasPrefix(name, internalPath) {
			continue
		}
		rel := strings.TrimPrefix(name, internalPath)
		if rel == "" {
			continue
		}
		parts := strings.Split(rel, "/")
		childName := parts[0]
		if childName == "" {
			continue
		}

		if seen[childName] {
			continue
		}
		seen[childName] = true

		isDir := len(parts) > 1 || f.FileInfo().IsDir()
		var size int64
		var mode fs.FileMode
		if isDir {
			mode = fs.ModeDir | 0755
		} else {
			size = int64(f.UncompressedSize64)
			mode = 0644
		}

		entries = append(entries, &virtualDirEntry{
			name:    childName,
			isDir:   isDir,
			size:    size,
			mode:    mode,
			modTime: zipModTime,
		})
	}

	return entries, nil
}

// Stat is a zip-aware replacement for os.Stat
func Stat(path string) (fs.FileInfo, error) {
	zipPath, internalPath, isZip := SplitZipPath(path)
	if !isZip {
		return os.Stat(path)
	}

	rc, err := GetZipReader(zipPath)
	if err != nil {
		return nil, err
	}

	zipStat, err := os.Stat(zipPath)
	var zipModTime time.Time
	if err == nil {
		zipModTime = zipStat.ModTime()
	}

	internalPath = filepath.ToSlash(internalPath)
	internalPath = strings.TrimPrefix(internalPath, "/")

	if internalPath == "" {
		return &virtualDirEntry{
			name:    filepath.Base(zipPath),
			isDir:   true,
			mode:    fs.ModeDir | 0755,
			modTime: zipModTime,
		}, nil
	}

	for _, f := range rc.File {
		if filepath.ToSlash(f.Name) == internalPath {
			return f.FileInfo(), nil
		}
	}

	prefix := internalPath + "/"
	for _, f := range rc.File {
		if strings.HasPrefix(filepath.ToSlash(f.Name), prefix) {
			return &virtualDirEntry{
				name:    filepath.Base(internalPath),
				isDir:   true,
				mode:    fs.ModeDir | 0755,
				modTime: zipModTime,
			}, nil
		}
	}

	return nil, os.ErrNotExist
}
