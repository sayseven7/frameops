---
version: 1
slug: "web-app-page-tsx"
primary_target: "app/page.tsx"
related_targets: ["app/protected-workspace.tsx"]
---

Scope: authenticated FrameOPS workspace rooted at `web/app/page.tsx`.

Mode: Operate. Audience: internal pentesters reviewing planning and consolidation work. Task: scan the selected engagement's authorized scope, checklist, finding review state, evidence custody, retest history, import history, and report availability from the authenticated API-backed workspace. Preserve real API behavior and user data; never add synthetic metrics, targets, or claims.

Chosen direction: FrameSeven Ops Deck. The workbench in the first viewport connects the selected project's authorized scope to its loaded finding records. Keep the fixed operations rail at desktop, horizontal navigation at tablet/mobile, semantic dark/light/system tokens, visible focus, reduced motion, localized state labels, and horizontally scrollable data tables below 700px. This is production UI, not a static preview.
