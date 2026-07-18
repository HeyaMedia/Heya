package transcoder

import (
	"errors"
	"fmt"
	"os"

	"github.com/karbowiak/heya/internal/atomicfile"
)

// reserveAtomicOutput chooses a unique temporary path next to out. Producers
// write the temporary file completely and then publish it with os.Rename, so
// readers never observe a partial cache artifact. A deterministic `<out>.tmp`
// name is unsafe when multiple Heya processes share one cache.
func reserveAtomicOutput(out string) (string, error) {
	return atomicfile.Reserve(out, 0o640)
}

func removeTemporaryOutput(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("remove temporary output: %w", err)
}

// produceAtomicOutput runs produce against a unique same-directory temporary
// file and publishes it only after produce succeeds. Failed producers leave no
// target (or preserve an older target), and concurrent processes never expose
// one another's partially-written files.
func produceAtomicOutput(out string, produce func(tmp string) error) (returnErr error) {
	return atomicfile.Produce(out, 0o640, produce)
}
