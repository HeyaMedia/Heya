//go:build darwin

package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadHostCPUDarwinUsesNormalizedLoadAverage(t *testing.T) {
	sample := readHostCPU()
	assert.True(t, sample.available)
	assert.True(t, sample.instantaneous)
	assert.Equal(t, "load_average_1m", sample.metric)
	assert.Greater(t, sample.total, 0.0)
	assert.GreaterOrEqual(t, sample.busy, 0.0)
}
