//go:build windows

package transcoder

import (
	"errors"
	"os"
)

var errProcessSuspendUnsupported = errors.New("process suspension is unavailable on windows")

func suspendProcess(_ *os.Process) error { return errProcessSuspendUnsupported }
func resumeProcess(_ *os.Process) error  { return errProcessSuspendUnsupported }
