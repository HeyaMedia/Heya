package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLibraryScopeRoot(t *testing.T) {
	inside, root := libraryScopeRoot([]string{"/storage/Music", "/storage/NewMusic"}, "/storage/NewMusic/LISA")
	require.True(t, inside)
	require.Equal(t, "/storage/NewMusic", root)

	inside, _ = libraryScopeRoot([]string{"/storage/Music"}, "/storage/Musicology/LISA")
	require.False(t, inside)

	inside, _ = libraryScopeRoot([]string{"/storage/Music"}, "/storage/Music/../Secrets")
	require.False(t, inside)
}
