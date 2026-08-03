---
version: alpha
name: FrameOPS
summary: FrameSeven Ops Deck for dense, synthetic security operations previews.
colors:
  primary: "#85f266"
  secondary: "#62d8de"
  tertiary: "#ff7043"
  ink: "#061417"
  surface: "#0a1d22"
  surface-raised: "#0d252b"
  line: "#294148"
  text: "#f2f7f3"
  muted: "#a7bab6"
  focus: "#ffe16b"
typography:
  display:
    fontFamily: "Aptos, Segoe UI, system-ui, sans-serif"
    fontSize: "4rem"
    fontWeight: 400
    lineHeight: 0.98
    letterSpacing: "-0.035em"
  body:
    fontFamily: "Aptos, Segoe UI, system-ui, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "0.65rem"
    fontWeight: 700
    lineHeight: 1.4
    letterSpacing: "0.06em"
rounded:
  surface: "14px"
  control: "8px"
spacing:
  compact: "10px"
  panel: "20px"
  deck: "56px"
components:
  navigation-active:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.text}"
    rounded: "{rounded.control}"
    padding: "0 12px"
  report-status:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.ink}"
    rounded: "{rounded.surface}"
    padding: "20px"
---

# Design System: FrameOPS

## Overview

**Creative North Star: "FrameSeven Ops Deck"**

A dense, near-black operational desk for pentest planning and consolidation. Attack surface and evidence chain are the signature reading mechanism; generic metric-card dashboards are rejected.

The system is designed for an operator reviewing a synthetic local preview under low ambient light. Green denotes authorized scope or affirmative progress, orange is reserved for review states, and every state keeps a written label or symbol.

## Colors

Electric scope green is the limited command signal. Cyan identifies data and evidence detail; alert orange marks a review requirement. Petro-teal surfaces create depth with tonal layering, not nested cards.

**The Signal Discipline Rule.** Use green for active navigation, authorized scope, and the sole high-emphasis delivery panel; do not use it as general decoration.

## Typography

Aptos and Segoe UI form the workhorse UI voice. Display scale is reserved for the task statement; dense labels and measurements use monospace only where they describe data, IDs, or operational state.

**The Evidence Label Rule.** Uppercase monospace labels identify artifacts and states; prose and controls remain in the UI sans.

## Layout

Desktop uses a fixed 224px operations rail with a bounded 1200px content deck. The first viewport holds navigation, engagement context, the attack-surface map, and a dense operating summary. At 900px the rail becomes horizontally scrollable navigation; at 620px, panels become a single reading column and the surface map stacks its nodes.

## Elevation & Depth

Tonal layering carries routine depth. Only the attack-surface region uses a broad, low-contrast shadow (`0 24px 56px rgba(0,0,0,.24)`) to establish it as the workbench's central artifact.

## Shapes

Operational panels use gently rounded corners (14px); navigation controls use compact 8px corners. Evidence nodes remain hard-edged rectangles, reinforcing their machine-record character.

## Components

### Navigation

The active row uses raised teal, a green 2px inset signal, and persistent text contrast. All navigation links meet a 44px minimum target.

### Operational Panels

Panels use dark tonal surfaces and 20px padding. Finding review is distinguished by orange border, warning glyph, and explicit text. The delivery panel reverses to green with dark text only for its limited state summary.

### Attack Surface

The signature component is a bordered evidence map with named synthetic targets, trace lines, and written legend labels. It is illustrative and never implies a connection or active scan.

## Do's and Don'ts

### Do:
- **Do** make synthetic/demo status explicit in the command bar and content states.
- **Do** use labels, glyphs, and text with semantic colors.
- **Do** preserve a visible yellow keyboard focus ring and reduced-motion behavior.

### Don't:
- **Don't** claim API activity, authentication, persistence, evidence hashes, or real findings in previews.
- **Don't** replace the attack-surface mechanism with generic metric tiles.
- **Don't** use monospace as paragraph or display typography.
