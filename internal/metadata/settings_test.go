package metadata

import (
	"encoding/json"
	"testing"
)

func TestParseSettingsDefaultsUseLocalDataToTrue(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`{"watch":true}`)} {
		settings := ParseSettings(raw)
		if !settings.UseLocalData {
			t.Fatalf("ParseSettings(%s) UseLocalData=false, want true", string(raw))
		}
	}
}

func TestLibrarySettingsAllowsUseLocalDataFalse(t *testing.T) {
	var settings LibrarySettings
	if err := json.Unmarshal([]byte(`{"use_local_data":false}`), &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settings.UseLocalData {
		t.Fatal("UseLocalData=true, want explicit false to survive")
	}
}
