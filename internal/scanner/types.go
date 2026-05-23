package scanner

type ScanResult struct {
	Discovered int `json:"discovered"`
	New        int `json:"new"`
	Updated    int `json:"updated"`
	Unchanged  int `json:"unchanged"`
	Deleted    int `json:"deleted"`
	Errors     int `json:"errors"`
}

type ProgressFunc func(discovered int, current string)

type ScanOptions struct {
	ForceRescan bool
	OnProgress  ProgressFunc
}
