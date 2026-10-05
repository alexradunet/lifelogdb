package importer

import (
	"fmt"
	"strconv"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

type resolvedReadingKeys struct {
	byWrite map[int]string
	aliases map[string]string
}

func resolveReadingKeys(t *core.Tx, source string, f *Facts, pos []int) (resolvedReadingKeys, error) {
	ids, err := readingIdentities(f, pos)
	if err != nil {
		return resolvedReadingKeys{}, err
	}
	res := resolvedReadingKeys{byWrite: map[int]string{}, aliases: map[string]string{}}
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
				res.aliases[id.canonicalKey] = id.canonicalKey
				res.aliases[id.legacyKey] = id.canonicalKey
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
	for _, root := range roots {
		rootsByID[root.ID] = root
		for _, id := range group {
			if readingRootMatches(file, id, root, group) {
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
			res.aliases[id.canonicalKey] = id.canonicalKey
			res.aliases[id.legacyKey] = id.canonicalKey
		case 1:
			root := rootsByID[roots[0]]
			res.byWrite[id.index] = root.ImportKey
			res.aliases[id.canonicalKey] = root.ImportKey
			res.aliases[id.legacyKey] = root.ImportKey
			res.aliases[root.ImportKey] = root.ImportKey
		default:
			return refuse("%s: ambiguous legacy reading group %s on %s has multiple roots for one source reading", file, id.metricKey, id.day)
		}
	}
	return nil
}

func readingRootMatches(file string, id readingIdentity, root core.ImportedMeasurementRoot, group []readingIdentity) bool {
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
	if root.ImportKey == id.canonicalKey || root.ImportKey == id.legacyKey {
		return true
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

func describeResolvedKey(original, resolved string) string {
	if original == resolved {
		return original
	}
	return fmt.Sprintf("%s (resolved to %s)", original, resolved)
}
