package diagnostics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessCPUUsesConventionalPerCoreScale(t *testing.T) {
	assert.Equal(t, 125.0, clampProcessPercent(125, 4))
	assert.Equal(t, 400.0, clampProcessPercent(999, 4))
	assert.Zero(t, clampProcessPercent(-1, 4))
}
