//! Keyboard handling for the agent workspace.
//!
//! Focus model: `AgentList | Stage | Chat`. `Shift+Tab` toggles the active
//! agent's plan/execute mode from anywhere. `Tab` cycles the stage view.
//! `Ctrl+R` toggles review mode (Diff/PR only), which hides the chat.

use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};

use super::{InputOverlay, OverlayKind, StageView, Workspace, WorkspaceFocus};
use crate::ui::scroll::ScrollState;

pub async fn handle_key(ws: &mut Workspace, key: KeyEvent) -> Option<String> {
    if ws.overlay.is_some() {
        handle_overlay(ws, key).await;
        return None;
    }

    // Shift+Tab toggles plan/execute mode regardless of focus.
    if key.code == KeyCode::BackTab {
        ws.toggle_mode();
        return None;
    }

    match ws.focus {
        WorkspaceFocus::AgentList => handle_list(ws, key).await,
        WorkspaceFocus::Stage => handle_stage(ws, key).await,
        WorkspaceFocus::Chat => handle_chat(ws, key).await,
    }
    None
}

async fn handle_overlay(ws: &mut Workspace, key: KeyEvent) {
    let Some(overlay) = ws.overlay.as_mut() else {
        return;
    };
    match key.code {
        KeyCode::Esc => {
            ws.overlay = None;
        }
        KeyCode::Enter => {
            let overlay = ws.overlay.take().unwrap();
            let value = overlay.value.trim().to_string();
            if value.is_empty() {
                return;
            }
            match overlay.kind {
                OverlayKind::NewAgent => match ws.create_agent(&value).await {
                    Ok(_) => {
                        ws.focus = WorkspaceFocus::Chat;
                        ws.flash.success(format!("Created agent {value}"));
                    }
                    Err(e) => ws.flash.error(format!("Failed to create agent: {e}")),
                },
                OverlayKind::ReviewComment => {
                    ws.submit_review(value).await;
                }
            }
        }
        KeyCode::Backspace => overlay.backspace(),
        KeyCode::Left => {
            overlay.cursor = overlay.cursor.saturating_sub(1);
        }
        KeyCode::Right => {
            let len = overlay.value.chars().count();
            overlay.cursor = (overlay.cursor + 1).min(len);
        }
        KeyCode::Char(c) if !key.modifiers.contains(KeyModifiers::CONTROL) => {
            overlay.insert(c);
        }
        _ => {}
    }
}

async fn handle_list(ws: &mut Workspace, key: KeyEvent) {
    match key.code {
        KeyCode::Char('j') | KeyCode::Down => {
            if ws.selected + 1 < ws.agents.len() {
                ws.selected += 1;
                ws.on_selection_changed();
            }
        }
        KeyCode::Char('k') | KeyCode::Up => {
            if ws.selected > 0 {
                ws.selected -= 1;
                ws.on_selection_changed();
            }
        }
        KeyCode::Char('g') => {
            ws.selected = 0;
            ws.on_selection_changed();
        }
        KeyCode::Char('G') => {
            ws.selected = ws.agents.len().saturating_sub(1);
            ws.on_selection_changed();
        }
        KeyCode::Enter | KeyCode::Char('l') | KeyCode::Right | KeyCode::Char('f') => {
            ws.focus = WorkspaceFocus::Stage;
        }
        KeyCode::Char('n') => {
            ws.overlay = Some(InputOverlay::new(OverlayKind::NewAgent, "New agent name"));
        }
        KeyCode::Char('d') => {
            ws.remove_agent(ws.selected).await;
        }
        _ => {}
    }
}

async fn handle_stage(ws: &mut Workspace, key: KeyEvent) {
    let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);

    // Review-mode specific actions.
    if ws.review_mode {
        match key.code {
            KeyCode::Char('c') => {
                let title = match ws.stage_view {
                    StageView::Pr => "Reply to reviewer",
                    _ => "Message to agent",
                };
                ws.overlay = Some(InputOverlay::new(OverlayKind::ReviewComment, title));
                return;
            }
            KeyCode::Char('r') if ctrl => {
                ws.toggle_review_mode();
                return;
            }
            KeyCode::Esc | KeyCode::Char('q') => {
                ws.review_mode = false;
                return;
            }
            _ => {}
        }
    }

    match key.code {
        KeyCode::Tab => ws.cycle_stage(),
        KeyCode::Char('1') => ws.set_stage(StageView::Plan),
        KeyCode::Char('2') => ws.set_stage(StageView::Diff),
        KeyCode::Char('3') => ws.set_stage(StageView::Pr),
        KeyCode::Char('r') if ctrl => ws.toggle_review_mode(),
        KeyCode::Char('h') | KeyCode::Left => ws.focus = WorkspaceFocus::AgentList,
        KeyCode::Char('l') | KeyCode::Right => {
            if !ws.review_mode {
                ws.focus = WorkspaceFocus::Chat;
            }
        }
        KeyCode::Char('f') => {
            ws.focus = if ws.review_mode {
                WorkspaceFocus::AgentList
            } else {
                WorkspaceFocus::Chat
            };
        }
        KeyCode::Char('j') | KeyCode::Down => scroll(ws, |s| s.scroll_down(1)),
        KeyCode::Char('k') | KeyCode::Up => scroll(ws, |s| s.scroll_up(1)),
        KeyCode::Char('d') if ctrl => scroll(ws, |s| s.half_page_down()),
        KeyCode::Char('u') if ctrl => scroll(ws, |s| s.half_page_up()),
        KeyCode::Char('g') => scroll(ws, |s| s.goto_top()),
        KeyCode::Char('G') => scroll(ws, |s| s.scroll_to_bottom()),
        _ => {}
    }
}

async fn handle_chat(ws: &mut Workspace, key: KeyEvent) {
    let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
    match key.code {
        KeyCode::Enter => ws.submit_chat().await,
        KeyCode::Esc => ws.focus = WorkspaceFocus::Stage,
        KeyCode::Tab => ws.cycle_stage(),
        KeyCode::Backspace => {
            if ws.chat_cursor > 0 {
                let chars: Vec<char> = ws.chat_input.chars().collect();
                ws.chat_cursor -= 1;
                ws.chat_input = chars
                    .iter()
                    .enumerate()
                    .filter(|(i, _)| *i != ws.chat_cursor)
                    .map(|(_, c)| *c)
                    .collect();
            }
        }
        KeyCode::Left => ws.chat_cursor = ws.chat_cursor.saturating_sub(1),
        KeyCode::Right => {
            let len = ws.chat_input.chars().count();
            ws.chat_cursor = (ws.chat_cursor + 1).min(len);
        }
        KeyCode::Char(c) if !ctrl => {
            let idx = char_to_byte(&ws.chat_input, ws.chat_cursor);
            ws.chat_input.insert(idx, c);
            ws.chat_cursor += 1;
        }
        _ => {}
    }
}

fn char_to_byte(s: &str, char_idx: usize) -> usize {
    s.char_indices()
        .nth(char_idx)
        .map(|(i, _)| i)
        .unwrap_or(s.len())
}

/// Apply a scroll operation to the active stage panel's scroll state.
fn scroll<F: FnOnce(&mut ScrollState)>(ws: &mut Workspace, f: F) {
    let view = ws.stage_view;
    if let Some(a) = ws.agents.get_mut(ws.selected) {
        let s = match view {
            StageView::Plan => &mut a.plan_scroll,
            StageView::Diff => &mut a.diff_scroll,
            StageView::Pr => &mut a.pr_scroll,
        };
        f(s);
    }
}
