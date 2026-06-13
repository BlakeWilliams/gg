//! Top-level workspace layout: header, the three-column body
//! (`agent list | stage | chat`), and a dynamic footer. Mirrors the chrome
//! conventions used by the local diff view.

use ratatui::Frame;
use ratatui::layout::{Constraint, Direction, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;

use super::agent::AgentMode;
use super::widgets;
use super::{StageView, Workspace, WorkspaceFocus};
use crate::ui::styles::DiffColors;

const AGENT_LIST_WIDTH: u16 = 32;
const CHAT_WIDTH: u16 = 52;
const CHAT_MIN_TOTAL: u16 = 100;

pub fn render(ws: &mut Workspace, frame: &mut Frame, area: Rect, colors: &DiffColors) {
    let vert = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(1), // header
            Constraint::Length(1), // separator
            Constraint::Fill(1),   // body
            Constraint::Length(1), // footer separator
            Constraint::Length(1), // footer
        ])
        .split(area);

    render_header(ws, frame, vert[0], colors);
    render_rule(frame, vert[1], colors);
    render_body(ws, frame, vert[2], colors);
    render_rule(frame, vert[3], colors);
    render_footer(ws, frame, vert[4], colors);

    if ws.overlay.is_some() {
        render_overlay(ws, frame, area, colors);
    }
}

fn render_overlay(ws: &Workspace, frame: &mut Frame, area: Rect, colors: &DiffColors) {
    use ratatui::widgets::Clear;

    let Some(overlay) = &ws.overlay else {
        return;
    };

    let box_w = 56u16.min(area.width.saturating_sub(4)).max(20);
    let box_h = 3u16;
    let x = area.x + (area.width.saturating_sub(box_w)) / 2;
    let y = area.y + area.height / 3;
    let rect = Rect::new(x, y, box_w, box_h);
    frame.render_widget(Clear, rect);

    let bc = Style::default().fg(colors.border_fg);
    let inner_w = box_w.saturating_sub(2) as usize;

    let title = format!(" {} ", overlay.title);
    let title_w = title.chars().count();
    let fill = inner_w.saturating_sub(title_w);
    let top = Line::from(vec![
        Span::styled("╭", bc),
        Span::styled(title, Style::default().add_modifier(Modifier::BOLD)),
        Span::styled("─".repeat(fill), bc),
        Span::styled("╮", bc),
    ]);

    // Input row with block cursor.
    let chars: Vec<char> = overlay.value.chars().collect();
    let c = overlay.cursor.min(chars.len());
    let before: String = chars[..c].iter().collect();
    let mut content_spans = vec![Span::styled("│ ", bc), Span::raw(before)];
    if c < chars.len() {
        content_spans.push(Span::styled(
            chars[c].to_string(),
            Style::default().fg(Color::Black).bg(Color::White),
        ));
        let after: String = chars[c + 1..].iter().collect();
        content_spans.push(Span::raw(after));
    } else {
        content_spans.push(Span::styled("▍", Style::default().fg(Color::White)));
    }
    content_spans.push(Span::styled(" │", bc));
    let mid = Line::from(content_spans);

    let bottom = Line::from(vec![
        Span::styled("╰", bc),
        Span::styled("─".repeat(inner_w), bc),
        Span::styled("╯", bc),
    ]);

    frame.render_widget(Paragraph::new(vec![top, mid, bottom]), rect);
}

fn render_rule(frame: &mut Frame, area: Rect, colors: &DiffColors) {
    let line = "─".repeat(area.width as usize);
    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(
            line,
            Style::default().fg(colors.chrome_fg),
        ))),
        area,
    );
}

fn render_header(ws: &Workspace, frame: &mut Frame, area: Rect, _colors: &DiffColors) {
    let mut spans = vec![Span::styled(
        " gg ",
        Style::default()
            .fg(Color::Magenta)
            .add_modifier(Modifier::BOLD),
    )];
    spans.push(Span::styled("workspace", widgets::dim()));

    if let Some(agent) = ws.selected_agent() {
        spans.push(Span::styled("  ·  ", widgets::dim()));
        spans.push(Span::styled(agent.name.clone(), widgets::bright()));
        spans.push(Span::styled(
            format!("  ({})", agent.branch),
            widgets::dim(),
        ));
        let (label, color) = match agent.mode {
            AgentMode::Plan => ("PLAN", Color::Blue),
            AgentMode::Execute => ("EXEC", Color::Magenta),
        };
        spans.push(Span::raw("  "));
        spans.push(Span::styled(
            label,
            Style::default().fg(color).add_modifier(Modifier::BOLD),
        ));
        if ws.review_mode {
            spans.push(Span::raw("  "));
            spans.push(Span::styled(
                "REVIEW",
                Style::default()
                    .fg(Color::Yellow)
                    .add_modifier(Modifier::BOLD),
            ));
        }
    }

    frame.render_widget(Paragraph::new(Line::from(spans)), area);
}

fn render_body(ws: &mut Workspace, frame: &mut Frame, area: Rect, colors: &DiffColors) {
    let show_chat = !ws.review_mode && area.width >= CHAT_MIN_TOTAL;
    let list_w = AGENT_LIST_WIDTH.min(area.width / 3);
    let chat_w = if show_chat { CHAT_WIDTH.min(area.width / 3) } else { 0 };

    let cols = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Length(list_w),
            Constraint::Length(1), // divider
            Constraint::Fill(1),
            Constraint::Length(if show_chat { 1 } else { 0 }), // divider
            Constraint::Length(chat_w),
        ])
        .split(area);

    let list_focus = ws.focus == WorkspaceFocus::AgentList;
    super::agent_list::render(ws, frame, cols[0], list_focus, colors);

    widgets::render_vline(frame, cols[1].x, cols[1].y, cols[1].height, colors.chrome_fg);

    super::stage::render(ws, frame, cols[2], colors);

    if show_chat {
        widgets::render_vline(frame, cols[3].x, cols[3].y, cols[3].height, colors.chrome_fg);
        let chat_focus = ws.focus == WorkspaceFocus::Chat;
        super::chat::render(ws, frame, cols[4], chat_focus, colors);
    }
}

fn render_footer(ws: &Workspace, frame: &mut Frame, area: Rect, _colors: &DiffColors) {
    let hints: Vec<(&str, &str)> = match ws.focus {
        WorkspaceFocus::AgentList => vec![
            ("j/k", "navigate"),
            ("enter", "select"),
            ("n", "new"),
            ("d", "remove"),
            ("l", "stage"),
        ],
        WorkspaceFocus::Stage => {
            let mut h = vec![("tab", "switch view"), ("⇧tab", "plan/exec")];
            if matches!(ws.stage_view, StageView::Diff | StageView::Pr) {
                h.push(("ctrl+r", "review"));
            }
            h.push(("h", "list"));
            h.push(("l", "chat"));
            h
        }
        WorkspaceFocus::Chat => vec![
            ("enter", "send"),
            ("⇧tab", "plan/exec"),
            ("tab", "switch view"),
            ("h", "stage"),
        ],
    };

    let mut spans = vec![Span::raw(" ")];
    for (i, (key, desc)) in hints.iter().enumerate() {
        if i > 0 {
            spans.push(Span::styled("  ", widgets::dim()));
        }
        spans.push(Span::styled(
            (*key).to_string(),
            Style::default()
                .fg(Color::Magenta)
                .add_modifier(Modifier::BOLD),
        ));
        spans.push(Span::raw(" "));
        spans.push(Span::styled((*desc).to_string(), widgets::dim()));
    }

    frame.render_widget(Paragraph::new(Line::from(spans)), area);
}
