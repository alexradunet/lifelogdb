package api_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestTemporalCollectionEvidenceInEitherOrderAndMemberActions(t *testing.T) {
	for _, localFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(localFirst), func(t *testing.T) {
			c, h := fresh(t)
			actions := must(c.Get("/actions"))
			kind := must(c.Do(find(actions, "create-page"), map[string]string{"title": "Workout & rest"}))
			days := []string{"2020-01-01", "2020-01-02"}
			if localFirst {
				days[0], days[1] = days[1], days[0]
			}
			utc := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout & rest", "day": days[0], "start_at": "2020-01-01T12:00:00.000Z"}))
			local := must(c.Do(find(actions, "capture-session"), map[string]string{"kind": "Workout & rest", "day": days[1], "start_local": "2020-01-01T23:00:00.000", "end_local": "2020-01-02T07:00:00.000", "start_zone_unverified": "Claimed/Zone", "end_zone_unverified": "Other/Claim"}))
			tomb := must(c.Do(find(local, "tombstone-session"), nil))
			must(c.Do(find(kind, "tombstone"), nil))
			list := must(c.Get("/sessions?include_deleted=1"))
			body := browse(t, h, "/sessions?include_deleted=1")
			for _, want := range []string{"2020-01-01T23:00:00.000", "2020-01-02T07:00:00.000", "Claimed/Zone", "Other/Claim", "kind_deleted_at", "deleted_at", "missing end", "unresolved", "Workout &amp; rest"} {
				if !strings.Contains(body, want) {
					t.Fatalf("collection omitted %q", want)
				}
			}
			seen := map[string]bool{}
			for _, member := range list.Entities {
				seen[member.Href] = true
				detail := must(c.Get(member.Href))
				deleted := member.Href == href(tomb, "self")
				for name, want := range map[string]bool{
					"revive-session":    deleted,
					"edit-session":      !deleted,
					"tombstone-session": !deleted,
				} {
					if got := find(detail, name).Name != ""; got != want {
						t.Fatalf("member %s action %s present=%v want=%v", member.Href, name, got, want)
					}
				}
			}
			if !seen[href(utc, "self")] || !seen[href(local, "self")] {
				t.Fatal("members not navigable")
			}
		})
	}
}
