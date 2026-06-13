pub mod types;

pub mod copilot;

use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::mpsc;

use self::types::AgentEvent;

/// Controls what an agent is allowed to do while it runs.
///
/// In `Plan` mode the agent runs read-only: it may read files, browse, and
/// write *only* to the designated plan artifact. In `Execute` mode all
/// operations (file writes, shell, etc.) are approved.
#[derive(Debug, Clone)]
pub enum PermissionPolicy {
    /// Read-only except for writing the plan artifact at `plan_path`
    /// (repo-relative).
    Plan { plan_path: String },
    /// Everything is approved.
    Execute,
}

impl PermissionPolicy {
    pub fn is_plan(&self) -> bool {
        matches!(self, PermissionPolicy::Plan { .. })
    }
}

#[async_trait]
pub trait AgentRunner: Send + Sync {
    /// Take the receiver for agent events. Returns `Some` on the first call,
    /// `None` thereafter — the app event loop owns the receiver and selects on it.
    fn take_events(&self) -> Option<mpsc::Receiver<AgentEvent>>;
    async fn send(&self, comment_id: &str, prompt: &str) -> anyhow::Result<()>;
    async fn start(&self) -> anyhow::Result<()>;
    fn stop(&self);
    /// Update the permission policy governing what the agent may do. Takes
    /// effect for the next (and any in-flight) tool permission request.
    fn set_policy(&self, policy: PermissionPolicy);
}

/// Creates [`AgentRunner`] instances, one per agent worktree. The seam that
/// makes the backend pluggable (Copilot today; e.g. a Cursor backend later).
pub trait AgentFactory: Send + Sync {
    /// Create a runner that operates with the given working directory as its
    /// repo root (an agent's worktree path).
    fn create(&self, worktree: &str) -> Arc<dyn AgentRunner>;
}
