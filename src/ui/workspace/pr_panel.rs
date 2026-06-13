//! PR panel: shows pull request metadata and review threads for the active
//! agent's branch. In review mode you can reply to a thread (human-facing).

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};

use super::widgets;
use super::Workspace;
use crate::ui::styles::DiffColors;

pub fn render(
    ws: &mut Workspace,
    frame: &mut Frame,
    area: Rect,
    focused: bool,
    colors: &DiffColors,
) {
    let width = area.width.saturating_sub(2) as usize;
    let Some(agent) = ws.agents.get_mut(ws.selected) else {
        widgets::render_placeholder(frame, area, "No agent");
        return;
    };

    let Some(pr) = agent.pr.clone() else {
        widgets::render_placeholder(frame, area, "No pull request for this branch");
        return;
    };

    let mut lines: Vec<Line<'static>> = Vec::new();
    lines.push(Line::from(vec![
        Span::styled(
            format!("PR #{}", pr.number),
            Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD),
        ),
        Span::styled(
            format!("  {} → {}", pr.head.ref_name, pr.base.ref_name),
            widgets::dim(),
        ),
    ]));
    if let Some(url) = &pr.html_url {
        lines.push(Line::from(Span::styled(url.clone(), widgets::dim())));
    }
    lines.push(Line::from(Span::raw("")));

    lines.push(Line::from(Span::styled(
        format!("Review comments ({})", agent.review_comments.len()),
        Style::default()
            .fg(Color::White)
            .add_modifier(Modifier::BOLD),
    )));
    lines.push(Line::from(Span::raw("")));

    if agent.review_comments.is_empty() {
        lines.push(Line::from(Span::styled(
            "No review comments yet.",
            widgets::dim(),
        )));
    } else {
        for c in &agent.review_comments {
            lines.push(Line::from(vec![
                Span::styled(
                    format!("@{}", c.user.login),
                    Style::default()
                        .fg(Color::White)
                        .add_modifier(Modifier::BOLD),
                ),
                Span::styled(
                    format!("  {}:{}", c.path, c.line.or(c.original_line).unwrap_or(0)),
                    widgets::dim(),
                ),
            ]));
            for wl in widgets::wrap_plain(&c.body, width) {
                lines.push(Line::from(Span::raw(wl)));
            }
            lines.push(Line::from(Span::raw("")));
        }
    }

    widgets::render_text_panel(
        frame,
        area,
        " Pull Request",
        focused,
        &lines,
        &mut agent.pr_scroll,
        colors,
    );
}
