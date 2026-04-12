package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/blakewilliams/ghq/internal/cache"
	"github.com/blakewilliams/ghq/internal/git"
	"github.com/blakewilliams/ghq/internal/github"
	"github.com/blakewilliams/ghq/internal/ui"
	"github.com/cli/go-gh/v2/pkg/repository"
	tea "charm.land/bubbletea/v2"
)

func main() {
	owner := flag.String("owner", "", "repository owner")
	repo := flag.String("repo", "", "repository name")
	flag.Parse()

	// First positional arg is the mode: inbox, pr, diff
	mode := ""
	if flag.NArg() > 0 {
		mode = flag.Arg(0)
	}

	// Detect owner/repo from local git remote if not specified.
	detectedOwner, detectedRepo := *owner, *repo
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
	}))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
