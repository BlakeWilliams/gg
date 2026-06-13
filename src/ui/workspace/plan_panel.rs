//! Plan panel: renders the agent's plan artifact (Markdown) with light
//! styling. Opens automatically while a plan is being generated.

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};

use super::widgets;
use super::Workspace;
use crate::ui::styles::DiffColors;

const DOTS: [&str; 4] = ["", ".", "..", "..."];

pub fn render(
    ws: &mut Workspace,
    frame: &mut Frame,
    area: Rect,
    focused: bool,
    colors: &DiffColors,
) {
    let width = area.width.saturating_sub(2) as usize;
    let anim = ws.anim_frame;
    let Some(agent) = ws.agents.get_mut(ws.selected) else {
        widgets::render_placeholder(frame, area, "No agent");
        return;
    };

    if agent.plan.markdown.trim().is_empty() {
        if agent.plan.generating {
            let dots = DOTS[(anim / 2) % DOTS.len()];
            widgets::render_placeholder(frame, area, &format!("Generating plan{dots}"));
        } else {
            widgets::render_placeholder(
                frame,
                area,
                "No plan yet — ask the agent in plan mode.",
            );
        }
        return;
    }

    let lines = render_markdown(&agent.plan.markdown, width);
    let title = if agent.plan.generating {
        " Plan (generating…)"
    } else {
        " Plan"
    };
    widgets::render_text_panel(frame, area, title, focused, &lines, &mut agent.plan_scroll, colors);
}

/// Lightweight Markdown rendering tuned for correct width-wrapping (so the
/// scrollbar stays accurate). Handles headings, bullets, and fenced code.
fn render_markdown(md: &str, width: usize) -> Vec<Line<'static>> {
    let mut lines: Vec<Line<'static>> = Vec::new();
    let mut in_code = false;

    for raw in md.split('\n') {
        let trimmed = raw.trim_start();

        if trimmed.starts_with("```") {
            in_code = !in_code;
            continue;
        }

        if in_code {
            lines.push(Line::from(Span::styled(
                truncate(raw, width),
                Style::default().fg(Color::Green),
            )));
            continue;
        }

        if let Some(rest) = trimmed.strip_prefix("### ") {
            push_wrapped(&mut lines, rest, width, heading_style());
        } else if let Some(rest) = trimmed.strip_prefix("## ") {
            push_wrapped(&mut lines, rest, width, heading_style());
        } else if let Some(rest) = trimmed.strip_prefix("# ") {
            push_wrapped(&mut lines, rest, width, heading_style());
        } else if let Some(rest) = trimmed
            .strip_prefix("- ")
            .or_else(|| trimmed.strip_prefix("* "))
        {
            let wrapped = widgets::wrap_plain(rest, width.saturating_sub(2));
            for (i, wl) in wrapped.into_iter().enumerate() {
                if i == 0 {
                    lines.push(Line::from(vec![
                        Span::styled("• ", Style::default().fg(Color::Magenta)),
                        Span::raw(wl),
                    ]));
                } else {
                    lines.push(Line::from(Span::raw(format!("  {wl}"))));
                }
            }
        } else {
            push_wrapped(&mut lines, raw, width, Style::default());
        }
    }
    lines
}

fn heading_style() -> Style {
    Style::default()
        .fg(Color::White)
        .add_modifier(Modifier::BOLD)
}

fn push_wrapped(lines: &mut Vec<Line<'static>>, text: &str, width: usize, style: Style) {
    for wl in widgets::wrap_plain(text, width) {
        lines.push(Line::from(Span::styled(wl, style)));
    }
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let taken: String = s.chars().take(max.saturating_sub(1)).collect();
    format!("{taken}…")
}
