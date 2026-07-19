// Package generatedwrite describes files deliberately published into a media
// library by Heya. Writers return the exact bytes they produced so the
// filesystem watcher can suppress only the matching self-generated event.
package generatedwrite

import "crypto/sha256"

// Output is immutable evidence about the desired bytes at one sidecar path.
//
// Attested means Path currently contains exactly Size/SHA256 and is therefore
// safe to record as Heya-generated. Written additionally means this call
// published those bytes and may have emitted filesystem events. A retry after
// a failed durable acknowledgement returns Attested=true, Written=false when
// the exact desired bytes are already present. User-owned mismatches set both
// flags false and must never be acknowledged or overwritten.
type Output struct {
	Path     string
	Size     int64
	SHA256   [sha256.Size]byte
	Attested bool
	Written  bool
}

// FromBytes constructs evidence for content published by this call.
func FromBytes(path string, content []byte) Output {
	return Output{Path: path, Size: int64(len(content)), SHA256: sha256.Sum256(content), Attested: true, Written: true}
}

// AttestBytes constructs evidence for exact desired content already present at
// path. No write occurred during this call.
func AttestBytes(path string, content []byte) Output {
	return Attest(path, int64(len(content)), sha256.Sum256(content))
}

// Attest constructs evidence from a previously computed exact signature.
func Attest(path string, size int64, digest [sha256.Size]byte) Output {
	return Output{Path: path, Size: size, SHA256: digest, Attested: true}
}

// Published constructs evidence from a signature computed while atomically
// publishing a file.
func Published(path string, size int64, digest [sha256.Size]byte) Output {
	return Output{Path: path, Size: size, SHA256: digest, Attested: true, Written: true}
}
