package chrootarchive

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/reexec"
)

var chrootArchiver = &archive.Archiver{Untar: Untar}

func chroot(path string) error {
	if err := syscall.Chroot(path); err != nil {
		return err
	}
	return syscall.Chdir("/")
}

func untar() {
	runtime.LockOSThread()
	flag.Parse()
	if err := chroot(flag.Arg(0)); err != nil {
		fatal(err)
	}
	var options *archive.TarOptions
	if err := json.NewDecoder(strings.NewReader(flag.Arg(1))).Decode(&options); err != nil {
		fatal(err)
	}
	if err := archive.Unpack(os.Stdin, "/", options); err != nil {
		fatal(err)
	}
	// fully consume stdin in case it is zero padded
	flush(os.Stdin)
	os.Exit(0)
}

func Untar(tarArchive io.Reader, dest string, options *archive.TarOptions) error {
	if tarArchive == nil {
		return fmt.Errorf("Empty archive")
	}
	if options == nil {
		options = &archive.TarOptions{}
	}
	if options.ExcludePatterns == nil {
		options.ExcludePatterns = []string{}
	}

	var (
		buf bytes.Buffer
		enc = json.NewEncoder(&buf)
	)
	if err := enc.Encode(options); err != nil {
		return fmt.Errorf("Untar json encode: %v", err)
	}
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		if err := os.MkdirAll(dest, 0777); err != nil {
			return err
		}
	}
	dest = filepath.Clean(dest)
	decompressedArchive, err := archive.DecompressStream(tarArchive)
	if err != nil {
		return err
	}
	defer decompressedArchive.Close()

	cmd := reexec.Command("docker-untar", dest, buf.String())
	cmd.Stdin = decompressedArchive
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Untar %s %s", err, out)
	}
	return nil
}

func TarUntar(src, dst string) error {
	return chrootArchiver.TarUntar(src, dst)
}

// CopyWithTar creates a tar archive of filesystem path `src`, and
// unpacks it at filesystem path `dst`.
// The archive is streamed directly with fixed buffering and no
// intermediary disk IO.
func CopyWithTar(src, dst string) error {
	return chrootArchiver.CopyWithTar(src, dst)
}

// CopyFileWithTar emulates the behavior of the 'cp' command-line
// for a single file. It copies a regular file from path `src` to
// path `dst`, and preserves all its metadata.
//
// If `dst` ends with a trailing slash '/', the final destination path
// will be `dst/base(src)`.
func CopyFileWithTar(src, dst string) (err error) {
	return chrootArchiver.CopyFileWithTar(src, dst)
}

// UntarPath is a convenience function which looks for an archive
// at filesystem path `src`, and unpacks it at `dst`.
func UntarPath(src, dst string) error {
	return chrootArchiver.UntarPath(src, dst)
}
