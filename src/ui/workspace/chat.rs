//! Right pane: the conversation with the active agent, plus a composer at the
//! bottom. Streams live as the agent replies. Hidden in review mode.

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;

use super::agent::{Agent, ChatAuthor, ChatBlock, ToolStatus};
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
    if area.width == 0 || area.height == 0 {
        return;
    }

    let input = ws.chat_input.clone();
    let cursor = ws.chat_cursor;
    let anim = ws.anim_frame;

    // Reserve the bottom 3 rows for the composer (rule + label + input).
    let composer_h = 3u16.min(area.height);
    let convo_h = area.height.saturating_sub(composer_h);
    let convo_area = Rect::new(area.x, area.y, area.width, convo_h);
    let composer_area = Rect::new(
        area.x,
        area.y + convo_h,
        area.width,
        composer_h,
    );

    let width = area.width.saturating_sub(2) as usize;

    let Some(agent) = ws.agents.get_mut(ws.selected) else {
        widgets::render_placeholder(frame, area, "No agent");
        return;
    };

    let lines = build_lines(agent, width, anim);
    // Keep the view pinned to the latest message while streaming.
    if agent.is_streaming() {
        agent.chat_scroll.scroll_to_bottom();
    }
    widgets::render_text_panel(
        frame,
        convo_area,
        " Chat",
        focused,
        &lines,
        &mut agent.chat_scroll,
        colors,
    );

    render_composer(frame, composer_area, focused, &input, cursor, colors);
}

fn build_lines(agent: &Agent, width: usize, anim: usize) -> Vec<Line<'static>> {
    let mut lines: Vec<Line<'static>> = Vec::new();
    for (mi, msg) in agent.conversation.iter().enumerate() {
        match msg.author {
            ChatAuthor::You => {
                lines.push(Line::from(Span::styled(
                    "You",
                    Style::default()
                        .fg(Color::White)
                        .add_modifier(Modifier::BOLD | Modifier::UNDERLINED),
                )));
            }
            ChatAuthor::Agent => {
                lines.push(Line::from(Span::styled(
                    "✦ Agent",
                    Style::default()
                        .fg(Color::Cyan)
                        .add_modifier(Modifier::BOLD),
                )));
            }
        }

        for block in &msg.blocks {
            match block {
                ChatBlock::Text(text) => {
                    for wl in widgets::wrap_plain(text, width) {
                        lines.push(Line::from(Span::raw(wl)));
                    }
                }
                ChatBlock::Tool(tool) => {
                    let (icon, color) = match tool.status {
                        ToolStatus::Running => ("●", Color::Yellow),
                        ToolStatus::Done => ("●", Color::Green),
                        ToolStatus::Failed => ("●", Color::Red),
                    };
                    let label = if tool.summary.is_empty() {
                        tool.name.clone()
                    } else {
                        format!("{} {}", tool.name, tool.summary)
                    };
                    lines.push(Line::from(vec![
                        Span::styled(format!(" {icon} "), Style::default().fg(color)),
                        Span::styled(truncate(&label, width.saturating_sub(3)), widgets::dim()),
                    ]));
                }
            }
        }

        // Animated "thinking" indicator on the streaming message.
        let is_streaming_msg = agent.streaming_idx == Some(mi);
        if is_streaming_msg && msg.is_empty() {
            let dots = DOTS[(anim / 2) % DOTS.len()];
            lines.push(Line::from(Span::styled(
                format!("Thinking{dots}"),
                widgets::dim(),
            )));
        }

        lines.push(Line::from(Span::raw("")));
    }

    if agent.conversation.is_empty() {
        lines.push(Line::from(Span::styled(
            "Send a message to start.",
            widgets::dim(),
        )));
    }
    lines
}

fn render_composer(
    frame: &mut Frame,
    area: Rect,
    focused: bool,
    input: &str,
    cursor: usize,
    colors: &DiffColors,
) {
    if area.height == 0 {
        return;
    }
    // Top rule.
    let rule = "─".repeat(area.width as usize);
    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(
            rule,
            Style::default().fg(colors.chrome_fg),
        ))),
        Rect::new(area.x, area.y, area.width, 1),
    );

    if area.height < 2 {
        return;
    }

    // Input line with prompt marker and a block cursor when focused.
    let mut spans = vec![Span::styled(
        "› ",
        Style::default()
            .fg(Color::Magenta)
            .add_modifier(Modifier::BOLD),
    )];

    if input.is_empty() && !focused {
        spans.push(Span::styled("type a message…", widgets::dim()));
    } else if focused {
        let chars: Vec<char> = input.chars().collect();
        let c = cursor.min(chars.len());
        let before: String = chars[..c].iter().collect();
        spans.push(Span::raw(before));
        if c < chars.len() {
            let at = chars[c].to_string();
            spans.push(Span::styled(
                at,
                Style::default().fg(Color::Black).bg(Color::White),
            ));
            let after: String = chars[c + 1..].iter().collect();
            spans.push(Span::raw(after));
        } else {
            spans.push(Span::styled(
                "▍",
                Style::default().fg(Color::White),
            ));
        }
    } else {
        spans.push(Span::raw(input.to_string()));
    }

    frame.render_widget(
        Paragraph::new(Line::from(spans)),
        Rect::new(area.x, area.y + 1, area.width, 1),
    );
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        return s.to_string();
    }
    let taken: String = s.chars().take(max.saturating_sub(1)).collect();
    format!("{taken}…")
}
