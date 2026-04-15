package main

import (
	"testing"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantMode     string
		wantOwner    string
		wantRepo     string
		wantPRNumber int
	}{
		{
			name:         "owner/repo returns pulls mode",
			input:        "owner/repo",
			wantMode:     "pulls",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 0,
		},
		{
			name:         "owner/repo/pulls returns pulls mode",
			input:        "owner/repo/pulls",
			wantMode:     "pulls",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 0,
		},
		{
			name:         "owner/repo/pull returns pulls mode",
			input:        "owner/repo/pull",
			wantMode:     "pulls",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 0,
		},
		{
			name:         "owner/repo/pulls/123 returns pr mode with number",
			input:        "owner/repo/pulls/123",
			wantMode:     "pr",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 123,
		},
		{
			name:         "owner/repo/pull/123 returns pr mode with number",
			input:        "owner/repo/pull/123",
			wantMode:     "pr",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 123,
		},
		{
			name:         "strips .git suffix from repo",
			input:        "owner/repo.git",
			wantMode:     "pulls",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 0,
		},
		{
			name:         "strips .git suffix with pulls path",
			input:        "owner/repo.git/pulls/456",
			wantMode:     "pr",
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantPRNumber: 456,
		},
		{
			name:         "invalid input returns empty",
			input:        "invalid",
			wantMode:     "",
			wantOwner:    "",
			wantRepo:     "",
			wantPRNumber: 0,
		},
		{
			name:         "just a number returns empty",
			input:        "123",
			wantMode:     "",
			wantOwner:    "",
			wantRepo:     "",
			wantPRNumber: 0,
		},
		{
			name:         "owner/repo/123 without pull keyword returns empty",
			input:        "owner/repo/123",
			wantMode:     "",
			wantOwner:    "",
			wantRepo:     "",
			wantPRNumber: 0,
		},
		{
			name:         "trailing slash on pulls returns empty",
			input:        "owner/repo/pulls/",
			wantMode:     "",
			wantOwner:    "",
			wantRepo:     "",
			wantPRNumber: 0,
		},
		{
			name:         "real world example blakewilliams/githq/pull/69",
			input:        "blakewilliams/githq/pull/69",
			wantMode:     "pr",
			wantOwner:    "blakewilliams",
			wantRepo:     "githq",
			wantPRNumber: 69,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, owner, repo, prNumber := parsePath(tt.input)

			if mode != tt.wantMode {
				t.Errorf("parsePath(%q) mode = %q, want %q", tt.input, mode, tt.wantMode)
			}
			if owner != tt.wantOwner {
				t.Errorf("parsePath(%q) owner = %q, want %q", tt.input, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("parsePath(%q) repo = %q, want %q", tt.input, repo, tt.wantRepo)
			}
			if prNumber != tt.wantPRNumber {
				t.Errorf("parsePath(%q) prNumber = %d, want %d", tt.input, prNumber, tt.wantPRNumber)
			}
		})
	}
}
