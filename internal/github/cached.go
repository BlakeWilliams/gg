package github

import (
	"context"
	"fmt"
	"time"

	"github.com/blakewilliams/ghq/internal/cache"
)

// CachedClient wraps a Client with TanStack Query-style caching.
type CachedClient struct {
	client *Client
	cache  *cache.Cache
}

// NewCachedClient creates a CachedClient wrapping the given Client.
func NewCachedClient(client *Client, opts cache.Options) *CachedClient {
	return &CachedClient{
		client: client,
		cache:  cache.New(opts),
	}
}

// Scoped returns a new CachedClient targeting the given owner/repo,
// sharing the same cache and REST/GraphQL clients.
func (c *CachedClient) Scoped(owner, repo string) *CachedClient {
	return &CachedClient{
		client: &Client{
			rest:  c.client.rest,
			gql:   c.client.gql,
			owner: owner,
			repo:  repo,
		},
		cache: c.cache,
	}
}

// Owner returns the repo owner.
func (c *CachedClient) Owner() string { return c.client.owner }

// Repo returns the repo name.
func (c *CachedClient) Repo() string { return c.client.repo }

// RepoFullName returns the owner/repo string.
func (c *CachedClient) RepoFullName() string {
	return c.client.RepoFullName()
}

// GCInterval returns the configured GC interval for tick commands.
func (c *CachedClient) GCInterval() time.Duration {
	return c.cache.GCInterval()
}

// GC runs garbage collection on the cache.
func (c *CachedClient) GC() {
	c.cache.GC()
}

// InvalidatePR removes cached data for a specific PR.
func (c *CachedClient) InvalidatePR(number int) {
	c.cache.Invalidate(fmt.Sprintf("pull:%d", number))
	c.cache.Invalidate(fmt.Sprintf("pull-files:%d", number))
}

// InvalidateAll clears the entire cache.
func (c *CachedClient) InvalidateAll() {
	c.cache.InvalidatePrefix("")
}

// --- Cached query methods ---
// These return (data, found, refetchFn) from the cache layer.
// found=true means cached data is available immediately.
// refetchFn is non-nil when the data needs a fresh fetch (cache miss or stale).

// ListPullRequests returns cached PRs and an optional refetch function.
func (c *CachedClient) ListPullRequests() (data []PullRequest, found bool, refetch func() ([]PullRequest, error)) {
	fetchFn := func() ([]PullRequest, error) {
		return c.client.ListPullRequests(context.Background())
	}
	data, found, _, refetch = cache.Query(c.cache, "pulls", fetchFn)
	return
}

// GetReviews returns cached reviews and an optional refetch function.
func (c *CachedClient) GetReviews(number int) (data []Review, found bool, refetch func() ([]Review, error)) {
	key := fmt.Sprintf("reviews:%d", number)
	fetchFn := func() ([]Review, error) {
		return c.client.GetReviews(context.Background(), number)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// GetIssueComments returns cached comments and an optional refetch function.
func (c *CachedClient) GetIssueComments(number int) (data []IssueComment, found bool, refetch func() ([]IssueComment, error)) {
	key := fmt.Sprintf("comments:%d", number)
	fetchFn := func() ([]IssueComment, error) {
		return c.client.GetIssueComments(context.Background(), number)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// GetReviewComments returns cached review comments and an optional refetch function.
func (c *CachedClient) GetReviewComments(number int) (data []ReviewComment, found bool, refetch func() ([]ReviewComment, error)) {
	key := fmt.Sprintf("review-comments:%d", number)
	fetchFn := func() ([]ReviewComment, error) {
		return c.client.GetReviewComments(context.Background(), number)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// GetCheckRuns returns cached check runs and an optional refetch function.
func (c *CachedClient) GetCheckRuns(ref string) (data []CheckRun, found bool, refetch func() ([]CheckRun, error)) {
	key := fmt.Sprintf("check-runs:%s", ref)
	fetchFn := func() ([]CheckRun, error) {
		return c.client.GetCheckRuns(context.Background(), ref)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// GetPullRequestFiles returns cached PR files and an optional refetch function.
func (c *CachedClient) GetPullRequestFiles(number int) (data []PullRequestFile, found bool, refetch func() ([]PullRequestFile, error)) {
	key := fmt.Sprintf("pull-files:%d", number)
	fetchFn := func() ([]PullRequestFile, error) {
		return c.client.GetPullRequestFiles(context.Background(), number)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// GetFileContent returns cached file content and an optional refetch function.
func (c *CachedClient) GetFileContent(filename, ref string) (data string, found bool, refetch func() (string, error)) {
	key := fmt.Sprintf("file-content:%s:%s", ref, filename)
	fetchFn := func() (string, error) {
		return c.client.GetFileContent(context.Background(), filename, ref)
	}
	data, found, _, refetch = cache.Query(c.cache, key, fetchFn)
	return
}

// FetchFileContent synchronously fetches file content, using the cache.
// Intended for use inside a goroutine.
func (c *CachedClient) FetchFileContent(filename, ref string) (string, error) {
	key := fmt.Sprintf("file-content:%s:%s", ref, filename)
	fetchFn := func() (string, error) {
		return c.client.GetFileContent(context.Background(), filename, ref)
	}

	data, found, _, refetchFn := cache.Query(c.cache, key, fetchFn)
	if found {
		return data, nil
	}
	if refetchFn != nil {
		return refetchFn()
	}
	return "", fmt.Errorf("failed to fetch %s", filename)
}

// --- Direct fetch methods (no caching) ---

// FetchAuthenticatedUser fetches the current user.
func (c *CachedClient) FetchAuthenticatedUser() (User, error) {
	return c.client.GetAuthenticatedUser(context.Background())
}

// FetchInbox fetches all inbox PRs with GraphQL enrichment.
func (c *CachedClient) FetchInbox(username, nwo string) ([]InboxPR, error) {
	prs, err := c.client.FetchInboxPRs(context.Background(), username, nwo)
	if err != nil {
		return nil, err
	}
	prs, _ = c.client.EnrichPRs(context.Background(), prs, username)
	return prs, nil
}

// FetchPR fetches a single PR by owner/repo/number.
func (c *CachedClient) FetchPR(owner, repo string, number int) (PullRequest, error) {
	return c.client.GetPullRequestFromRepo(context.Background(), owner, repo, number)
}

// FetchPRByBranch finds an open PR for the given branch.
func (c *CachedClient) FetchPRByBranch(branch string) (PullRequest, error) {
	return c.client.GetPullRequestByBranch(context.Background(), branch)
}

// CreateReviewComment posts a new review comment.
func (c *CachedClient) CreateReviewComment(number int, body, commitID, path string, line int, side string, startLine int, startSide string) (ReviewComment, error) {
	comment, err := c.client.CreateReviewComment(context.Background(), number, body, commitID, path, line, side, startLine, startSide)
	if err != nil {
		return ReviewComment{}, err
	}
	c.cache.Invalidate(fmt.Sprintf("review-comments:%d", number))
	return comment, nil
}

// ReplyToReviewComment replies to an existing comment thread.
func (c *CachedClient) ReplyToReviewComment(number int, commentID int, body string) (ReviewComment, error) {
	comment, err := c.client.ReplyToReviewComment(context.Background(), number, commentID, body)
	if err != nil {
		return ReviewComment{}, err
	}
	c.cache.Invalidate(fmt.Sprintf("review-comments:%d", number))
	return comment, nil
}
