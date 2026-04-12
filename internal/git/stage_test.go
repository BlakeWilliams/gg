package git

import (
	"strings"
	"testing"
)

func TestBuildPartialPatch_SingleAddition(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added line\n+another line\n context end"

	// Stage only the first addition (new line 2).
	result := buildPartialPatch("test.go", "modified", patch, []int{2}, nil)

	if result == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(result, "+added line") {
		t.Error("expected +added line in patch")
	}
	// "another line" should NOT be in the patch as an addition.
	if strings.Contains(result, "+another line") {
		t.Error("should not contain +another line")
	}
}

func TestBuildPartialPatch_SingleDeletion(t *testing.T) {
	patch := "@@ -1,4 +1,2 @@\n context\n-removed one\n-removed two\n context end"

	// Stage only the first deletion (old line 2).
	result := buildPartialPatch("test.go", "modified", patch, nil, []int{2})

	if result == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(result, "-removed one") {
		t.Error("expected -removed one in patch")
	}
	// "removed two" should be converted to context (kept as unchanged).
	if strings.Contains(result, "-removed two") {
		t.Error("should not contain -removed two as deletion")
	}
	if !strings.Contains(result, " removed two") {
		t.Error("expected 'removed two' as context line")
	}
}

func TestBuildPartialPatch_NoSelection(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added line\n context end"

	result := buildPartialPatch("test.go", "modified", patch, nil, nil)

	if result != "" {
		t.Error("expected empty patch when nothing selected")
	}
}

func TestBuildPartialPatch_AllSelected(t *testing.T) {
	patch := "@@ -1,3 +1,5 @@\n context\n+line a\n+line b\n context end"

	result := buildPartialPatch("test.go", "modified", patch, []int{2, 3}, nil)

	if result == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(result, "+line a") {
		t.Error("missing +line a")
	}
	if !strings.Contains(result, "+line b") {
		t.Error("missing +line b")
	}
}

func TestBuildPartialPatch_HasDiffHeader(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n+added\n context end"

	result := buildPartialPatch("test.go", "modified", patch, []int{2}, nil)

	if !strings.HasPrefix(result, "diff --git a/test.go b/test.go\n") {
		t.Error("expected diff --git header")
	}
	if !strings.Contains(result, "--- a/test.go\n") {
		t.Error("expected --- header")
	}
	if !strings.Contains(result, "+++ b/test.go\n") {
		t.Error("expected +++ header")
	}
}

func TestBuildHunkPatch_FindsCorrectHunk(t *testing.T) {
	patch := "@@ -1,3 +1,3 @@\n first\n-old\n+new\n last\n@@ -10,3 +10,4 @@\n ctx\n+target line\n ctx end"

	// Target line is in the second hunk at new line 11.
	result := buildHunkPatch("test.go", "modified", patch, 11, "RIGHT")

	if result == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(result, "+target line") {
		t.Error("expected +target line")
	}
	// Should NOT contain the first hunk.
	if strings.Contains(result, "+new") {
		t.Error("should not contain first hunk's addition")
	}
}

func TestBuildHunkPatch_DeletionSide(t *testing.T) {
	patch := "@@ -1,3 +1,2 @@\n context\n-deleted line\n context end"

	result := buildHunkPatch("test.go", "modified", patch, 2, "LEFT")

	if result == "" {
		t.Fatal("expected non-empty patch")
	}
	if !strings.Contains(result, "-deleted line") {
		t.Error("expected -deleted line")
	}
}

func TestBuildHunkPatch_NotFound(t *testing.T) {
	patch := "@@ -1,3 +1,3 @@\n context\n+added\n context end"

	result := buildHunkPatch("test.go", "modified", patch, 99, "RIGHT")

	if result != "" {
		t.Error("expected empty patch for non-existent line")
	}
}
