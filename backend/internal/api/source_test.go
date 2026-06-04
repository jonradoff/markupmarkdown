package api

import (
	"testing"

	"markupmarkdown/internal/models"
)

func TestIsDocLevel(t *testing.T) {
	cases := []struct {
		name string
		a    models.Anchor
		want bool
	}{
		{"empty", models.Anchor{}, true},
		{"only-whitespace-exact", models.Anchor{Exact: "   "}, true},
		{"with-text", models.Anchor{Start: 0, End: 4, Exact: "abcd"}, false},
		{"start-end-set-empty-exact", models.Anchor{Start: 0, End: 10}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDocLevel(tc.a); got != tc.want {
				t.Errorf("isDocLevel(%+v)=%v want %v", tc.a, got, tc.want)
			}
		})
	}
}

func TestReanchorComments_CleanWhenExactFound(t *testing.T) {
	comments := []models.Comment{{
		ID:     "c1",
		Anchor: models.Anchor{Start: 5, End: 10, Exact: "hello"},
	}}
	out := reanchorComments(comments, "say hello world")
	if out[0].Status != reanchorClean {
		t.Fatalf("status=%v want clean", out[0].Status)
	}
	if out[0].Exact != "hello" {
		t.Fatalf("exact=%q want hello", out[0].Exact)
	}
}

func TestReanchorComments_OrphanWhenExactGone(t *testing.T) {
	comments := []models.Comment{{
		ID:     "c1",
		Anchor: models.Anchor{Start: 5, End: 12, Exact: "goodbye"},
	}}
	out := reanchorComments(comments, "say hello world")
	if out[0].Status != reanchorOrphan {
		t.Fatalf("status=%v want orphan", out[0].Status)
	}
	if out[0].OriginalExact != "goodbye" {
		t.Fatalf("originalExact=%q want goodbye", out[0].OriginalExact)
	}
}

func TestReanchorComments_DocLevelLeftAlone(t *testing.T) {
	comments := []models.Comment{{
		ID:     "c1",
		Anchor: models.Anchor{}, // doc-level: empty
	}}
	out := reanchorComments(comments, "anything")
	if out[0].Status != reanchorDocLevel {
		t.Fatalf("status=%v want docLevel", out[0].Status)
	}
}

func TestReanchorComments_OrphanRevivesWhenSourceRestored(t *testing.T) {
	// Comment was previously marked orphan. anchor.exact is preserved
	// from before. If the user reverts the source and the original text
	// reappears, the next sync un-orphans the comment.
	comments := []models.Comment{{
		ID: "c1",
		Anchor: models.Anchor{
			Start: 5, End: 16,
			Exact: "hello there",
		},
		Orphan:        true,
		OriginalExact: "hello there",
	}}
	out := reanchorComments(comments, "well hello there friend")
	if out[0].Status != reanchorClean {
		t.Fatalf("status=%v want clean", out[0].Status)
	}
	if out[0].Exact != "hello there" {
		t.Fatalf("exact=%q want 'hello there'", out[0].Exact)
	}
}

func TestValidateAnchor_AllowsDocLevel(t *testing.T) {
	if err := ValidateAnchor(models.Anchor{}); err != nil {
		t.Fatalf("doc-level anchor should validate: %v", err)
	}
}

func TestValidateAnchor_RejectsInvalidRange(t *testing.T) {
	if err := ValidateAnchor(models.Anchor{Start: 10, End: 5, Exact: "x"}); err == nil {
		t.Fatal("invalid range should fail")
	}
}

func TestValidateManualAnchor_RequiresExactInContent(t *testing.T) {
	err := validateManualAnchor(patchCommentAnchorRequest{
		Start: 0, End: 5, Exact: "missing",
	}, "this is the doc content")
	if err == nil {
		t.Fatal("expected error when exact is not in content")
	}
}

func TestValidateManualAnchor_AcceptsMatchInContent(t *testing.T) {
	err := validateManualAnchor(patchCommentAnchorRequest{
		Start: 0, End: 5, Exact: "this",
	}, "this is the doc content")
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
}
