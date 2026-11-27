package catalog

import (
	"os"

	"golang.org/x/sys/unix"
)

func cloneLocal(in *os.File, path string) error {
	return unix.Fclonefileat(int(in.Fd()), unix.AT_FDCWD, path, unix.CLONE_NOOWNERCOPY)
}
