package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AssetName returns the expected release archive filename for the given OS and architecture.
func AssetName(goos, arch string) (string, error) {
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("unsupported platform %s/%s", goos, arch)
	}
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return "bonsai_" + goos + "_" + arch + ext, nil
}

// IsPermissionError checks whether an error is caused by filesystem permission denials.
func IsPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) || errors.Is(err, os.ErrPermission) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "access is denied")
}

func verify(data, sums []byte, name string) error {
	var expected string
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if expected != "" {
				return fmt.Errorf("duplicate checksum")
			}
			expected = f[0]
		}
	}
	decoded, e := hex.DecodeString(expected)
	if e != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("missing or invalid checksum for %s", name)
	}
	actual := sha256.Sum256(data)
	if !bytes.Equal(decoded, actual[:]) {
		return fmt.Errorf("SHA256 mismatch for %s", name)
	}
	return nil
}

func extract(data []byte, goos string) ([]byte, error) {
	name := "bonsai"
	if goos == "windows" {
		name += ".exe"
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range z.File {
			if f.Name == name && f.Mode().IsRegular() {
				r, e := f.Open()
				if e != nil {
					return nil, e
				}
				defer r.Close()
				return readBinary(r)
			}
		}
	} else {
		z, e := gzip.NewReader(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		defer z.Close()
		t := tar.NewReader(z)
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.Name == name && h.Typeflag == tar.TypeReg {
				return readBinary(t)
			}
		}
	}
	return nil, fmt.Errorf("archive contains no %s", name)
}

func readBinary(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, maxDownload+1))
	if e != nil {
		return nil, e
	}
	if len(b) == 0 || len(b) > maxDownload {
		return nil, fmt.Errorf("invalid binary size")
	}
	return b, nil
}

// replace safely swaps the target executable with the new binary in-place.
func replace(target string, binary []byte) error {
	info, e := os.Stat(target)
	if e != nil {
		if IsPermissionError(e) {
			return fmt.Errorf("permission denied. Please run 'sudo bonsai --update' or update via your package manager: %w", e)
		}
		return e
	}

	// An exclusive lock prevents concurrent invocations from replacing each other.
	lock, e := os.OpenFile(target+".update-lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		if IsPermissionError(e) {
			return fmt.Errorf("permission denied. Please run 'sudo bonsai --update' or update via your package manager: %w", e)
		}
		return fmt.Errorf("lock installation (check directory permissions or another updater): %w", e)
	}
	lock.Close()
	defer os.Remove(target + ".update-lock")

	f, e := os.CreateTemp(filepath.Dir(target), ".bonsai-update-*")
	if e != nil {
		if IsPermissionError(e) {
			return fmt.Errorf("permission denied. Please run 'sudo bonsai --update' or update via your package manager: %w", e)
		}
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if _, e = f.Write(binary); e != nil {
		f.Close()
		return e
	}
	if e = f.Chmod(info.Mode().Perm()); e != nil {
		f.Close()
		if IsPermissionError(e) {
			return fmt.Errorf("permission denied. Please run 'sudo bonsai --update' or update via your package manager: %w", e)
		}
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = swap(tmp, target); e != nil {
		if IsPermissionError(e) {
			return fmt.Errorf("permission denied. Please run 'sudo bonsai --update' or update via your package manager: %w", e)
		}
		return e
	}
	return nil
}

// Install replaces the current running executable with the specified Release.
func (c *Client) Install(ctx context.Context, r Release) error {
	target, e := os.Executable()
	if e != nil {
		return e
	}
	target, e = filepath.EvalSymlinks(target)
	if e != nil {
		return e
	}
	return c.installAt(ctx, r, target)
}

func (c *Client) installAt(ctx context.Context, r Release, target string) error {
	name, e := AssetName(runtime.GOOS, runtime.GOARCH)
	if e != nil {
		return e
	}
	url, e := r.asset(name)
	if e != nil {
		return e
	}
	sumsURL, e := r.asset("checksums.txt")
	if e != nil {
		return e
	}
	sums, e := c.get(ctx, sumsURL, 2<<20)
	if e != nil {
		return e
	}
	data, e := c.get(ctx, url, maxDownload)
	if e != nil {
		return e
	}
	if e = verify(data, sums, name); e != nil {
		return e
	}
	binary, e := extract(data, runtime.GOOS)
	if e != nil {
		return e
	}
	return replace(target, binary)
}
