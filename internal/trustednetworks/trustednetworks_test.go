package trustednetworks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalNormalizesAndDeduplicates(t *testing.T) {
	canonical, values, err := Canonical("192.168.1.7/16\n100.64.0.0/10,192.168.0.0/16 2001:db8::1")
	require.NoError(t, err)
	assert.Equal(t, "100.64.0.0/10,192.168.0.0/16,2001:db8::1/128", canonical)
	assert.Equal(t, []string{"100.64.0.0/10", "192.168.0.0/16", "2001:db8::1/128"}, values)
}

func TestContainsOnlyConfiguredPeers(t *testing.T) {
	prefixes, err := Parse(DefaultValue)
	require.NoError(t, err)
	assert.True(t, Contains(prefixes, "100.76.110.94"))
	assert.True(t, Contains(prefixes, "192.168.10.10"))
	assert.False(t, Contains(prefixes, "100.128.0.1"))
	assert.False(t, Contains(prefixes, "203.0.113.7"))
	assert.False(t, Contains(prefixes, "unknown"))
}

func TestParseRejectsInvalidAndOversizedLists(t *testing.T) {
	_, err := Parse("not-a-network")
	assert.ErrorContains(t, err, "invalid trusted network")

	values := make([]string, MaxEntries+1)
	for i := range values {
		values[i] = "127.0.0.1"
	}
	_, _, err = CanonicalList(values)
	assert.ErrorContains(t, err, "maximum")
}
