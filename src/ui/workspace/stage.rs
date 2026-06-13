//! Center stage: a tab bar (Plan | Diff | PR) above the active panel. The PR
//! tab is dimmed until the active agent has an open pull request.

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;

use super::widgets;
use super::{StageView, Workspace, WorkspaceFocus};
use crate::ui::styles::DiffColors;

pub fn render(ws: &mut Workspace, frame: &mut Frame, area: Rect, colors: &DiffColors) {
    if area.width == 0 || area.height == 0 {
        return;
    }
    let focused = ws.focus == WorkspaceFocus::Stage;

    let has_pr = ws.selected_agent().map(|a| a.pr.is_some()).unwrap_or(false);
    render_tabs(frame, Rect::new(area.x, area.y, area.width, 1), ws.stage_view, has_pr, focused);

    if area.height <= 1 {
        return;
    }
    let content = Rect::new(area.x, area.y + 1, area.width, area.height - 1);

    match ws.stage_view {
        StageView::Plan => super::plan_panel::render(ws, frame, content, focused, colors),
        StageView::Diff => super::diff_panel::render(ws, frame, content, focused, colors),
        StageView::Pr => super::pr_panel::render(ws, frame, content, focused, colors),
    }
}

fn render_tabs(
    frame: &mut Frame,
    area: Rect,
    active: StageView,
    has_pr: bool,
    focused: bool,
) {
    let mut spans = vec![Span::raw(" ")];
    for view in [StageView::Plan, StageView::Diff, StageView::Pr] {
        let is_active = view == active;
        let disabled = matches!(view, StageView::Pr) && !has_pr;
        let style = if disabled {
            Style::default().fg(Color::DarkGray)
        } else if is_active {
            let base = Style::default()
                .fg(Color::Yellow)
                .add_modifier(Modifier::BOLD);
            if focused {
                base.add_modifier(Modifier::UNDERLINED)
            } else {
                base
            }
        } else {
            widgets::dim()
        };
        spans.push(Span::styled(format!(" {} ", view.label()), style));
        spans.push(Span::raw(" "));
    }
    frame.render_widget(Paragraph::new(Line::from(spans)), area);
}
