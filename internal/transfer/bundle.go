// Package transfer provides archive-based file bundling and extraction
// for pushing configuration to remote hosts in a single transfer.
package transfer

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileEntry describes a file to include in a bundle.
type FileEntry struct {
	LocalPath  string
	RemotePath string // relative path on the remote (forward-slash separated)
	Mode       fs.FileMode
}

// CollectFiles walks a local directory and returns FileEntry items with
// RemotePath set relative to remotePrefix. Only regular files are included.
func CollectFiles(localDir, remotePrefix string) ([]FileEntry, error) {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return nil, err
	}
	var files []FileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileEntry{
			LocalPath:  filepath.Join(localDir, e.Name()),
			RemotePath: remotePrefix + e.Name(),
			Mode:       info.Mode(),
		})
	}
	return files, nil
}

// CreateBundle writes a gzip-compressed tar archive of the given files to w.
// File contents are read from disk at LocalPath; archive entries use
// RemotePath as the tar header name (forward-slash separated).
func CreateBundle(w io.Writer, files []FileEntry) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for _, f := range files {
		if err := addFile(tw, f); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return fmt.Errorf("adding %s: %w", f.RemotePath, err)
		}
	}

	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return fmt.Errorf("closing tar: %w", err)
	}
	return gw.Close()
}

// CreateBundleFromBytes is like CreateBundle but takes in-memory content
// instead of reading from disk. Useful for generated config files.
func CreateBundleFromBytes(w io.Writer, entries map[string][]byte, mode fs.FileMode) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for name, data := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: int64(mode),
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return err
		}
		if _, err := tw.Write(data); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return err
		}
	}

	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

// CreateBundleMixed writes a tar.gz containing one in-memory file (the
// generated config) plus a set of disk files. This avoids writing the
// config to a temp file just to tar it.
func CreateBundleMixed(w io.Writer, configData []byte, configPath string, files []FileEntry) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: configPath,
		Mode: 0644,
		Size: int64(len(configData)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(configData); err != nil {
		return err
	}

	for _, f := range files {
		if err := addFile(tw, f); err != nil {
			_ = tw.Close()
			_ = gw.Close()
			return fmt.Errorf("adding %s: %w", f.RemotePath, err)
		}
	}

	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

func addFile(tw *tar.Writer, f FileEntry) error {
	file, err := os.Open(f.LocalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	mode := f.Mode
	if mode == 0 {
		mode = info.Mode()
	}

	hdr := &tar.Header{
		Name: f.RemotePath,
		Mode: int64(mode),
		Size: info.Size(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, file)
	return err
}

// ExtractBundle reads a gzip-compressed tar archive from r and extracts
// files into baseDir. Directory entries are created as needed. Paths are
// validated to prevent directory traversal attacks.
func ExtractBundle(r io.Reader, baseDir string) (int, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	count := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("tar: %w", err)
		}

		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return count, fmt.Errorf("path traversal blocked: %s", hdr.Name)
		}

		target := filepath.Join(baseDir, filepath.FromSlash(clean))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return count, err
			}
		case tar.TypeReg, 0:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return count, err
			}
			mode := fs.FileMode(hdr.Mode) & 0777
			if mode == 0 {
				mode = 0644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return count, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return count, err
			}
			f.Close()
			count++
		}
	}

	return count, nil
}
