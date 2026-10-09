package github

import "testing"

func TestSummarizeReviewsCountsStandingVerdicts(t *testing.T) {
	cases := []struct {
		name    string
		reviews []Review
		want    ReviewSummary
	}{
		{"none", nil, ReviewSummary{}},
		{"latest verdict wins", []Review{{Author: "a", State: "CHANGES_REQUESTED"}, {Author: "a", State: "APPROVED"}}, ReviewSummary{Approvals: 1}},
		{"comment keeps verdict", []Review{{Author: "a", State: "APPROVED"}, {Author: "a", State: "COMMENTED"}}, ReviewSummary{Approvals: 1}},
		{"pending ignored", []Review{{Author: "a", State: "PENDING"}}, ReviewSummary{}},
		{"dismissed clears", []Review{{Author: "a", State: "CHANGES_REQUESTED"}, {Author: "a", State: "DISMISSED"}}, ReviewSummary{}},
		{"mixed reviewers", []Review{{Author: "a", State: "APPROVED"}, {Author: "b", State: "CHANGES_REQUESTED"}, {Author: "c", State: "APPROVED"}}, ReviewSummary{Approvals: 2, ChangesRequested: 1}},
	}
	for _, c := range cases {
		if got := SummarizeReviews(c.reviews); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
