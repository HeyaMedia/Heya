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

func TestParseSettingsMalformedJSONUsesSafeDefaults(t *testing.T) {
	settings := ParseSettings([]byte(`{"watch":true`))
	if !settings.UseLocalData {
		t.Fatal("UseLocalData=false, want safe default for malformed settings")
	}
	if settings.Watch {
		t.Fatal("Watch=true, want partially decoded malformed settings discarded")
	}
}
