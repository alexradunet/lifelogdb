package importer

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

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
	owners := map[string]string{}
	for i := range ids {
		key := ids[i].metricKey
		owner, loaded := owners[key]
		if !loaded {
			p, err := t.Lookup(key)
			if err != nil {
				return resolvedReadingKeys{}, err
			}
			owner = "name:" + key
			if p != nil {
				owner = "entity:" + strconv.FormatInt(p.ID, 10)
			}
			owners[key] = owner
		}
		ids[i].groupKey = owner
	}
	return resolveReadingIdentitiesWithLoader(source, f, ids, t.ImportedMeasurementRootsByFile)
}

type readingRootLoader func(source, file, metricKey string) ([]core.ImportedMeasurementRoot, error)

func resolveReadingKeysWithLoader(source string, f *Facts, pos []int, load readingRootLoader) (resolvedReadingKeys, error) {
	ids, err := readingIdentities(f, pos)
	if err != nil {
		return resolvedReadingKeys{}, err
	}
	return resolveReadingIdentitiesWithLoader(source, f, ids, load)
}

func resolveReadingIdentitiesWithLoader(source string, f *Facts, ids []readingIdentity, load readingRootLoader) (resolvedReadingKeys, error) {
	// Reject colliding timed identities after name resolution, before any writes.
	seenTimed := map[string]bool{}
	for _, id := range ids {
		if id.takenAt == "" {
			continue
		}
		identity := id.groupKey + "|" + id.day + "|" + id.takenAt
		if seenTimed[identity] {
			return resolvedReadingKeys{}, refuse("%s: repeated timed reading identity %s on %s at %s", f.File, id.metricKey, id.day, id.takenAt)
		}
		seenTimed[identity] = true
	}
	res := resolvedReadingKeys{byWrite: map[int]string{}, aliases: map[string]string{}, aliasWrite: map[string]int{}, storedKeyWrite: map[string]int{}, ambiguousAliases: map[string]bool{}}
	byGroup := map[string][]readingIdentity{}
	for _, id := range ids {
		byGroup[id.groupKey+"|"+id.day] = append(byGroup[id.groupKey+"|"+id.day], id)
		res.byWrite[id.index] = id.canonicalKey
	}
	// Roots belong only to this call's checked facts and transaction snapshot.
	rootsByMetricDay := map[string]map[string][]core.ImportedMeasurementRoot{}
	for _, group := range byGroup {
		sort.SliceStable(group, func(i, j int) bool { return group[i].pos < group[j].pos })
		ordinal, previous := 0, -1
		for i := range group {
			if group[i].takenAt != "" {
				continue
			}
			if ordinal > 0 && group[i].pos == previous {
				return resolvedReadingKeys{}, refuse("%s: reading group has tied source positions; use distinct quotes", f.File)
			}
			ordinal++
			previous = group[i].pos
			group[i].canonicalOrdinal = ordinal
			group[i].canonicalKey = readingKey(f.File, group[i].metricKey+"|"+group[i].day, strconv.Itoa(ordinal))
			res.byWrite[group[i].index] = group[i].canonicalKey
		}
		metric := group[0].metricKey
		byDay, loaded := rootsByMetricDay[group[0].groupKey]
		if !loaded {
			roots, err := load(source, f.File, metric)
			if err != nil {
				return resolvedReadingKeys{}, err
			}
			byDay = map[string][]core.ImportedMeasurementRoot{}
			for _, root := range roots {
				byDay[root.Day] = append(byDay[root.Day], root)
			}
			rootsByMetricDay[group[0].groupKey] = byDay
		}
		groupRoots := byDay[group[0].day]
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
	ordinals := map[int]bool{}
	storedMetric, mixedRaw, allCanonical := "", false, true
	for _, root := range roots {
		parsed, ok := parseReadingKey(root.ImportKey)
		if !ok || parsed.file != file || parsed.day != group[0].day || !rootMetricNames(root, text.TitleKey(parsed.metric)) {
			return false
		}
		if storedMetric == "" {
			storedMetric = parsed.metric
		} else if storedMetric != parsed.metric {
			mixedRaw = true
		}
		allCanonical = allCanonical && parsed.metric == text.TitleKey(parsed.metric)
		if parsed.token == "" || group[0].takenAt != "" {
			return false
		}
		n, err := strconv.Atoi(parsed.token)
		if err != nil || n < 1 || n > len(group) || ordinals[n] {
			return false
		}
		ordinals[n] = true
	}
	// Distinct historical raw spelling namespaces cannot establish canonical
	// ordinals merely by casefolding. Only already-canonical owned alias keys may
	// share a complete ordinal range; retain the single-raw-namespace legacy path.
	return len(ordinals) == len(group) && (!mixedRaw || allCanonical)
}

func readingRootMatches(file string, id readingIdentity, root core.ImportedMeasurementRoot, group []readingIdentity, canonicalStored, storedOrdinal bool) bool {
	parsed, ok := parseReadingKey(root.ImportKey)
	if !ok || parsed.file != file || !rootMetricNames(root, text.TitleKey(parsed.metric)) || parsed.day != id.day {
		return false
	}
	if !rootMetricNames(root, id.metricKey) || root.Day != id.day || root.TakenAt != id.takenAt || root.TZ != id.tz ||
		!(id.withKey == "" && root.CapturedWithKey == "" || slices.Contains(root.CapturedWithKeys, id.withKey)) {
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

func rootMetricNames(root core.ImportedMeasurementRoot, key string) bool {
	return slices.Contains(root.MetricKeys, key)
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
	f, pos, _, _, err := w.prepare(parsed.file, false)
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

// collectTrialReadingIdentityFailures only continues across independent file failures.
// Workspace access and cancellation remain global aborts, never replay permission.
func (w *Workspace) collectTrialReadingIdentityFailures(ctx context.Context, trial *core.Store) ([]Failure, error) {
	lines, _, err := w.Ledger()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Check shared prerequisites separately so their failures are not attributed to files.
	if g, err := w.Gate("rules.md"); err != nil {
		return nil, err
	} else if g != "approved" {
		return nil, refuse("rules.md is %s: the owner approves it first (lifelog import approve rules)", g)
	}
	rules, err := w.Rules()
	if err != nil {
		return nil, err
	}
	approved, err := w.ApprovedMetrics()
	if err != nil {
		return nil, err
	}
	selectedFiles := map[string]bool{}
	batches, e := w.appliedPrepared(ctx)
	if e != nil {
		return nil, e
	}
	for _, b := range batches {
		if e = w.admitSource(b.File); e != nil {
			return nil, e
		}
		if e = verifyPrepared(ctx, trial, b); e != nil {
			return nil, e
		}
		selectedFiles[b.File] = true
	}
	photos, e := w.appliedSelectedPhotos(ctx)
	if e != nil {
		return nil, e
	}
	for _, p := range photos {
		if e = w.admitSource(p.File); e != nil {
			return nil, e
		}
		if _, e = w.verifySelectedPhoto(ctx, trial, p, false); e != nil {
			return nil, e
		}
		selectedFiles[p.File] = true
		if p.Sidecar != "" {
			selectedFiles[p.Sidecar] = true
		}
	}
	var failures []Failure
	for _, line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if line.State != "x" && line.State != "?" {
			continue
		}
		if selectedFiles[line.File] {
			continue
		}
		err := func() error {
			f, err := w.LoadFacts(line.File)
			if err != nil {
				return err
			}
			hasReading := false
			for _, wr := range f.Writes {
				if wr.Reading != nil {
					hasReading = true
					break
				}
			}
			if !hasReading {
				return nil
			}
			src, err := w.readRawSource(line.File)
			if err != nil {
				return err
			}
			pos, refused, errs := w.checkStatic(f, src, rules, approved)
			if len(errs) > 0 {
				return refuse("%s: %s", line.File, strings.Join(errs, "; "))
			}
			if len(refused) > 0 {
				return fileRefusal(line.File, refused)
			}
			return trial.DryRun(ctx, rules.Source, func(t *core.Tx) error {
				_, err := resolveReadingKeys(t, rules.Source, f, pos)
				return err
			})
		}()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			failures = append(failures, Failure{Step: "reading-identity", File: line.File, Error: err.Error()})
		}
	}
	return failures, nil
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
	return buildCorrectionProofEntries(needed, func(file string) (*Facts, []int, error) {
		f, pos, _, _, err := w.prepare(file, false)
		return f, pos, err
	}, func(source string, f *Facts, pos []int) (resolvedReadingKeys, error) {
		var resolved resolvedReadingKeys
		err := trial.DryRun(ctx, source, func(t *core.Tx) error {
			var err error
			resolved, err = resolveReadingKeys(t, source, f, pos)
			return err
		})
		return resolved, err
	})
}

func buildCorrectionProofEntries(needed map[correctionRoot]bool, prepare func(string) (*Facts, []int, error), resolve func(string, *Facts, []int) (resolvedReadingKeys, error)) (*correctionProof, error) {
	proof := &correctionProof{entries: map[correctionRoot]correctionProofEntry{}}
	type groupKey struct{ source, file string }
	groups := map[groupKey][]correctionRoot{}
	for root := range needed {
		if parsed, ok := parseReadingKey(root.key); ok {
			g := groupKey{root.source, parsed.file}
			groups[g] = append(groups[g], root)
		}
	}
	// One checked snapshot and trial resolution per source/file, scoped to this invocation.
	for group, roots := range groups {
		f, pos, err := prepare(group.file)
		if err != nil {
			return nil, err
		}
		resolved, err := resolve(group.source, f, pos)
		if err != nil {
			return nil, err
		}
		for _, root := range roots {
			idx, ok := resolved.storedKeyWrite[root.key]
			if !ok {
				return nil, refuse("%s: correction root %s for %s has no validated original trial reading", group.file, root.key, root.metric)
			}
			proof.entries[root] = correctionProofEntry{facts: f, positions: pos, writeIndex: idx}
		}
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
