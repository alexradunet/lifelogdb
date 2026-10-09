package importer

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// DraftPreparedJSON keeps new supported-profile selectors lossless at all adapters.
// roundRaw, when not empty, is a JSON list of the quantity codes rounded to whole units.
func (w *Workspace) DraftPreparedJSON(ctx context.Context, file, profile, kind, bindingRaw, metricsRaw, roundRaw string) (*PreparedBatch, error) {
	var binding *FitSessionBinding
	if bindingRaw != "" {
		if !utf8.ValidString(bindingRaw) || validateSourceJSON([]byte(bindingRaw)) != nil {
			return nil, refuse("invalid lossless Fit binding")
		}
		binding = &FitSessionBinding{}
		dec := json.NewDecoder(strings.NewReader(bindingRaw))
		dec.DisallowUnknownFields()
		if dec.Decode(binding) != nil {
			return nil, refuse("invalid Fit binding")
		}
	}
	if !utf8.ValidString(metricsRaw) || validateSourceJSON([]byte(metricsRaw)) != nil {
		return nil, refuse("invalid lossless quantity mappings")
	}
	var metrics map[string]string
	if json.Unmarshal([]byte(metricsRaw), &metrics) != nil {
		return nil, refuse("invalid quantity mappings")
	}
	var round []string
	if roundRaw != "" {
		if !utf8.ValidString(roundRaw) || validateSourceJSON([]byte(roundRaw)) != nil {
			return nil, refuse("invalid lossless rounded quantities")
		}
		dec := json.NewDecoder(strings.NewReader(roundRaw))
		if dec.Decode(&round) != nil || dec.More() {
			return nil, refuse("round is a JSON list of quantity codes")
		}
	}
	return w.draftPrepared(ctx, file, profile, kind, binding, metrics, round)
}
