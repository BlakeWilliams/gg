//! Left pane: the list of agents. Each agent shows its name, branch, live
//! status (with a spinner while working), mode badge, and a PR badge when a
//! pull request exists.

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;

use super::agent::{Agent, AgentMode, AgentStatus};
use super::widgets;
use super::Workspace;
use crate::ui::styles::DiffColors;

const SPINNER: [&str; 10] = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

pub fn render(ws: &Workspace, frame: &mut Frame, area: Rect, focused: bool, _colors: &DiffColors) {
    if area.width == 0 || area.height == 0 {
        return;
    }

    // Header.
    let header = Rect::new(area.x, area.y, area.width, 1);
    let title = format!(" {} Agents", ws.agents.len());
    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(title, widgets::title_style(focused)))),
        header,
    );

    if area.height <= 1 {
        return;
    }
    let body = Rect::new(area.x, area.y + 1, area.width, area.height - 1);

    // Each agent occupies two rows: name line + meta line.
    let rows_per = 2u16;
    let capacity = (body.height / rows_per) as usize;
    let offset = if ws.selected >= capacity && capacity > 0 {
        ws.selected + 1 - capacity
    } else {
        0
    };

    let frame_idx = ws.anim_frame % SPINNER.len();
    let mut y = body.y;
    for (i, agent) in ws.agents.iter().enumerate().skip(offset) {
        if y + 1 >= body.y + body.height {
            break;
        }
        let selected = i == ws.selected;
        render_agent(frame, body, y, agent, selected, frame_idx);
        y += rows_per;
    }
}

fn render_agent(
    frame: &mut Frame,
    area: Rect,
    y: u16,
    agent: &Agent,
    selected: bool,
    frame_idx: usize,
) {
    let name_style = if selected {
        Style::default()
            .fg(Color::Magenta)
            .add_modifier(Modifier::BOLD)
    } else {
        Style::default()
    };

    let cursor = if selected { "▸ " } else { "  " };
    let name_line = Line::from(vec![
        Span::styled(cursor, name_style),
        Span::styled(truncate(&agent.name, area.width as usize - 2), name_style),
    ]);
    frame.render_widget(
        Paragraph::new(name_line),
        Rect::new(area.x, y, area.width, 1),
    );

    // Meta line: status/spinner + mode + PR badge.
    let mut meta: Vec<Span> = vec![Span::raw("    ")];
    let working = matches!(agent.status, AgentStatus::Planning | AgentStatus::Executing);
    if working {
        meta.push(Span::styled(
            format!("{} ", SPINNER[frame_idx]),
            Style::default().fg(Color::Magenta),
        ));
    }
    let status_color = match agent.status {
        AgentStatus::Error => Color::Red,
        AgentStatus::Idle => Color::DarkGray,
        _ => Color::Magenta,
    };
    meta.push(Span::styled(
        agent.status_label().to_string(),
        Style::default().fg(status_color),
    ));

    let (mode_label, mode_color) = match agent.mode {
        AgentMode::Plan => ("plan", Color::Blue),
        AgentMode::Execute => ("exec", Color::Magenta),
    };
    meta.push(Span::styled("  ", widgets::dim()));
    meta.push(Span::styled(
        mode_label.to_string(),
        Style::default().fg(mode_color),
    ));

    if let Some(pr) = &agent.pr {
        meta.push(Span::styled("  ", widgets::dim()));
        meta.push(Span::styled(
            format!("PR#{}", pr.number),
            Style::default().fg(Color::Cyan),
        ));
    }

    frame.render_widget(
        Paragraph::new(Line::from(meta)),
        Rect::new(area.x, y + 1, area.width, 1),
    );
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let taken: String = s.chars().take(max.saturating_sub(1)).collect();
    format!("{taken}…")
}
