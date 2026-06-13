//! Diff panel: shows the active agent's contribution (committed + uncommitted
//! changes versus the fork point) and populates live as the agent edits.

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

    if !agent.diff_loaded {
        widgets::render_placeholder(frame, area, "Loading diff…");
        return;
    }
    if agent.diff_files.is_empty() {
        widgets::render_placeholder(frame, area, "No changes yet");
        return;
    }

    let mut lines: Vec<Line<'static>> = Vec::new();
    for file in &agent.diff_files {
        let status_color = match file.status.as_str() {
            "added" => Color::Green,
            "removed" => Color::Red,
            "renamed" => Color::Blue,
            _ => Color::White,
        };
        lines.push(Line::from(vec![
            Span::styled(
                file.filename.clone(),
                Style::default()
                    .fg(status_color)
                    .add_modifier(Modifier::BOLD),
            ),
            Span::styled(
                format!("  +{} ", file.additions),
                Style::default().fg(colors.add_fg),
            ),
            Span::styled(
                format!("-{}", file.deletions),
                Style::default().fg(colors.del_fg),
            ),
        ]));

        for raw in file.patch.split('\n') {
            lines.push(render_diff_line(raw, width, colors));
        }
        lines.push(Line::from(Span::raw("")));
    }

    let total = agent.diff_files.len();
    let title = format!(" Diff · {total} file{}", if total == 1 { "" } else { "s" });
    widgets::render_text_panel(
        frame,
        area,
        &title,
        focused,
        &lines,
        &mut agent.diff_scroll,
        colors,
    );
}

fn render_diff_line(raw: &str, width: usize, colors: &DiffColors) -> Line<'static> {
    let clipped = truncate(raw, width);
    if raw.starts_with("@@") {
        Line::from(Span::styled(
            clipped,
            Style::default().fg(colors.hunk_fg).bg(colors.hunk_bg),
        ))
    } else if raw.starts_with('+') {
        Line::from(Span::styled(
            clipped,
            Style::default().fg(colors.add_fg).bg(colors.add_bg),
        ))
    } else if raw.starts_with('-') {
        Line::from(Span::styled(
            clipped,
            Style::default().fg(colors.del_fg).bg(colors.del_bg),
        ))
    } else {
        Line::from(Span::styled(
            clipped,
            Style::default().fg(colors.context_fg),
        ))
    }
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let taken: String = s.chars().take(max.saturating_sub(1)).collect();
    format!("{taken}…")
}
