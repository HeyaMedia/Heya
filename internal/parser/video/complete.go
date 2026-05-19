package video

import "regexp"

var completeDvdExp = regexp.MustCompile(`(?i)\b(NTSC|PAL)?.DVDR\b`)
var completeExp = regexp.MustCompile(`(?i)\b(COMPLETE)\b`)

func IsCompleteDvd(title string) bool {
	return completeDvdExp.MatchString(title)
}

func IsComplete(title string) bool {
	return completeExp.MatchString(title) || IsCompleteDvd(title)
}
