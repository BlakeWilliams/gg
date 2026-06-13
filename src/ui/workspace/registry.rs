//! Persistence for the agent workspace. The registry stores the durable parts
//! of each agent (identity, branch, worktree, mode, conversation) so the
//! workspace can be reconstructed across runs. Runtime-only state (runner,
//! PR, diff, scroll) is rebuilt on load.

use serde::{Deserialize, Serialize};

use super::agent::{AgentMode, ChatMessage};
use crate::cache::persist;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AgentRecord {
    pub id: String,
    pub name: String,
    pub branch: String,
    pub worktree_path: String,
    pub mode: AgentMode,
    #[serde(default)]
    pub conversation: Vec<ChatMessage>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Registry {
    #[serde(default)]
    pub agents: Vec<AgentRecord>,
    #[serde(default)]
    pub selected: usize,
}

/// Filename for a repo's workspace registry, keyed by `owner/repo`.
fn filename(owner: &str, repo: &str) -> String {
    let key = format!("{owner}-{repo}").replace(['/', ' '], "-");
    format!("workspace-{key}.json")
}

impl Registry {
    pub fn load(owner: &str, repo: &str) -> Self {
        persist::load(&filename(owner, repo)).unwrap_or_default()
    }

    pub fn save(&self, owner: &str, repo: &str) {
        if let Err(e) = persist::save(&filename(owner, repo), self) {
            tracing::warn!("failed to persist workspace registry: {e}");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn filename_is_sanitized() {
        assert_eq!(filename("octo", "cat"), "workspace-octo-cat.json");
    }

    #[test]
    fn registry_roundtrips_via_json() {
        let reg = Registry {
            agents: vec![AgentRecord {
                id: "a1".into(),
                name: "Fix bug".into(),
                branch: "bmw/fix".into(),
                worktree_path: "/tmp/wt".into(),
                mode: AgentMode::Plan,
                conversation: vec![ChatMessage::user("hello".into())],
            }],
            selected: 0,
        };
        let json = serde_json::to_string(&reg).unwrap();
        let back: Registry = serde_json::from_str(&json).unwrap();
        assert_eq!(back.agents.len(), 1);
        assert_eq!(back.agents[0].branch, "bmw/fix");
        assert_eq!(back.agents[0].mode, AgentMode::Plan);
    }
}
