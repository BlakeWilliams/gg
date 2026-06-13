//! Per-agent state for the agent workspace. Each agent maps 1:1 to a git
//! branch (and optional PR) and owns its own conversation, plan, and diff.

use std::sync::Arc;

use serde::{Deserialize, Serialize};

use crate::agent::AgentRunner;
use crate::github::types::{PullRequest, PullRequestFile, ReviewComment};
use crate::ui::scroll::ScrollState;

/// Whether the agent is currently planning (read-only) or executing (writes).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum AgentMode {
    Plan,
    Execute,
}

impl AgentMode {
    pub fn toggled(self) -> Self {
        match self {
            AgentMode::Plan => AgentMode::Execute,
            AgentMode::Execute => AgentMode::Plan,
        }
    }
}

/// High-level lifecycle status, drives the list spinner/labels.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AgentStatus {
    Idle,
    Planning,
    Executing,
    Error,
}

/// Author of a chat message.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ChatAuthor {
    You,
    Agent,
}

/// Status of a tool invocation shown inline in the chat.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum ToolStatus {
    Running,
    Done,
    Failed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolLine {
    pub name: String,
    pub summary: String,
    pub status: ToolStatus,
}

/// A renderable chunk of an agent message: free text or a tool activity row.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ChatBlock {
    Text(String),
    Tool(ToolLine),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessage {
    pub author: ChatAuthor,
    pub blocks: Vec<ChatBlock>,
}

impl ChatMessage {
    pub fn user(text: String) -> Self {
        Self {
            author: ChatAuthor::You,
            blocks: vec![ChatBlock::Text(text)],
        }
    }

    pub fn agent_empty() -> Self {
        Self {
            author: ChatAuthor::Agent,
            blocks: Vec::new(),
        }
    }

    /// Append streamed text to the trailing text block, creating one if the
    /// last block is a tool row.
    pub fn append_text(&mut self, text: &str) {
        match self.blocks.last_mut() {
            Some(ChatBlock::Text(s)) => s.push_str(text),
            _ => self.blocks.push(ChatBlock::Text(text.to_string())),
        }
    }

    pub fn is_empty(&self) -> bool {
        self.blocks.iter().all(|b| match b {
            ChatBlock::Text(s) => s.trim().is_empty(),
            ChatBlock::Tool(_) => false,
        })
    }
}

/// The plan artifact the agent produces in plan mode.
#[derive(Debug, Clone, Default)]
pub struct PlanState {
    pub markdown: String,
    pub generating: bool,
}

/// Live, non-persisted runtime state for an agent.
pub struct Agent {
    pub id: String,
    pub name: String,
    pub branch: String,
    pub worktree_path: String,
    pub mode: AgentMode,
    pub status: AgentStatus,

    pub conversation: Vec<ChatMessage>,
    /// Index into `conversation` of the in-flight agent reply, if streaming.
    pub streaming_idx: Option<usize>,

    pub plan: PlanState,
    pub diff_files: Vec<PullRequestFile>,
    pub diff_loaded: bool,

    pub pr: Option<PullRequest>,
    pub pr_loaded: bool,
    pub review_comments: Vec<ReviewComment>,

    pub runner: Arc<dyn AgentRunner>,

    // Per-agent scroll positions so switching agents preserves view.
    pub chat_scroll: ScrollState,
    pub plan_scroll: ScrollState,
    pub diff_scroll: ScrollState,
    pub pr_scroll: ScrollState,
}

impl Agent {
    pub fn is_streaming(&self) -> bool {
        self.streaming_idx.is_some()
    }

    /// A short, human-readable status line for the agent list.
    pub fn status_label(&self) -> &'static str {
        match self.status {
            AgentStatus::Idle => "idle",
            AgentStatus::Planning => "planning",
            AgentStatus::Executing => "executing",
            AgentStatus::Error => "error",
        }
    }
}
