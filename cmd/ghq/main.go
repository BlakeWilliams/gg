package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/blakewilliams/ghq/internal/cache"
	"github.com/blakewilliams/ghq/internal/git"
	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui"
	"github.com/cli/go-gh/v2/pkg/repository"
	tea "charm.land/bubbletea/v2"
)

func main() {
	// Parse the first positional argument
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}

	var mode string
	var detectedOwner, detectedRepo string
	var prNumber int

	switch arg {
	case "inbox":
		mode = "inbox"
	case ".", "review":
		mode = "diff"
	case "":
		// No arg, will show picker
	default:
		// Try to interpret as a path: owner/repo, owner/repo/pulls, owner/repo/pull/123
		mode, detectedOwner, detectedRepo, prNumber = parsePath(arg)
	}

	// Detect owner/repo from local git remote if not specified.
	if detectedOwner == "" || detectedRepo == "" {
		if r, err := repository.Current(); err == nil {
			detectedOwner = r.Owner
			detectedRepo = r.Name
		}
	}

	client, err := github.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cachedClient := github.NewCachedClient(client, cache.Options{
		StaleTime:  30 * time.Second,
		GCTime:     5 * time.Minute,
		GCInterval: 1 * time.Minute,
	})

	repoRoot, _ := git.RepoRoot(".")

	p := tea.NewProgram(ui.NewApp(ui.AppConfig{
		Client:   cachedClient,
		Owner:    detectedOwner,
		Repo:     detectedRepo,
		RepoRoot: repoRoot,
		Mode:     mode,
		PRNumber: prNumber,
	}))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// parsePath interprets a path-style argument like:
// - owner/repo -> pulls list
// - owner/repo/pulls -> pulls list
// - owner/repo/pull/123 or owner/repo/pulls/123 -> specific PR
func parsePath(arg string) (mode, owner, repo string, prNumber int) {
	// Match owner/repo patterns with optional /pulls or /pull/N suffix
	prPattern := regexp.MustCompile(`^([^/]+)/([^/]+)(?:/pulls?(?:/(\d+))?)?$`)
	matches := prPattern.FindStringSubmatch(arg)
	if matches == nil {
		return "", "", "", 0
	}

	owner = matches[1]
	repo = matches[2]

	if matches[3] != "" {
		// PR number specified
		prNumber, _ = strconv.Atoi(matches[3])
		mode = "pr"
	} else {
		// Just owner/repo or owner/repo/pulls
		mode = "pulls"
	}

	// Strip .git suffix if present
	repo = strings.TrimSuffix(repo, ".git")

	return mode, owner, repo, prNumber
}
