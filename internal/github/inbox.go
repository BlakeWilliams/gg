package github

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FetchInboxPRs fetches all PRs relevant to the given username from all repos.
// It makes 4 parallel searches and deduplicates the results.
// If nwo is non-empty, results are scoped to that repo.
func (c *Client) FetchInboxPRs(ctx context.Context, username, nwo string) ([]InboxPR, error) {
	lookback := time.Now().AddDate(0, -3, 0).Format("2006-01-02")

	repoFilter := ""
	if nwo != "" {
		repoFilter = "+repo:" + nwo
	}

	// Recently closed — 30-day lookback; they fade quickly via decay scoring.
	closedLookback := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	type query struct {
		source PRSource
		q      string
	}
	queries := []query{
		{SourceAuthored, fmt.Sprintf("author:%s+is:pr+is:open+updated:>%s%s", username, lookback, repoFilter)},
		{SourceReviewRequested, fmt.Sprintf("review-requested:%s+is:pr+is:open+updated:>%s%s", username, lookback, repoFilter)},
		{SourceMentioned, fmt.Sprintf("mentions:%s+is:pr+is:open+updated:>%s%s", username, lookback, repoFilter)},
		{SourceAssigned, fmt.Sprintf("assignee:%s+is:pr+is:open+updated:>%s%s", username, lookback, repoFilter)},
		// Closed/merged authored PRs from the last week.
		{SourceAuthored, fmt.Sprintf("author:%s+is:pr+is:closed+updated:>%s%s", username, closedLookback, repoFilter)},
	}

	type result struct {
		source PRSource
		items  []SearchIssueResult
		err    error
	}

	ch := make(chan result, len(queries))
	for _, qr := range queries {
		go func(s PRSource, q string) {
			items, err := c.SearchIssues(ctx, q)
			ch <- result{source: s, items: items, err: err}
		}(qr.source, qr.q)
	}

	// Collect results.
	type prKey struct {
		owner  string
		repo   string
		number int
	}
	merged := make(map[prKey]*InboxPR)

	for range queries {
		r := <-ch
		if r.err != nil {
			// Log but continue — partial results are better than none.
			continue
		}
		for _, item := range r.items {
			// Skip non-PR results.
			if item.PullRequest == nil {
				continue
			}

			repo := parseRepoFromURL(item.RepositoryURL)
			key := prKey{owner: repo.Owner, repo: repo.Name, number: item.Number}

			if existing, ok := merged[key]; ok {
				// Add source if not already present.
				if !existing.HasSource(r.source) {
					existing.Sources = append(existing.Sources, r.source)
				}
			} else {
				state := "open"
				if item.PullRequest.MergedAt != nil {
					state = "merged"
				} else if item.State == "closed" {
					state = "closed"
				} else if item.Draft {
					state = "draft"
				}

				pr := &InboxPR{
					Number:    item.Number,
					Title:     item.Title,
					State:     state,
					User:      item.User,
					Repo:      repo,
					UpdatedAt: item.UpdatedAt,
					CreatedAt: item.CreatedAt,
					Labels:    item.Labels,
					HTMLURL:   item.HTMLURL,
					Sources:   []PRSource{r.source},
				}
				merged[key] = pr
			}
		}
	}

	prs := make([]InboxPR, 0, len(merged))
	for _, pr := range merged {
		prs = append(prs, *pr)
	}
	return prs, nil
}

// EnrichPRs fetches extended state (review decision, CI, mergeable) for PRs
// via batched GraphQL queries (up to 50 PRs per request).
func (c *Client) EnrichPRs(ctx context.Context, prs []InboxPR, username string) ([]InboxPR, error) {
	const batchSize = 50

	for i := 0; i < len(prs); i += batchSize {
		end := i + batchSize
		if end > len(prs) {
			end = len(prs)
		}
		batch := prs[i:end]

		// Build batched GraphQL query.
		var queryParts []string
		for j, pr := range batch {
			alias := fmt.Sprintf("pr%d", j)
			part := fmt.Sprintf(`%s: repository(owner: %q, name: %q) {
				pullRequest(number: %d) {
					number
					mergeable
					reviewDecision
					reviews(last: 10, states: [APPROVED, CHANGES_REQUESTED]) {
						nodes { state submittedAt }
					}
					reviewRequests(first: 10) {
						nodes { requestedReviewer { ... on User { login } } }
					}
					commits(last: 1) {
						nodes { commit { committedDate statusCheckRollup { state } } }
					}
				}
			}`, alias, pr.Repo.Owner, pr.Repo.Name, pr.Number)
			queryParts = append(queryParts, part)
		}

		query := "{ " + strings.Join(queryParts, "\n") + " }"

		var resp map[string]struct {
			PullRequest *struct {
				Number         int    `json:"number"`
				Mergeable      string `json:"mergeable"`
				ReviewDecision string `json:"reviewDecision"`
				Reviews        *struct {
					Nodes []struct {
						State       string `json:"state"`
						SubmittedAt string `json:"submittedAt"`
					} `json:"nodes"`
				} `json:"reviews"`
				ReviewRequests *struct {
					Nodes []struct {
						RequestedReviewer *struct {
							Login string `json:"login"`
						} `json:"requestedReviewer"`
					} `json:"nodes"`
				} `json:"reviewRequests"`
				Commits *struct {
					Nodes []struct {
						Commit struct {
							CommittedDate string `json:"committedDate"`
							StatusCheckRollup *struct {
								State string `json:"state"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		}

		err := c.gql.Do(query, nil, &resp)
		if err != nil {
			// Continue with partial enrichment rather than failing entirely.
			continue
		}

		// Map results back to PRs.
		for j := range batch {
			alias := fmt.Sprintf("pr%d", j)
			data, ok := resp[alias]
			if !ok || data.PullRequest == nil {
				continue
			}
			pr := &prs[i+j]
			gqlPR := data.PullRequest

			// Mergeable.
			switch gqlPR.Mergeable {
			case "MERGEABLE":
				t := true
				pr.Mergeable = &t
			case "CONFLICTING":
				f := false
				pr.Mergeable = &f
			}

			// Review decision.
			pr.ReviewDecision = gqlPR.ReviewDecision

			// Review requested for this user.
			if gqlPR.ReviewRequests != nil {
				for _, rr := range gqlPR.ReviewRequests.Nodes {
					if rr.RequestedReviewer != nil && strings.EqualFold(rr.RequestedReviewer.Login, username) {
						pr.ReviewRequested = true
						break
					}
				}
			}

			// CI status.
			if gqlPR.Commits != nil && len(gqlPR.Commits.Nodes) > 0 {
				commit := gqlPR.Commits.Nodes[0].Commit
				if commit.StatusCheckRollup != nil {
					switch commit.StatusCheckRollup.State {
					case "SUCCESS":
						pr.CIStatus = "success"
					case "FAILURE":
						pr.CIStatus = "failure"
					case "ERROR":
						pr.CIStatus = "error"
					case "PENDING", "EXPECTED":
						pr.CIStatus = "pending"
					}
				}
				if commit.CommittedDate != "" {
					if t, err := time.Parse(time.RFC3339, commit.CommittedDate); err == nil {
						pr.LatestCommitAt = &t
					}
				}
			}

			// Latest review date.
			if gqlPR.Reviews != nil {
				for _, rev := range gqlPR.Reviews.Nodes {
					if rev.SubmittedAt != "" {
						if t, err := time.Parse(time.RFC3339, rev.SubmittedAt); err == nil {
							if pr.LatestReviewAt == nil || t.After(*pr.LatestReviewAt) {
								pr.LatestReviewAt = &t
							}
						}
					}
				}
			}
		}
	}

	return prs, nil
}

// parseRepoFromURL extracts owner/repo from a repository_url like
// "https://api.github.com/repos/owner/repo".
func parseRepoFromURL(url string) RepoRef {
	// URL format: https://api.github.com/repos/{owner}/{repo}
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return RepoRef{
			Owner: parts[len(parts)-2],
			Name:  parts[len(parts)-1],
		}
	}
	return RepoRef{}
}
