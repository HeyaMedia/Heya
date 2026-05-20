package transcoder

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLockDedup(t *testing.T) {
	rl := NewRunLock[int]()
	var calls atomic.Int32

	var wg sync.WaitGroup
	results := make([]int, 10)

	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			val, err := rl.Do("key", func() (int, error) {
				calls.Add(1)
				time.Sleep(50 * time.Millisecond)
				return 42, nil
			})
			require.NoError(t, err)
			results[idx] = val
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for _, v := range results {
		assert.Equal(t, 42, v)
	}
}

func TestRunLockDifferentKeys(t *testing.T) {
	rl := NewRunLock[string]()
	var calls atomic.Int32

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		val, _ := rl.Do("a", func() (string, error) {
			calls.Add(1)
			time.Sleep(20 * time.Millisecond)
			return "alpha", nil
		})
		assert.Equal(t, "alpha", val)
	}()

	go func() {
		defer wg.Done()
		val, _ := rl.Do("b", func() (string, error) {
			calls.Add(1)
			time.Sleep(20 * time.Millisecond)
			return "beta", nil
		})
		assert.Equal(t, "beta", val)
	}()

	wg.Wait()
	assert.Equal(t, int32(2), calls.Load())
}

func TestRunLockErrorPropagation(t *testing.T) {
	rl := NewRunLock[int]()

	var wg sync.WaitGroup
	wg.Add(3)

	for range 3 {
		go func() {
			defer wg.Done()
			_, err := rl.Do("err", func() (int, error) {
				return 0, assert.AnError
			})
			assert.ErrorIs(t, err, assert.AnError)
		}()
	}

	wg.Wait()
}

func TestRunLockRerunsAfterCompletion(t *testing.T) {
	rl := NewRunLock[int]()

	val1, _ := rl.Do("key", func() (int, error) { return 1, nil })
	val2, _ := rl.Do("key", func() (int, error) { return 2, nil })

	assert.Equal(t, 1, val1)
	assert.Equal(t, 2, val2)
}
