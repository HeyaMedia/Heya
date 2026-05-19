package video

import "regexp"

type Resolution string

const (
	R2160P Resolution = "2160P"
	R1080P Resolution = "1080P"
	R720P  Resolution = "720P"
	R576P  Resolution = "576P"
	R540P  Resolution = "540P"
	R480P  Resolution = "480P"
)

var resolutionExp = regexp.MustCompile(`(?i)(?P<R2160P>2160p|4k[-_. ](?:UHD|HEVC|BD)|(?:UHD|HEVC|BD)[-_. ]4k|\b(4k)\b|COMPLETE\.UHD|UHD\.COMPLETE)|(?P<R1080P>1080[ip]|1920x1080)(10bit)?|(?P<R720P>720[ip]|1280x720|960p)(10bit)?|(?P<R576P>576[ip])|(?P<R540P>540[ip])|(?P<R480P>480[ip]|640x480|848x480)`)

type ResolutionResult struct {
	Resolution Resolution
	Source     string
}

func ParseResolution(title string) ResolutionResult {
	match := resolutionExp.FindStringSubmatch(title)
	if match != nil {
		names := resolutionExp.SubexpNames()
		for i, name := range names {
			if i == 0 || name == "" || match[i] == "" {
				continue
			}
			switch name {
			case "R2160P":
				return ResolutionResult{Resolution: R2160P, Source: match[i]}
			case "R1080P":
				return ResolutionResult{Resolution: R1080P, Source: match[i]}
			case "R720P":
				return ResolutionResult{Resolution: R720P, Source: match[i]}
			case "R576P":
				return ResolutionResult{Resolution: R576P, Source: match[i]}
			case "R540P":
				return ResolutionResult{Resolution: R540P, Source: match[i]}
			case "R480P":
				return ResolutionResult{Resolution: R480P, Source: match[i]}
			}
		}
	}

	sources := ParseSource(title)
	for _, s := range sources {
		if s == SourceDVD {
			return ResolutionResult{Resolution: R480P}
		}
	}

	return ResolutionResult{}
}
