//! The agent workspace: a top-level view where each agent maps 1:1 to a git
//! branch (and optional PR). Layout is `agent list | stage | chat`, where the
//! stage cycles between Plan, Diff and PR panels. `Shift+Tab` toggles an
//! agent between plan (read-only) and execution (writes) mode; a review-mode
//! toggle hides the chat to focus on reviewing the diff/PR.

pub mod agent;
pub mod agent_list;
pub mod chat;
pub mod diff_panel;
pub mod keybinds;
pub mod layout;
pub mod plan_panel;
pub mod pr_panel;
pub mod registry;
pub mod stage;
pub mod widgets;

use std::sync::Arc;

use ratatui::Frame;
use ratatui::layout::Rect;
use tokio::sync::mpsc;

use crate::agent::{AgentFactory, AgentRunner, PermissionPolicy};
use crate::agent::types::{AgentEvent, EventKind, EventPayload};
use crate::config::{AgentIsolation, Config};
use crate::github::CachedClient;
use crate::github::types::{PullRequest, PullRequestFile, ReviewComment};
use crate::ui::flash::FlashState;
use crate::ui::styles::DiffColors;

use self::agent::{
    Agent, AgentMode, AgentStatus, ChatBlock, ChatMessage, PlanState, ToolLine, ToolStatus,
};
use self::registry::{AgentRecord, Registry};

/// Which pane currently has keyboard focus.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WorkspaceFocus {
    AgentList,
    Stage,
    Chat,
}

/// Which panel the center stage is showing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StageView {
    Plan,
    Diff,
    Pr,
}

impl StageView {
    pub fn label(self) -> &'static str {
        match self {
            StageView::Plan => "Plan",
            StageView::Diff => "Diff",
            StageView::Pr => "PR",
        }
    }
}

/// What a text input overlay is collecting.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum OverlayKind {
    NewAgent,
    ReviewComment,
}

/// A small centered text input overlay (new agent name / review comment).
#[derive(Debug, Clone)]
pub struct InputOverlay {
    pub kind: OverlayKind,
    pub title: String,
    pub value: String,
    pub cursor: usize,
}

impl InputOverlay {
    pub fn new(kind: OverlayKind, title: impl Into<String>) -> Self {
        Self {
            kind,
            title: title.into(),
            value: String::new(),
            cursor: 0,
        }
    }

    pub fn insert(&mut self, c: char) {
        let idx = byte_index(&self.value, self.cursor);
        self.value.insert(idx, c);
        self.cursor += 1;
    }

    pub fn backspace(&mut self) {
        if self.cursor == 0 {
            return;
        }
        let chars: Vec<char> = self.value.chars().collect();
        self.cursor -= 1;
        let new: String = chars
            .iter()
            .enumerate()
            .filter(|(i, _)| *i != self.cursor)
            .map(|(_, c)| *c)
            .collect();
        self.value = new;
    }
}

fn byte_index(s: &str, char_idx: usize) -> usize {
    s.char_indices()
        .nth(char_idx)
        .map(|(i, _)| i)
        .unwrap_or(s.len())
}

/// Result of loading a diff for a particular agent, delivered async.
pub struct DiffLoaded {
    pub agent_id: String,
    pub files: Vec<PullRequestFile>,
}

/// Result of fetching PR metadata + review comments for an agent, delivered async.
pub struct PrLoaded {
    pub agent_id: String,
    pub pr: Option<PullRequest>,
    pub comments: Vec<ReviewComment>,
}

pub struct Workspace {
    pub repo_root: String,
    pub owner: String,
    pub repo: String,
    pub default_branch: String,
    pub github: Arc<CachedClient>,
    pub factory: Arc<dyn AgentFactory>,
    pub isolation: AgentIsolation,
    pub worktree_root: std::path::PathBuf,
    pub plan_path: String,
    pub username: Option<String>,

    pub agents: Vec<Agent>,
    pub selected: usize,
    pub focus: WorkspaceFocus,
    pub stage_view: StageView,
    pub review_mode: bool,

    /// Chat composer for the active agent.
    pub chat_input: String,
    pub chat_cursor: usize,

    /// Spinner animation frame.
    pub anim_frame: usize,

    pub flash: FlashState,

    /// Active text input overlay (new agent name / review comment), if any.
    pub overlay: Option<InputOverlay>,

    pub size: Rect,

    // Async plumbing — owned here, polled by the app event loop.
    pub agent_event_tx: mpsc::UnboundedSender<(String, AgentEvent)>,
    pub agent_event_rx: mpsc::UnboundedReceiver<(String, AgentEvent)>,
    pub diff_tx: mpsc::UnboundedSender<DiffLoaded>,
    pub diff_rx: mpsc::UnboundedReceiver<DiffLoaded>,
    pub pr_tx: mpsc::UnboundedSender<PrLoaded>,
    pub pr_rx: mpsc::UnboundedReceiver<PrLoaded>,

    /// Worktree path the app should (re)point its file watcher at.
    pub pending_watch: Option<String>,

    pub colors: DiffColors,
}

impl Workspace {
    pub async fn new(
        repo_root: String,
        owner: String,
        repo: String,
        current_branch: String,
        config: &Config,
        github: Arc<CachedClient>,
        factory: Arc<dyn AgentFactory>,
    ) -> Self {
        let default_branch = crate::git::default_branch_short(&repo_root)
            .await
            .unwrap_or_else(|_| "main".to_string());

        let worktree_root = config
            .worktree_root
            .clone()
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| crate::git::worktree::default_worktree_root(&repo_root));

        let (agent_event_tx, agent_event_rx) = mpsc::unbounded_channel();
        let (diff_tx, diff_rx) = mpsc::unbounded_channel();
        let (pr_tx, pr_rx) = mpsc::unbounded_channel();

        let mut ws = Self {
            repo_root: repo_root.clone(),
            owner,
            repo,
            default_branch,
            github,
            factory,
            isolation: config.agent_isolation,
            worktree_root,
            plan_path: config.plan_path.clone(),
            username: None,
            agents: Vec::new(),
            selected: 0,
            focus: WorkspaceFocus::Chat,
            stage_view: StageView::Plan,
            review_mode: false,
            chat_input: String::new(),
            chat_cursor: 0,
            anim_frame: 0,
            flash: FlashState::new(),
            overlay: None,
            size: Rect::default(),
            agent_event_tx,
            agent_event_rx,
            diff_tx,
            diff_rx,
            pr_tx,
            pr_rx,
            pending_watch: None,
            colors: DiffColors::default(),
        };

        // Hydrate from the persisted registry; seed with the current branch if
        // empty so there is always at least one agent to work with.
        let registry = Registry::load(&ws.owner, &ws.repo);
        if registry.agents.is_empty() {
            ws.hydrate_agent(AgentRecord {
                id: new_id(),
                name: current_branch.clone(),
                branch: current_branch.clone(),
                worktree_path: repo_root.clone(),
                mode: AgentMode::Plan,
                conversation: Vec::new(),
            });
        } else {
            for record in registry.agents {
                ws.hydrate_agent(record);
            }
            ws.selected = registry.selected.min(ws.agents.len().saturating_sub(1));
        }

        if let Some(a) = ws.agents.get(ws.selected) {
            ws.pending_watch = Some(a.worktree_path.clone());
        }

        ws
    }

    /// Build a live `Agent` from a persisted record: create + start its runner,
    /// wire the event forwarder, and load its diff.
    fn hydrate_agent(&mut self, record: AgentRecord) {
        let runner = self.factory.create(&record.worktree_path);
        self.spawn_runtime(&record.id, &runner, record.mode);

        let agent = Agent {
            id: record.id,
            name: record.name,
            branch: record.branch,
            worktree_path: record.worktree_path,
            mode: record.mode,
            status: AgentStatus::Idle,
            conversation: record.conversation,
            streaming_idx: None,
            plan: PlanState::default(),
            diff_files: Vec::new(),
            diff_loaded: false,
            pr: None,
            pr_loaded: false,
            review_comments: Vec::new(),
            runner,
            chat_scroll: Default::default(),
            plan_scroll: Default::default(),
            diff_scroll: Default::default(),
            pr_scroll: Default::default(),
        };
        let id = agent.id.clone();
        let worktree = agent.worktree_path.clone();
        self.agents.push(agent);
        self.load_plan(&id);
        self.load_diff(&id, &worktree);
    }

    /// Start the runner, apply its policy, and forward its events into the
    /// merged channel tagged with the agent id.
    fn spawn_runtime(&self, id: &str, runner: &Arc<dyn AgentRunner>, mode: AgentMode) {
        runner.set_policy(self.policy_for(mode));
        let start_runner = runner.clone();
        tokio::spawn(async move {
            if let Err(e) = start_runner.start().await {
                tracing::warn!("failed to start agent runner: {e}");
            }
        });

        if let Some(mut rx) = runner.take_events() {
            let tx = self.agent_event_tx.clone();
            let id = id.to_string();
            tokio::spawn(async move {
                while let Some(ev) = rx.recv().await {
                    if tx.send((id.clone(), ev)).is_err() {
                        break;
                    }
                }
            });
        }
    }

    fn policy_for(&self, mode: AgentMode) -> PermissionPolicy {
        match mode {
            AgentMode::Plan => PermissionPolicy::Plan {
                plan_path: self.plan_path.clone(),
            },
            AgentMode::Execute => PermissionPolicy::Execute,
        }
    }

    pub fn selected_agent(&self) -> Option<&Agent> {
        self.agents.get(self.selected)
    }

    fn agent_index(&self, id: &str) -> Option<usize> {
        self.agents.iter().position(|a| a.id == id)
    }

    /// Persist the registry to disk.
    pub fn persist(&self) {
        let registry = Registry {
            agents: self
                .agents
                .iter()
                .map(|a| AgentRecord {
                    id: a.id.clone(),
                    name: a.name.clone(),
                    branch: a.branch.clone(),
                    worktree_path: a.worktree_path.clone(),
                    mode: a.mode,
                    conversation: a.conversation.clone(),
                })
                .collect(),
            selected: self.selected,
        };
        registry.save(&self.owner, &self.repo);
    }

    /// Create a brand new agent: a branch + worktree (or branch switch) and a
    /// fresh runner/conversation. Returns the new agent's id on success.
    pub async fn create_agent(&mut self, name: &str) -> anyhow::Result<String> {
        let branch = sanitize_branch(name);
        let base = self.default_branch.clone();

        let worktree_path = match self.isolation {
            AgentIsolation::Worktree => {
                crate::git::worktree::add_worktree(
                    &self.repo_root,
                    &self.worktree_root,
                    &branch,
                    &base,
                )
                .await?
            }
            AgentIsolation::Checkout => {
                crate::git::worktree::switch_branch(&self.repo_root, &branch, &base).await?;
                self.repo_root.clone()
            }
        };

        let id = new_id();
        let runner = self.factory.create(&worktree_path);
        self.spawn_runtime(&id, &runner, AgentMode::Plan);

        let agent = Agent {
            id: id.clone(),
            name: name.to_string(),
            branch,
            worktree_path: worktree_path.clone(),
            mode: AgentMode::Plan,
            status: AgentStatus::Idle,
            conversation: Vec::new(),
            streaming_idx: None,
            plan: PlanState::default(),
            diff_files: Vec::new(),
            diff_loaded: false,
            pr: None,
            pr_loaded: false,
            review_comments: Vec::new(),
            runner,
            chat_scroll: Default::default(),
            plan_scroll: Default::default(),
            diff_scroll: Default::default(),
            pr_scroll: Default::default(),
        };
        self.agents.push(agent);
        self.selected = self.agents.len() - 1;
        self.stage_view = StageView::Plan;
        self.pending_watch = Some(worktree_path.clone());
        self.load_diff(&id, &worktree_path);
        self.fetch_pr(&id);
        self.persist();
        Ok(id)
    }

    /// Remove an agent and (in worktree mode) tear down its worktree.
    pub async fn remove_agent(&mut self, idx: usize) {
        if idx >= self.agents.len() || self.agents.len() <= 1 {
            self.flash.error("Cannot remove the last agent".to_string());
            return;
        }
        let agent = self.agents.remove(idx);
        agent.runner.stop();
        if self.isolation == AgentIsolation::Worktree && agent.worktree_path != self.repo_root {
            let repo_root = self.repo_root.clone();
            let path = agent.worktree_path.clone();
            tokio::spawn(async move {
                if let Err(e) = crate::git::worktree::remove_worktree(&repo_root, &path).await {
                    tracing::warn!("failed to remove worktree {path}: {e}");
                }
            });
        }
        if self.selected >= self.agents.len() {
            self.selected = self.agents.len() - 1;
        }
        self.on_selection_changed();
        self.persist();
    }

    /// Called whenever the selected agent changes — reset composer + repoint
    /// the file watcher and lazily refresh the new agent's data.
    pub fn on_selection_changed(&mut self) {
        self.chat_input.clear();
        self.chat_cursor = 0;
        if let Some(a) = self.agents.get(self.selected) {
            let id = a.id.clone();
            let worktree = a.worktree_path.clone();
            let pr_loaded = a.pr_loaded;
            self.pending_watch = Some(worktree.clone());
            self.load_plan(&id);
            self.load_diff(&id, &worktree);
            if !pr_loaded {
                self.fetch_pr(&id);
            }
            // If the active agent has no PR, fall back off the PR tab.
            if self.stage_view == StageView::Pr && self.agents[self.selected].pr.is_none() {
                self.stage_view = StageView::Diff;
            }
        }
    }

    /// Send the composer contents to the active agent.
    pub async fn submit_chat(&mut self) {
        let text = self.chat_input.trim().to_string();
        if text.is_empty() {
            return;
        }
        self.chat_input.clear();
        self.chat_cursor = 0;
        self.send_message(text).await;
    }

    /// Send a message to the active agent, starting a streaming reply.
    pub async fn send_message(&mut self, text: String) {
        let (id, runner, mode) = match self.agents.get_mut(self.selected) {
            Some(a) => {
                a.conversation.push(ChatMessage::user(text.clone()));
                a.conversation.push(ChatMessage::agent_empty());
                a.streaming_idx = Some(a.conversation.len() - 1);
                a.status = match a.mode {
                    AgentMode::Plan => AgentStatus::Planning,
                    AgentMode::Execute => AgentStatus::Executing,
                };
                if a.mode == AgentMode::Plan {
                    a.plan.generating = true;
                    self.stage_view = StageView::Plan;
                } else {
                    self.stage_view = StageView::Diff;
                }
                (a.id.clone(), a.runner.clone(), a.mode)
            }
            None => return,
        };

        let prompt = build_prompt(mode, &self.plan_path, &text);
        if let Err(e) = runner.send(&id, &prompt).await {
            self.flash.error(format!("Agent error: {e}"));
            if let Some(a) = self.agents.get_mut(self.selected) {
                a.streaming_idx = None;
                a.status = AgentStatus::Error;
            }
        }
        self.persist();
    }

    /// Submit a review interaction from review mode: on the Diff this messages
    /// the agent; on the PR it posts a reply to the latest review thread.
    pub async fn submit_review(&mut self, text: String) {
        if text.trim().is_empty() {
            return;
        }
        match self.stage_view {
            StageView::Diff | StageView::Plan => {
                self.send_message(text).await;
            }
            StageView::Pr => {
                let (pr_number, reply_to) = match self.selected_agent() {
                    Some(a) => match &a.pr {
                        Some(pr) => (
                            pr.number,
                            a.review_comments
                                .iter()
                                .rfind(|c| c.in_reply_to_id.is_none())
                                .map(|c| c.id),
                        ),
                        None => {
                            self.flash.error("No PR to comment on".to_string());
                            return;
                        }
                    },
                    None => return,
                };
                let Some(reply_to) = reply_to else {
                    self.flash
                        .error("No review thread to reply to yet".to_string());
                    return;
                };
                let github = self.github.clone();
                let owner = self.owner.clone();
                let repo = self.repo.clone();
                match github
                    .reply_to_comment(&owner, &repo, pr_number, reply_to, &text)
                    .await
                {
                    Ok(_) => {
                        self.flash.success("Reply posted".to_string());
                        if let Some(a) = self.selected_agent() {
                            self.fetch_pr(&a.id.clone());
                        }
                    }
                    Err(e) => self.flash.error(format!("Failed to post reply: {e}")),
                }
            }
        }
    }

    /// Toggle plan/execute mode for the active agent.
    pub fn toggle_mode(&mut self) {
        let new_mode = match self.agents.get(self.selected) {
            Some(a) => a.mode.toggled(),
            None => return,
        };
        let policy = self.policy_for(new_mode);
        if let Some(a) = self.agents.get_mut(self.selected) {
            a.mode = new_mode;
            a.runner.set_policy(policy);
        }
        let label = if new_mode == AgentMode::Plan {
            "plan"
        } else {
            "execution"
        };
        self.flash.success(format!("Switched to {label} mode"));
        self.persist();
    }

    /// Cycle the stage view, skipping PR when unavailable.
    pub fn cycle_stage(&mut self) {
        let has_pr = self.selected_agent().map(|a| a.pr.is_some()).unwrap_or(false);
        self.stage_view = match self.stage_view {
            StageView::Plan => StageView::Diff,
            StageView::Diff => {
                if has_pr {
                    StageView::Pr
                } else {
                    StageView::Plan
                }
            }
            StageView::Pr => StageView::Plan,
        };
    }

    pub fn set_stage(&mut self, view: StageView) {
        if view == StageView::Pr && !self.selected_agent().map(|a| a.pr.is_some()).unwrap_or(false)
        {
            self.flash.error("No PR for this agent yet".to_string());
            return;
        }
        self.stage_view = view;
    }

    pub fn toggle_review_mode(&mut self) {
        if matches!(self.stage_view, StageView::Diff | StageView::Pr) {
            self.review_mode = !self.review_mode;
            if self.review_mode {
                self.focus = WorkspaceFocus::Stage;
            }
        } else {
            self.flash
                .error("Review mode is only available on Diff or PR".to_string());
        }
    }

    fn load_plan(&mut self, id: &str) {
        let Some(idx) = self.agent_index(id) else {
            return;
        };
        let full = std::path::Path::new(&self.agents[idx].worktree_path).join(&self.plan_path);
        if let Ok(contents) = std::fs::read_to_string(&full) {
            self.agents[idx].plan.markdown = contents;
        }
    }

    fn load_diff(&self, id: &str, worktree: &str) {
        let tx = self.diff_tx.clone();
        let id = id.to_string();
        let worktree = worktree.to_string();
        let base = self.default_branch.clone();
        tokio::spawn(async move {
            let raw = crate::git::diff::agent_diff(&worktree, &base)
                .await
                .unwrap_or_default();
            let files = crate::git::diff::parse_diff_to_files(&raw);
            let _ = tx.send(DiffLoaded { agent_id: id, files });
        });
    }

    /// Reload the diff (and plan) for the active agent — used on watcher events.
    pub fn reload_active_diff(&mut self) {
        if let Some(a) = self.agents.get(self.selected) {
            let id = a.id.clone();
            let worktree = a.worktree_path.clone();
            self.load_plan(&id);
            self.load_diff(&id, &worktree);
        }
    }

    fn fetch_pr(&self, id: &str) {
        let Some(idx) = self.agent_index(id) else {
            return;
        };
        let branch = self.agents[idx].branch.clone();
        let github = self.github.clone();
        let owner = self.owner.clone();
        let repo = self.repo.clone();
        let tx = self.pr_tx.clone();
        let id = id.to_string();
        tokio::spawn(async move {
            let pr = github
                .pull_request_by_branch(&owner, &repo, &branch)
                .await
                .ok()
                .flatten();
            let comments = if let Some(ref pr) = pr {
                github
                    .review_comments(&owner, &repo, pr.number)
                    .await
                    .unwrap_or_default()
            } else {
                Vec::new()
            };
            let _ = tx.send(PrLoaded {
                agent_id: id,
                pr,
                comments,
            });
        });
    }

    // ---- async result handlers (called by the app event loop) ----

    pub fn apply_diff_loaded(&mut self, loaded: DiffLoaded) {
        if let Some(idx) = self.agent_index(&loaded.agent_id) {
            self.agents[idx].diff_files = loaded.files;
            self.agents[idx].diff_loaded = true;
        }
    }

    pub fn apply_pr_loaded(&mut self, loaded: PrLoaded) {
        if let Some(idx) = self.agent_index(&loaded.agent_id) {
            self.agents[idx].pr = loaded.pr;
            self.agents[idx].pr_loaded = true;
            self.agents[idx].review_comments = loaded.comments;
        }
    }

    pub fn handle_agent_event(&mut self, id: String, event: AgentEvent) {
        let Some(idx) = self.agent_index(&id) else {
            return;
        };
        let agent = &mut self.agents[idx];
        let Some(msg_idx) = agent.streaming_idx else {
            return;
        };

        match event.kind {
            EventKind::Delta | EventKind::Message => {
                if let EventPayload::Delta(d) = event.payload {
                    if event.kind == EventKind::Message {
                        // Full message replaces the trailing text block.
                        if let Some(ChatBlock::Text(s)) = agent.conversation[msg_idx].blocks.last_mut()
                        {
                            *s = d.text;
                        } else {
                            agent.conversation[msg_idx].append_text(&d.text);
                        }
                    } else {
                        agent.conversation[msg_idx].append_text(&d.text);
                    }
                }
            }
            EventKind::ToolStart => {
                if let EventPayload::Tool(t) = event.payload {
                    agent.conversation[msg_idx].blocks.push(ChatBlock::Tool(ToolLine {
                        name: t.tool_name,
                        summary: t.args_summary,
                        status: ToolStatus::Running,
                    }));
                    // First tool write in execute mode flips the stage to Diff.
                    if agent.mode == AgentMode::Execute {
                        self.stage_view = StageView::Diff;
                    }
                }
            }
            EventKind::ToolComplete => {
                // Mark the most recent running tool as done.
                for block in agent.conversation[msg_idx].blocks.iter_mut().rev() {
                    if let ChatBlock::Tool(t) = block {
                        if t.status == ToolStatus::Running {
                            t.status = ToolStatus::Done;
                            break;
                        }
                    }
                }
            }
            EventKind::Done => {
                agent.streaming_idx = None;
                agent.status = AgentStatus::Idle;
                agent.plan.generating = false;
                // Drop an empty trailing agent message.
                if agent.conversation[msg_idx].is_empty() {
                    agent.conversation.remove(msg_idx);
                }
                let agent_id = agent.id.clone();
                let worktree = agent.worktree_path.clone();
                self.load_plan(&agent_id);
                self.load_diff(&agent_id, &worktree);
                if self.agents[idx].pr.is_none() {
                    self.fetch_pr(&agent_id);
                }
                self.persist();
            }
            EventKind::Error => {
                if let EventPayload::Error(e) = event.payload {
                    agent.conversation[msg_idx].append_text(&format!("\n\n⚠ {}", e.message));
                }
                agent.streaming_idx = None;
                agent.status = AgentStatus::Error;
                agent.plan.generating = false;
            }
        }
    }

    pub fn needs_animation(&self) -> bool {
        !self.flash.is_empty() || self.agents.iter().any(|a| a.is_streaming())
    }

    pub fn tick(&mut self) {
        self.anim_frame = self.anim_frame.wrapping_add(1);
        self.flash.gc();
    }

    pub fn take_pending_watch(&mut self) -> Option<String> {
        self.pending_watch.take()
    }

    pub fn resize(&mut self, w: u16, h: u16) {
        self.size = Rect::new(0, 0, w, h);
    }

    pub fn render(&mut self, frame: &mut Frame, area: Rect, colors: &DiffColors) {
        self.colors = colors.clone();
        self.size = area;
        layout::render(self, frame, area, colors);
        self.flash.render(frame, area);
    }
}

/// Generate a short unique id.
fn new_id() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);
    format!("agent-{nanos:x}")
}

/// Turn a human agent name into a git branch name under the `bmw/` namespace.
fn sanitize_branch(name: &str) -> String {
    let slug: String = name
        .trim()
        .to_lowercase()
        .chars()
        .map(|c| if c.is_alphanumeric() { c } else { '-' })
        .collect();
    let slug = slug.trim_matches('-').to_string();
    let slug = if slug.is_empty() {
        "agent".to_string()
    } else {
        slug
    };
    format!("agent/{slug}")
}

fn build_prompt(mode: AgentMode, plan_path: &str, user_msg: &str) -> String {
    match mode {
        AgentMode::Plan => format!(
            "You are operating in PLAN MODE. Do not modify any files in the repository \
             except for writing your implementation plan as Markdown to the file `{plan_path}` \
             (create it if needed). First research the codebase read-only, then write or update \
             `{plan_path}` with a clear, actionable plan. Do not run shell commands that mutate \
             state.\n\nUser request:\n{user_msg}"
        ),
        AgentMode::Execute => format!(
            "You are operating in EXECUTION MODE. Implement the requested changes by editing \
             files in this repository. Follow the existing plan if one is present at \
             `{plan_path}`.\n\nUser request:\n{user_msg}"
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_branch_slugifies_under_namespace() {
        assert_eq!(sanitize_branch("Fix the bug!"), "agent/fix-the-bug");
        assert_eq!(sanitize_branch("  Spaced  Name  "), "agent/spaced--name");
        assert_eq!(sanitize_branch("---"), "agent/agent");
    }

    #[test]
    fn plan_prompt_references_plan_path_and_is_readonly() {
        let p = build_prompt(AgentMode::Plan, ".gg/plan.md", "do a thing");
        assert!(p.contains("PLAN MODE"));
        assert!(p.contains(".gg/plan.md"));
        assert!(p.contains("do a thing"));
    }

    #[test]
    fn execute_prompt_allows_edits() {
        let p = build_prompt(AgentMode::Execute, ".gg/plan.md", "ship it");
        assert!(p.contains("EXECUTION MODE"));
        assert!(p.contains("ship it"));
    }

    #[test]
    fn input_overlay_edits_text() {
        let mut o = InputOverlay::new(OverlayKind::NewAgent, "t");
        for c in "abc".chars() {
            o.insert(c);
        }
        assert_eq!(o.value, "abc");
        assert_eq!(o.cursor, 3);
        o.backspace();
        assert_eq!(o.value, "ab");
        assert_eq!(o.cursor, 2);
        o.cursor = 1;
        o.insert('Z');
        assert_eq!(o.value, "aZb");
    }

    #[test]
    fn stage_view_labels() {
        assert_eq!(StageView::Plan.label(), "Plan");
        assert_eq!(StageView::Diff.label(), "Diff");
        assert_eq!(StageView::Pr.label(), "PR");
    }
}
