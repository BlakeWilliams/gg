//! Git worktree and branch management used by the agent workspace.
//!
//! Each workspace agent maps 1:1 to a git branch. In the default `worktree`
//! isolation mode, every agent gets its own checkout under a shared worktree
//! root so multiple agents can edit code in parallel without stomping each
//! other. In `checkout` mode the helpers fall back to switching branches in
//! the primary checkout.

use std::path::{Path, PathBuf};

use anyhow::{Context, Result, bail};
use tokio::process::Command;

/// A worktree linked to the repository, as reported by `git worktree list`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Worktree {
    pub path: String,
    pub branch: Option<String>,
}

async fn run_git(dir: &str, args: &[&str]) -> Result<String> {
    let mut full: Vec<&str> = vec!["-C", dir];
    full.extend_from_slice(args);
    let output = Command::new("git")
        .args(&full)
        .output()
        .await
        .with_context(|| format!("failed to run git {}", args.join(" ")))?;
    if !output.status.success() {
        bail!(
            "git {}: {}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr).trim()
        );
    }
    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

/// Returns true if a local branch with the given name exists.
pub async fn branch_exists(dir: &str, branch: &str) -> bool {
    Command::new("git")
        .args([
            "-C",
            dir,
            "rev-parse",
            "--verify",
            "--quiet",
            &format!("refs/heads/{branch}"),
        ])
        .output()
        .await
        .map(|o| o.status.success())
        .unwrap_or(false)
}

/// Create a new local branch off `base` without checking it out.
/// No-op if the branch already exists.
#[allow(dead_code)]
pub async fn create_branch(dir: &str, branch: &str, base: &str) -> Result<()> {
    if branch_exists(dir, branch).await {
        return Ok(());
    }
    run_git(dir, &["branch", branch, base]).await?;
    Ok(())
}

/// Default worktree root: `<repo>/.git/gg-worktrees`.
pub fn default_worktree_root(repo_root: &str) -> PathBuf {
    Path::new(repo_root).join(".git").join("gg-worktrees")
}

/// Sanitize a branch name into a filesystem-safe directory component.
pub fn sanitize_dir_name(branch: &str) -> String {
    branch
        .chars()
        .map(|c| match c {
            '/' | '\\' | ':' | ' ' => '-',
            other => other,
        })
        .collect()
}

/// Add a worktree for `branch` under `root`, creating the branch off `base`
/// first if it does not already exist. Returns the worktree path. If a
/// worktree for the branch already exists, returns its existing path.
pub async fn add_worktree(
    repo_root: &str,
    root: &Path,
    branch: &str,
    base: &str,
) -> Result<String> {
    // Reuse an existing worktree if one is already linked to this branch.
    if let Ok(existing) = list_worktrees(repo_root).await {
        if let Some(wt) = existing
            .into_iter()
            .find(|w| w.branch.as_deref() == Some(branch))
        {
            return Ok(wt.path);
        }
    }

    std::fs::create_dir_all(root)
        .with_context(|| format!("failed to create worktree root {}", root.display()))?;
    let target = root.join(sanitize_dir_name(branch));
    let target_str = target.to_string_lossy().to_string();

    let branch_present = branch_exists(repo_root, branch).await;
    if branch_present {
        run_git(repo_root, &["worktree", "add", &target_str, branch]).await?;
    } else {
        // Create the branch and the worktree in one shot.
        run_git(
            repo_root,
            &["worktree", "add", "-b", branch, &target_str, base],
        )
        .await?;
    }
    Ok(target_str)
}

/// Remove a worktree directory and prune the administrative metadata.
pub async fn remove_worktree(repo_root: &str, worktree_path: &str) -> Result<()> {
    run_git(repo_root, &["worktree", "remove", "--force", worktree_path]).await?;
    let _ = run_git(repo_root, &["worktree", "prune"]).await;
    Ok(())
}

/// List linked worktrees via `git worktree list --porcelain`.
pub async fn list_worktrees(repo_root: &str) -> Result<Vec<Worktree>> {
    let out = run_git(repo_root, &["worktree", "list", "--porcelain"]).await?;
    let mut worktrees = Vec::new();
    let mut path: Option<String> = None;
    let mut branch: Option<String> = None;
    for line in out.lines() {
        if let Some(p) = line.strip_prefix("worktree ") {
            // Flush previous entry.
            if let Some(p_prev) = path.take() {
                worktrees.push(Worktree {
                    path: p_prev,
                    branch: branch.take(),
                });
            }
            path = Some(p.to_string());
        } else if let Some(b) = line.strip_prefix("branch ") {
            branch = Some(b.strip_prefix("refs/heads/").unwrap_or(b).to_string());
        } else if line.is_empty() {
            if let Some(p_prev) = path.take() {
                worktrees.push(Worktree {
                    path: p_prev,
                    branch: branch.take(),
                });
            }
        }
    }
    if let Some(p_prev) = path.take() {
        worktrees.push(Worktree {
            path: p_prev,
            branch,
        });
    }
    Ok(worktrees)
}

/// Switch the primary checkout to `branch`, creating it off `base` if needed.
/// Used in `checkout` isolation mode.
pub async fn switch_branch(dir: &str, branch: &str, base: &str) -> Result<()> {
    if branch_exists(dir, branch).await {
        run_git(dir, &["switch", branch]).await?;
    } else {
        run_git(dir, &["switch", "-c", branch, base]).await?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitizes_branch_names() {
        assert_eq!(sanitize_dir_name("feature/foo"), "feature-foo");
        assert_eq!(sanitize_dir_name("bmw/agent x"), "bmw-agent-x");
        assert_eq!(sanitize_dir_name("simple"), "simple");
    }

    #[test]
    fn default_root_under_git() {
        let root = default_worktree_root("/tmp/repo");
        assert!(root.ends_with("gg-worktrees"));
        assert!(root.to_string_lossy().contains(".git"));
    }
}
