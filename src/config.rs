use std::path::PathBuf;

use serde::Deserialize;

const DEFAULT_COMMENT_PANEL_MIN_WIDTH: u16 = 40;
const DEFAULT_DIFF_MIN_WIDTH: u16 = 80;
const DEFAULT_SCROLL_MARGIN: usize = 5;
const DEFAULT_PLAN_PATH: &str = ".gg/plan.md";

/// How agents are isolated from one another on disk.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum AgentIsolation {
    /// Each agent works in its own git worktree (default).
    #[default]
    Worktree,
    /// All agents share a single checkout; selecting an agent switches branches.
    Checkout,
}

/// Which agent backend powers the workspace agents.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum AgentBackend {
    /// GitHub Copilot SDK (default).
    #[default]
    Copilot,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(default)]
pub struct Config {
    pub help_mode: Option<String>,
    pub commit_prompt: Option<String>,
    pub pr_prompt: Option<String>,
    pub comment_panel_min_width: u16,
    pub diff_min_width: u16,
    pub scroll_margin: usize,
    /// Agent isolation strategy. Defaults to `worktree`.
    pub agent_isolation: AgentIsolation,
    /// Agent backend. Defaults to `copilot`.
    pub agent_backend: AgentBackend,
    /// Base directory for per-agent worktrees. When unset, defaults to
    /// `<repo>/.git/gg-worktrees`.
    pub worktree_root: Option<String>,
    /// Repo-relative path the agent writes its plan to. Defaults to `.gg/plan.md`.
    pub plan_path: String,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            help_mode: None,
            commit_prompt: None,
            pr_prompt: None,
            comment_panel_min_width: DEFAULT_COMMENT_PANEL_MIN_WIDTH,
            diff_min_width: DEFAULT_DIFF_MIN_WIDTH,
            scroll_margin: DEFAULT_SCROLL_MARGIN,
            agent_isolation: AgentIsolation::default(),
            agent_backend: AgentBackend::default(),
            worktree_root: None,
            plan_path: DEFAULT_PLAN_PATH.to_string(),
        }
    }
}

impl Config {
    pub fn load() -> Self {
        match Self::path() {
            Some(path) => Self::load_from(&path),
            None => Self::default(),
        }
    }

    pub fn load_from(path: &std::path::Path) -> Self {
        let contents = match std::fs::read_to_string(path) {
            Ok(c) => c,
            Err(_) => return Self::default(),
        };
        let mut config: Config = match serde_yaml::from_str(&contents) {
            Ok(c) => c,
            Err(_) => return Self::default(),
        };
        config.clamp_minimums();
        config
    }

    fn clamp_minimums(&mut self) {
        if self.comment_panel_min_width < DEFAULT_COMMENT_PANEL_MIN_WIDTH {
            self.comment_panel_min_width = DEFAULT_COMMENT_PANEL_MIN_WIDTH;
        }
        if self.diff_min_width < DEFAULT_DIFF_MIN_WIDTH {
            self.diff_min_width = DEFAULT_DIFF_MIN_WIDTH;
        }
        if self.plan_path.trim().is_empty() {
            self.plan_path = DEFAULT_PLAN_PATH.to_string();
        }
    }

    pub fn dir() -> Option<PathBuf> {
        if let Ok(xdg) = std::env::var("XDG_CONFIG_HOME") {
            Some(PathBuf::from(xdg).join("gg"))
        } else {
            dirs::home_dir().map(|h| h.join(".config").join("gg"))
        }
    }

    pub fn path() -> Option<PathBuf> {
        Self::dir().map(|d| d.join("config.yaml"))
    }
}
