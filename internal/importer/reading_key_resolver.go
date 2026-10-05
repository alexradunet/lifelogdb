package importer

import (
	"context"
	"fmt"
	"strconv"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

type resolvedReadingKeys struct {
	byWrite          map[int]string
	aliases          map[string]string
	aliasWrite       map[string]int
	storedKeyWrite   map[string]int
	ambiguousAliases map[string]bool
}

func resolveReadingKeys(t *core.Tx, source string, f *Facts, pos []int) (resolvedReadingKeys, error) {
	ids, err := readingIdentities(f, pos)
	if err != nil {
		return resolvedReadingKeys{}, err
	}
	res := resolvedReadingKeys{byWrite: map[int]string{}, aliases: map[string]string{}, aliasWrite: map[string]int{}, storedKeyWrite: map[string]int{}, ambiguousAliases: map[string]bool{}}
	byGroup := map[string][]readingIdentity{}
	for _, id := range ids {
		byGroup[id.metricKey+"|"+id.day] = append(byGroup[id.metricKey+"|"+id.day], id)
		res.byWrite[id.index] = id.canonicalKey
	}
	for _, group := range byGroup {
		roots, err := t.ImportedMeasurementRootsByFile(source, f.File, group[0].metricKey)
		if err != nil {
			return resolvedReadingKeys{}, err
		}
		var groupRoots []core.ImportedMeasurementRoot
		for _, root := range roots {
			if root.Day == group[0].day {
				groupRoots = append(groupRoots, root)
			}
		}
		if len(groupRoots) == 0 {
			for _, id := range group {
				res.setAlias(id.canonicalKey, id.canonicalKey, id.index)
				res.setAlias(id.legacyKey, id.canonicalKey, id.index)
			}
			continue
		}
		if err := resolveReadingGroup(f.File, group, groupRoots, res); err != nil {
			return resolvedReadingKeys{}, err
		}
	}
	return res, nil
}

func resolveReadingGroup(file string, group []readingIdentity, roots []core.ImportedMeasurementRoot, res resolvedReadingKeys) error {
	matchesByRoot := map[string][]int64{}
	rootsByID := map[int64]core.ImportedMeasurementRoot{}
	rootsForWrite := map[int][]int64{}
	canonicalStored := canonicalStoredGroup(file, group, roots)
	storedOrdinal := completeSingleStoredOrdinalGroup(file, group, roots)
	for _, root := range roots {
		rootsByID[root.ID] = root
		for _, id := range group {
			if readingRootMatches(file, id, root, group, canonicalStored, storedOrdinal) {
				matchesByRoot[root.ImportKey] = append(matchesByRoot[root.ImportKey], int64(id.index))
				rootsForWrite[id.index] = append(rootsForWrite[id.index], root.ID)
			}
		}
	}
	for _, root := range roots {
		matches := matchesByRoot[root.ImportKey]
		if len(matches) != 1 {
			return refuse("%s: ambiguous legacy reading group %s on %s for stored key %s", file, group[0].metricKey, group[0].day, root.ImportKey)
		}
	}
	for _, id := range group {
		roots := rootsForWrite[id.index]
		switch len(roots) {
		case 0:
			res.byWrite[id.index] = id.canonicalKey
			res.setAlias(id.canonicalKey, id.canonicalKey, id.index)
			res.setAlias(id.legacyKey, id.canonicalKey, id.index)
		case 1:
			root := rootsByID[roots[0]]
			res.byWrite[id.index] = root.ImportKey
			res.setAlias(id.canonicalKey, root.ImportKey, id.index)
			if !canonicalStored {
				res.setAlias(id.legacyKey, root.ImportKey, id.index)
			}
			res.setAlias(root.ImportKey, root.ImportKey, id.index)
			res.storedKeyWrite[root.ImportKey] = id.index
		default:
			return refuse("%s: ambiguous legacy reading group %s on %s has multiple roots for one source reading", file, id.metricKey, id.day)
		}
	}
	return nil
}

func (r resolvedReadingKeys) setAlias(alias, resolved string, index int) {
	if alias == "" {
		return
	}
	if prev, ok := r.aliasWrite[alias]; ok && prev != index {
		r.ambiguousAliases[alias] = true
		delete(r.aliases, alias)
		return
	}
	r.aliasWrite[alias] = index
	if !r.ambiguousAliases[alias] {
		r.aliases[alias] = resolved
	}
}

func canonicalStoredGroup(file string, group []readingIdentity, roots []core.ImportedMeasurementRoot) bool {
	if len(roots) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, id := range group {
		seen[id.canonicalKey] = false
	}
	for _, root := range roots {
		parsed, ok := parseReadingKey(root.ImportKey)
		if !ok || parsed.file != file || parsed.metric != group[0].metricKey || parsed.day != group[0].day {
			return false
		}
		if _, ok := seen[root.ImportKey]; !ok {
			return false
		}
		seen[root.ImportKey] = true
	}
	for _, found := range seen {
		if !found {
			return false
		}
	}
	return true
}

func completeSingleStoredOrdinalGroup(file string, group []readingIdentity, roots []core.ImportedMeasurementRoot) bool {
	if len(roots) != len(group) {
		return false
	}
	storedMetric := ""
	ordinals := map[int]bool{}
	for _, root := range roots {
		parsed, ok := parseReadingKey(root.ImportKey)
		if !ok || parsed.file != file || parsed.day != group[0].day || text.TitleKey(parsed.metric) != group[0].metricKey {
			return false
		}
		if storedMetric == "" {
			storedMetric = parsed.metric
		} else if parsed.metric != storedMetric {
			return false
		}
		if parsed.token == "" || group[0].takenAt != "" {
			return false
		}
		n, err := strconv.Atoi(parsed.token)
		if err != nil || n < 1 || n > len(group) || ordinals[n] {
			return false
		}
		ordinals[n] = true
	}
	return len(ordinals) == len(group)
}

func readingRootMatches(file string, id readingIdentity, root core.ImportedMeasurementRoot, group []readingIdentity, canonicalStored, storedOrdinal bool) bool {
	parsed, ok := parseReadingKey(root.ImportKey)
	if !ok || parsed.file != file || text.TitleKey(parsed.metric) != id.metricKey || parsed.day != id.day {
		return false
	}
	if root.MetricKey != id.metricKey || root.Day != id.day || root.TakenAt != id.takenAt || root.TZ != id.tz || root.CapturedWithKey != id.withKey {
		return false
	}
	if !sameReadingValue(root.Value, id.value) {
		return false
	}
	if canonicalStored {
		return root.ImportKey == id.canonicalKey
	}
	if id.takenAt != "" {
		return root.ImportKey == id.canonicalKey || root.ImportKey == id.legacyKey || parsed.token == id.takenAt
	}
	ordinal, err := strconv.Atoi(parsed.token)
	if err != nil {
		return false
	}
	if storedOrdinal {
		return ordinal == id.canonicalOrdinal
	}
	if root.ImportKey == id.canonicalKey || root.ImportKey == id.legacyKey {
		return true
	}
	if parsed.metric == id.rawMetric {
		return ordinal == id.legacyOrdinal
	}
	return singleRawMetric(group) && ordinal == id.canonicalOrdinal
}

func sameReadingValue(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func singleRawMetric(group []readingIdentity) bool {
	if len(group) == 0 {
		return true
	}
	first := group[0].rawMetric
	for _, id := range group[1:] {
		if id.rawMetric != first {
			return false
		}
	}
	return true
}

func (w *Workspace) resolveCorrectionKey(ctxSource string, t *core.Tx, metric, key string) (string, error) {
	parsed, ok := parseReadingKey(key)
	if !ok {
		return key, nil
	}
	f, pos, _, err := w.prepare(parsed.file)
	if err != nil {
		return "", err
	}
	resolved, err := resolveReadingKeys(t, ctxSource, f, pos)
	if err != nil {
		return "", err
	}
	if resolved.ambiguousAliases[key] {
		return "", refuse("%s: ambiguous legacy correction root %s for %s", parsed.file, key, metric)
	}
	if alias, ok := resolved.aliases[key]; ok {
		return alias, nil
	}
	ids, err := readingIdentities(f, pos)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, id := range ids {
		if text.TitleKey(metric) != id.metricKey || parsed.day != id.day || text.TitleKey(parsed.metric) != id.metricKey {
			continue
		}
		if historicalKeyCouldName(parsed, id, ids) {
			matches = append(matches, resolved.byWrite[id.index])
		}
	}
	matches = uniqueStrings(matches)
	if len(matches) != 1 {
		return "", refuse("%s: ambiguous legacy correction root %s for %s", parsed.file, key, metric)
	}
	return matches[0], nil
}

func historicalKeyCouldName(parsed parsedReadingKey, id readingIdentity, all []readingIdentity) bool {
	if parsed.file == "" || parsed.day != id.day || text.TitleKey(parsed.metric) != id.metricKey {
		return false
	}
	if id.takenAt != "" {
		return parsed.token == id.takenAt
	}
	ordinal, err := strconv.Atoi(parsed.token)
	if err != nil {
		return false
	}
	if parsed.metric == id.rawMetric {
		return ordinal == id.legacyOrdinal
	}
	var group []readingIdentity
	for _, candidate := range all {
		if candidate.metricKey == id.metricKey && candidate.day == id.day {
			group = append(group, candidate)
		}
	}
	return singleRawMetric(group) && ordinal == id.canonicalOrdinal
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (w *Workspace) validateTrialReadingIdentity(ctx context.Context, trial *core.Store) error {
	lines, _, err := w.Ledger()
	if err != nil {
		return nil
	}
	for _, line := range lines {
		if line.State != "x" && line.State != "?" {
			continue
		}
		facts, loadErr := w.LoadFacts(line.File)
		if loadErr != nil {
			return loadErr
		}
		hasReading := false
		for _, wr := range facts.Writes {
			if wr.Reading != nil {
				hasReading = true
				break
			}
		}
		if !hasReading {
			continue
		}
		f, pos, rules, err := w.prepare(line.File)
		if err != nil {
			return err
		}
		if err := trial.DryRun(ctx, rules.Source, func(t *core.Tx) error {
			_, err := resolveReadingKeys(t, rules.Source, f, pos)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

type correctionProof struct {
	entries map[correctionRoot]correctionProofEntry
}

type correctionProofEntry struct {
	// Bind the index to this checked snapshot, never to a later read of the facts file.
	facts      *Facts
	positions  []int
	writeIndex int
}

func (w *Workspace) buildCorrectionProof(ctx context.Context, trial, target *core.Store, intents []correctionIntent) (*correctionProof, error) {
	if trial == nil || trial == target {
		return nil, nil
	}
	needed := map[correctionRoot]bool{}
	cs, err := w.Corrections()
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		needed[correctionRoot{c.Source, c.Metric, c.Key}] = true
	}
	for _, intent := range intents {
		needed[intent.root()] = true
	}
	proof := &correctionProof{entries: map[correctionRoot]correctionProofEntry{}}
	for root := range needed {
		parsed, ok := parseReadingKey(root.key)
		if !ok {
			continue
		}
		f, pos, _, err := w.prepare(parsed.file)
		if err != nil {
			return nil, err
		}
		entry := correctionProofEntry{facts: f, positions: pos, writeIndex: -1}
		err = trial.DryRun(ctx, root.source, func(t *core.Tx) error {
			resolved, err := resolveReadingKeys(t, root.source, f, pos)
			if err != nil {
				return err
			}
			idx, ok := resolved.storedKeyWrite[root.key]
			if !ok {
				return refuse("%s: correction root %s for %s has no validated original trial reading", parsed.file, root.key, root.metric)
			}
			entry.writeIndex = idx
			return nil
		})
		if err != nil {
			return nil, err
		}
		proof.entries[root] = entry
	}
	return proof, nil
}

func (w *Workspace) resolveCorrectionKeyFromProof(proof *correctionProof, target *core.Tx, source, metric, key string) (string, error) {
	if proof == nil {
		return w.resolveCorrectionKey(source, target, metric, key)
	}
	parsed, ok := parseReadingKey(key)
	if !ok {
		return key, nil
	}
	entry, ok := proof.entries[correctionRoot{source, metric, key}]
	if !ok {
		return "", refuse("%s: correction root %s for %s has no validated original trial reading", parsed.file, key, metric)
	}
	resolvedTarget, err := resolveReadingKeys(target, source, entry.facts, entry.positions)
	if err != nil {
		return "", err
	}
	mapped := resolvedTarget.byWrite[entry.writeIndex]
	if mapped == "" {
		return "", refuse("%s: correction root %s did not map to a replayed reading", entry.facts.File, key)
	}
	return mapped, nil
}

func describeResolvedKey(original, resolved string) string {
	if original == resolved {
		return original
	}
	return fmt.Sprintf("%s (resolved to %s)", original, resolved)
}
