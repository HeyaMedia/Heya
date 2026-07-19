//go:build !linux && !darwin

package atomicfile

import "errors"

func exchangePaths(_, _ string) error {
	return errors.New("atomic exchange is unsupported on this platform")
}
