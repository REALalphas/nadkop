package core

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// Private state (identity, subscriptions, catalogue) is only trusted when it
// is a regular file owned by the effective UID. Restored files with a foreign
// owner are rejected instead of being silently consumed. A file that is
// already owned by the caller but carries a weak mode is tightened to 0600 on
// read, which matches how DeviceID has historically repaired its own mode
// (the packaged empty hwid seed itself ships as 0644).
var ErrPrivateStateOwner = errors.New("private state has an unexpected owner; repair restore metadata")

func openPrivateState(path string) (*os.File, error) {
	// O_NONBLOCK keeps FIFO opens from blocking; regular-file reads are
	// unaffected, and a FIFO with no writer fails fast on read instead.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("private state must be a regular file")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		f.Close()
		return nil, errors.New("private state must be a regular file")
	}
	// os.Geteuid is 0 for the daemon even when tests run as an unprivileged
	// user, and it is also 0 for the mapped root inside the S3 user namespace.
	if owner.Uid != uint32(os.Geteuid()) {
		f.Close()
		return nil, ErrPrivateStateOwner
	}
	if st.Mode().Perm() != 0600 {
		if err := f.Chmod(0600); err != nil {
			f.Close()
			return nil, errors.New("cannot protect private state")
		}
	}
	return f, nil
}

// OpenPrivateState exposes the ownership/permission gate for the CLI and router.
// Callers close the returned file.
func OpenPrivateState(path string) (*os.File, error) {
	return openPrivateState(path)
}

// ReadPrivateState reads a canonical private file after the gate above.
func ReadPrivateState(path string) ([]byte, error) {
	f, err := openPrivateState(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSubscription+1))
	if err != nil || len(data) > maxSubscription {
		return nil, errors.New("private state exceeds 4 MiB or cannot be read")
	}
	return data, nil
}
