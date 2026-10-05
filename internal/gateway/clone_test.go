package gateway

import (
	adkmodels "github.com/Alcova-AI/adk-models-go"
	"testing"
)

func TestCloneCacheMarkerOptions(t *testing.T) {
	markers := map[string]map[string]any{"azure": {"promptCacheBreakpoint": map[string]any{"mode": "explicit"}}}
	original := &adkmodels.VercelConfig{SystemInstructionCacheOptions: markers, ConversationHistoryCacheOptions: markers}
	copied := CloneConfig(original)
	markers["azure"]["promptCacheBreakpoint"].(map[string]any)["mode"] = "changed"
	delete(markers, "azure")
	for _, options := range []map[string]map[string]any{copied.SystemInstructionCacheOptions, copied.ConversationHistoryCacheOptions} {
		if options["azure"]["promptCacheBreakpoint"].(map[string]any)["mode"] != "explicit" {
			t.Fatal("caller mutation changed snapshot")
		}
	}
}
