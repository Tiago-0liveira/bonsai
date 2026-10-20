package work

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

// Draft is one idea that has not been accepted. Attention is non-empty when
// the item cannot be trusted as a plain draft (competing revision heads, bad
// metadata, conflicting labels): the row stays visible so a concurrent edit
// never makes an idea disappear.
type Draft struct {
	Item      gardensync.Item
	Attention string
	Findings  []gardensync.Finding
}

// DraftCollection is the Drafts inbox. Total counts every draft before the
// query; Excluded counts items that belong to Board/Table (or are unusable).
type DraftCollection struct {
	Records         []Draft
	Total, Excluded int
}

// Drafts selects unvalidated ideas of any type. It never trusts a cached lane
// to include or exclude; acceptance is recomputed like Select does, so type
// and stage labels alone can never turn an idea into Work or hide it.
func Drafts(items []gardensync.Item, q Query, findings ...gardensync.Finding) DraftCollection {
	var out DraftCollection
	for _, it := range items {
		if it.Deleted || it.Issue.ID == 0 {
			out.Excluded++
			continue
		}
		meta, err := garden.DecodeMetadata(it.Issue.Metadata)
		c := garden.Classify(it.Issue.Labels)
		lane := garden.Lane(c, meta, it.Verification, err)
		if lane.Lane == garden.LaneWork && c.Stage != garden.StageDraft && len(it.Issue.Problems) == 0 {
			out.Excluded++
			continue
		}
		d := Draft{Item: it}
		d.Item.Classification = c
		switch {
		case meta.Accepted && c.Stage == garden.StageDraft && lane.Lane == garden.LaneWork:
			// Board excludes every draft-stage item, so an accepted one with a stale draft
			// label would otherwise vanish from both views.
			d.Attention = "specification is accepted but the item is labelled draft"
		case lane.Lane == garden.LaneAttention:
			d.Attention = lane.Reason
		case len(it.Issue.Problems) > 0:
			d.Attention = "Issue was reported with unusable data"
		case len(it.Problems) > 0:
			d.Attention = it.Problems[0].Message
		case c.Stage != garden.StageDraft:
			d.Attention = "unvalidated idea is not labelled draft"
		}
		out.Total++
		for _, f := range findings {
			if f.IssueID == it.Issue.ID {
				d.Findings = append(d.Findings, f)
			}
		}
		hay := strings.ToLower(fmt.Sprintf("#%d %s", it.Issue.Number, it.Issue.Title))
		if !strings.Contains(hay, strings.ToLower(strings.TrimSpace(q.Text))) || q.Type != "" && q.Type != c.Type {
			continue
		}
		out.Records = append(out.Records, d)
	}
	sort.Slice(out.Records, func(i, j int) bool {
		a, b := out.Records[i].Item.Issue, out.Records[j].Item.Issue
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		return a.ID < b.ID
	})
	return out
}

// Find returns the draft with the given Issue ID and its row index.
func (c DraftCollection) Find(id garden.IssueID) (Draft, int, bool) {
	for i, d := range c.Records {
		if d.Item.Issue.ID == id {
			return d, i, true
		}
	}
	return Draft{}, 0, false
}
