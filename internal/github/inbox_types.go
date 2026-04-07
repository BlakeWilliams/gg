package github

import "time"

// RepoRef identifies a repository by owner and name.
type RepoRef struct {
	Owner string
	Name  string
}

func (r RepoRef) FullName() string {
	return r.Owner + "/" + r.Name
}

// PRSource identifies how a PR relates to the current user.
type PRSource string

const (
	SourceAuthored        PRSource = "authored"
	SourceAssigned        PRSource = "assigned"
	SourceReviewRequested PRSource = "review_requested"
	SourceMentioned       PRSource = "mentioned"
)

// ActionReason describes why a PR needs attention.
type ActionReason string

const (
	ActionMerged            ActionReason = "merged"
	ActionMergeConflicts    ActionReason = "merge_conflicts"
	ActionCIFailed          ActionReason = "ci_failed"
	ActionChangesRequested  ActionReason = "changes_requested"
	ActionReadyToMerge      ActionReason = "ready_to_merge"
	ActionApproved          ActionReason = "approved"
	ActionCIPending         ActionReason = "ci_pending"
	ActionDraft             ActionReason = "draft"
	ActionWaitingForReview  ActionReason = "waiting_for_review"
	ActionReReviewRequested ActionReason = "re_review_requested"
	ActionReviewRequested   ActionReason = "review_requested"
	ActionMentioned         ActionReason = "mentioned"
	ActionClosed            ActionReason = "closed"
	ActionNone              ActionReason = ""
)

// SearchIssueResult is a single item from the GitHub search/issues endpoint.
type SearchIssueResult struct {
	Number        int       `json:"number"`
	Title         string    `json:"title"`
	State         string    `json:"state"`
	Draft         bool      `json:"draft"`
	User          User      `json:"user"`
	UpdatedAt     time.Time `json:"updated_at"`
	CreatedAt     time.Time `json:"created_at"`
	Labels        []Label   `json:"labels"`
	RepositoryURL string    `json:"repository_url"` // e.g. "https://api.github.com/repos/owner/repo"
	HTMLURL       string    `json:"html_url"`
	PullRequest   *struct {
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_request"`
}

// SearchResponse is the response from /search/issues.
type SearchResponse struct {
	TotalCount int                 `json:"total_count"`
	Items      []SearchIssueResult `json:"items"`
}

// InboxPR is an enriched PR for the inbox view.
type InboxPR struct {
	Number    int
	Title     string
	State     string // "open", "closed", "merged", "draft"
	User      User
	Repo      RepoRef
	UpdatedAt time.Time
	CreatedAt time.Time
	Labels    []Label
	HTMLURL   string
	Sources   []PRSource

	// GraphQL enrichment.
	ReviewDecision  string     // "APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", ""
	CIStatus        string     // "success", "failure", "pending", "error", ""
	Mergeable       *bool      // nil = unknown
	ReviewRequested bool       // user is explicitly requested as reviewer
	LatestCommitAt  *time.Time // date of the most recent commit
	LatestReviewAt  *time.Time // date of the most recent review

	// Computed.
	Action ActionReason
	Score  float64
}

// HasSource returns true if the PR has the given source.
func (p InboxPR) HasSource(s PRSource) bool {
	for _, src := range p.Sources {
		if src == s {
			return true
		}
	}
	return false
}
