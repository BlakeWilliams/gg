//! Shared rendering helpers for the workspace panes. These follow the existing
//! `gg` visual language: hand-drawn Unicode chrome, Cyan focus accents,
//! White+BOLD active titles vs DarkGray, and `┃`/`│` scrollbars.

use ratatui::Frame;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::Paragraph;
use unicode_width::UnicodeWidthStr;

use crate::ui::scroll::ScrollState;
use crate::ui::styles::DiffColors;

pub fn dim() -> Style {
    Style::default().fg(Color::DarkGray)
}

pub fn bright() -> Style {
    Style::default().fg(Color::White).add_modifier(Modifier::BOLD)
}

/// Title style: bright+bold when the pane is focused, dim otherwise.
pub fn title_style(focused: bool) -> Style {
    if focused { bright() } else { dim() }
}

/// Word-wrap plain text to `width` columns, preserving explicit newlines.
pub fn wrap_plain(text: &str, width: usize) -> Vec<String> {
    let width = width.max(1);
    let mut out = Vec::new();
    for raw_line in text.split('\n') {
        if raw_line.is_empty() {
            out.push(String::new());
            continue;
        }
        let mut current = String::new();
        let mut cur_w = 0usize;
        for word in raw_line.split(' ') {
            let w = UnicodeWidthStr::width(word);
            if cur_w == 0 {
                // Word may itself exceed width — hard-split it.
                if w > width {
                    for chunk in hard_split(word, width) {
                        out.push(chunk);
                    }
                    current.clear();
                    cur_w = 0;
                } else {
                    current.push_str(word);
                    cur_w = w;
                }
            } else if cur_w + 1 + w <= width {
                current.push(' ');
                current.push_str(word);
                cur_w += 1 + w;
            } else {
                out.push(std::mem::take(&mut current));
                if w > width {
                    for chunk in hard_split(word, width) {
                        out.push(chunk);
                    }
                    cur_w = 0;
                } else {
                    current.push_str(word);
                    cur_w = w;
                }
            }
        }
        out.push(current);
    }
    out
}

fn hard_split(word: &str, width: usize) -> Vec<String> {
    let mut chunks = Vec::new();
    let mut current = String::new();
    let mut cur_w = 0;
    for ch in word.chars() {
        let w = UnicodeWidthStr::width(ch.to_string().as_str());
        if cur_w + w > width {
            chunks.push(std::mem::take(&mut current));
            cur_w = 0;
        }
        current.push(ch);
        cur_w += w;
    }
    if !current.is_empty() {
        chunks.push(current);
    }
    chunks
}

/// Render a vertical separator column (`│`) down `area`.
pub fn render_vline(frame: &mut Frame, x: u16, y: u16, height: u16, color: Color) {
    for i in 0..height {
        frame.render_widget(
            Paragraph::new(Line::from(Span::styled("│", Style::default().fg(color)))),
            Rect::new(x, y + i, 1, 1),
        );
    }
}

/// Render a titled, scrollable text panel into `area`. The first row is the
/// title; the remaining rows show `lines` (already wrapped to width) with a
/// `┃`/`│` scrollbar in the last column. Returns the inner content height.
pub fn render_text_panel(
    frame: &mut Frame,
    area: Rect,
    title: &str,
    focused: bool,
    lines: &[Line<'static>],
    scroll: &mut ScrollState,
    colors: &DiffColors,
) {
    if area.height == 0 || area.width == 0 {
        return;
    }
    let header = Rect::new(area.x, area.y, area.width, 1);
    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(
            title.to_string(),
            title_style(focused),
        ))),
        header,
    );
    if area.height <= 1 {
        return;
    }

    let content = Rect::new(area.x, area.y + 1, area.width, area.height - 1);
    let view_h = content.height as usize;
    scroll.set_viewport_height(view_h);
    scroll.set_total(lines.len());
    scroll.clamp();
    let (thumb_start, thumb_len) = scroll.scrollbar();
    let offset = scroll.offset;

    let inner = Rect::new(
        content.x,
        content.y,
        content.width.saturating_sub(1),
        content.height,
    );
    let visible: Vec<Line> = lines.iter().skip(offset).take(view_h).cloned().collect();
    frame.render_widget(Paragraph::new(visible), inner);

    // Scrollbar column.
    let sb_x = content.x + content.width - 1;
    for i in 0..view_h {
        let in_thumb =
            thumb_start >= 0 && (i as i32) >= thumb_start && (i as i32) < thumb_start + thumb_len;
        let (ch, style) = if thumb_start < 0 {
            (" ", Style::default())
        } else if in_thumb {
            ("┃", Style::default().fg(colors.line_number_fg))
        } else {
            ("│", Style::default().fg(colors.chrome_fg))
        };
        frame.render_widget(
            Paragraph::new(Line::from(Span::styled(ch, style))),
            Rect::new(sb_x, content.y + i as u16, 1, 1),
        );
    }
}

/// A centered, dimmed placeholder for empty panes.
pub fn render_placeholder(frame: &mut Frame, area: Rect, message: &str) {
    if area.height == 0 {
        return;
    }
    let y = area.y + area.height / 2;
    let msg_w = UnicodeWidthStr::width(message) as u16;
    let x = area.x + area.width.saturating_sub(msg_w) / 2;
    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(message.to_string(), dim()))),
        Rect::new(x, y, msg_w.min(area.width), 1),
    );
}
