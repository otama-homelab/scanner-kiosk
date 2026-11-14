---
name: Scanner
description: A precise document workspace with retained Scanner identity.
colors:
  primary: "#4f46e5"
  primary-hover: "#4338ca"
  primary-soft: "#eeedff"
  on-primary: "#fff"
  canvas: "#f2f4f7"
  surface: "#fff"
  ink: "#242a35"
  muted: "#596473"
  line: "#d7dde5"
  control-line: "#81869b"
  dark-primary: "#b8b2ff"
  dark-primary-soft: "#30304f"
  dark-on-primary: "#171431"
  dark-canvas: "#15181e"
  dark-surface: "#1e232b"
  dark-surface-raised: "#262d37"
  dark-ink: "#edf0f5"
  dark-muted: "#b7c1cf"
  dark-line: "#3c4654"
  danger: "#a32936"
  dark-danger: "#ffc0c7"
  home-canvas: "#f7f7fa"
  home-dark-canvas: "#11121a"
typography:
  title:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "22px"
    fontWeight: 650
    lineHeight: 1.3
    letterSpacing: "-0.3px"
  body:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "14px"
  home-body:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif"
    fontSize: "18px"
    lineHeight: 1.65
rounded:
  control: "8px"
  workspace: "12px"
  home-panel: "20px"
spacing:
  compact: "8px"
  group: "12px"
  section: "16px"
  pane: "20px"
  outer: "24px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.control}"
    padding: "10px 16px"
  button-primary-hover:
    backgroundColor: "{colors.primary-hover}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "10px 16px"
  workspace:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.workspace}"
---
# Design System: Scanner

## Overview

**Creative North Star: "The Document Workbench"**

Scanner uses a restrained, professional working surface: neutral surroundings, crisp boundaries and a recognizable indigo accent. Useful document content supplies the density; decoration does not compete with the original.

The current document route is intentionally more compact and neutral than the retained acquisition and home screens. Its local palette and control geometry are scoped by `body:has(.document-screen)` in `web/public/document-review.css`, loaded after `web/public/style.css`. These document tokens are not a claim that every route has been restyled.

**Key Characteristics:**

- Neutral working surfaces with a restrained Scanner accent.
- Readable working typography and compact, explicit controls.
- Borders and tonal separation before decorative elevation.
- Responsive arrangements that retain content identity and accessible names.

## Colors

### Primary

Indigo identifies the Scanner brand, primary actions, selected controls and focus. The pale accent surface supports selection without painting the whole workspace. Dark appearance uses the existing lighter accent and dark ink pairing rather than simply reusing light-theme button colors.

### Neutral

The document canvas is cool gray, with white working surfaces and charcoal ink. Dark appearance uses a neutral charcoal canvas, a lighter work surface and a raised surface. Muted text and structural dividers have their own theme values. Control borders retain the shared control-line token rather than inheriting the lighter structural line. Danger remains semantic, with a separate dark value. Original document pixels are not recolored to match the theme.

Home canvas tokens describe the retained base route; its remaining surface, text and line values continue to live in the base stylesheet. This is a scoped extraction, not an exhaustive replacement token registry.

**The The Scoped Surface Rule.** Apply the document palette to the document route; preserve the home and acquisition palette outside that boundary.

## Typography

The native sans stack is a functional app/body stack, not a decorative display typeface. There is no separate display role to inherit for marketing pages. Document titles use the title token, shrinking to 20px on narrow screens. Document section headings use 20px, reducing to 18px on narrow screens; OCR result headings use 17px. Working text uses the body token, controls generally use 15px, and local labels use 14px. Metadata uses 13px; small state labels may use 12px. OCR transcripts preserve whitespace and wrap long content, with line-height 1.7 for plain text and 1.6 for selectable spans.

The inherited root remains the home-body token. Consequently rem-based existing controls still reference the root size; document-specific pixel declarations are deliberate overrides. Coarse-pointer input fields retain a minimum 16px font to avoid mobile input zoom.

## Layout

The document shell caps at 1560px with 24px horizontal padding; narrow screens use 16px. Compact app chrome precedes the document heading and workflow row. Correction and OCR both pair the source pane with local actions or text at a 1.2:1 ratio. Both use the same fitted viewer and zoom controls; the shared page selector stays above the workspace. The same selected page remains the working unit across views.

At 960px and below, correction and OCR become one column and the source remains visible above local actions or text. There is no source visibility toggle. At 600px and below, action rails become wrapping rows, toolbar actions reflow, comparison results stack, and page controls retain readable numeric identity. On screens at least 961px wide and 701px high, the document shell fills the viewport with bounded scroll areas for source, transcript and local controls; zoom and adoption decisions stay outside transcript scrolling. Shorter screens use normal page flow.

The home/acquisition layer retains its own 760px and 900px responsive rules and wider rounded panels; document breakpoints do not replace them.

**The The Content Boundary Rule.** Use a single bounded workspace with local controls rather than repeating full-size source images as decorative cards.

## Elevation & Depth

Document workspaces are flat at rest: backgrounds and thin borders define their hierarchy; document panels and buttons explicitly remove inherited shadows. The retained home layer still has subtle ambient shadows. Menus and dialogs may retain overlay elevation and the dialog backdrop; a document workspace is not a floating modal.

## Shapes

Document controls use gently curved corners through the control token; workspaces and delivery containers use the workspace token. Tabs use a straight underline and no corner rounding. Selectable OCR spans use 6px corners. Shared controls and home panels retain their existing larger rounding outside the document override. Thin borders carry structure; selection uses accent outlines or an accent surface rather than a new decorative shape.

## Components

### Buttons

Document primary Save uses accent ink pairing, a stable toolbar position and a minimum width of 136px on desktop, 124px on narrow screens. Shared primary buttons retain a minimum height of 58px; local review actions explicitly use 48px minimums. Secondary document buttons use the shared surface and control border with no shadow. Pointer hover uses accent-soft and accent border; primary hover uses primary-hover. Disabled controls keep the existing muted/disabled treatment and are not mistaken for selected controls. Keyboard focus retains a visible 3px focus outline with 4px offset.

Shared action SVGs use currentColor strokes (1.7), round caps and joins, and accessible button names. Document page actions expose those names as visible labels on desktop; Scan-next becomes icon-only on narrow screens while retaining its accessible name. Appearance controls also use inline SVGs.

**The The Immediate Theme Rule.** Document-route buttons change theme without a background transition so ink and surface colors switch together.

### Inputs / Fields

Native select and input controls preserve platform behavior, surface fill, control-line border and readable text. The document page selector has a minimum 44px height and compact page count; mobile keeps its accessible label while reducing visible label duplication. Input errors use the semantic danger border. Retained acquisition fields use the base control geometry.

### Navigation

Document Adjust/Text tabs are a native-feeling horizontal tablist with a 2px accent underline and stronger text when selected. The workflow row retains the imported state and stable Save toolbar. Saved/New/Compare is a separate compact result choice group, not another full-page navigation layer. Its selected button uses accent-soft, accent text and stronger weight. Home navigation and appearance controls keep their existing shared patterns.

### Cards / Containers

The document workspace has one thin outer boundary, not nested elevated cards. OCR source and text are divided by a structural rule; the text pane uses pane spacing. Transcript content can scroll without pushing decisions away. A separately bounded delivery section follows the local work surface. Home/acquisition panels retain their existing panel token and roomier padding.

### Recognition Source and Decisions

The shared source viewer displays the immutable source belonging to the selected recognition result. Saved/New/Compare stays above the image so narrow-screen users can change the source without scrolling through the transcript. Source identity, not merely visual similarity, decides which image is shown. Position-linked spans use a transparent highlight with accent stroke and a selected text outline. Ordinary review shows one result; explicit comparison may show two text results while retaining one source viewer. Adopt and discard remain explicit decisions and preserve work context. The text pane owns transcript scrolling; selectable span lists expand naturally rather than creating another scroll area. Recognition model, language and quality settings open inline in the same workspace, temporarily replacing the review area. Close or Escape restores the review and its focus. On desktop, only the settings body may scroll; on narrow or short screens, settings follow page flow with a visible sticky action row. These visual rules do not weaken source identity, expiry, recovery or retention behavior.

## Do's and Don'ts

### Do:

- Do preserve the Scanner logo and use the existing accent for primary and selected states.
- Do keep labels, focus indicators and touch controls usable in Japanese and English.
- Do keep document controls next to the content they affect and preserve the selected page when changing views.
- Do distinguish document-route overrides from retained home and acquisition styles.

### Don't:

- Don't add decorative metrics, unsupported accuracy claims or imagery that suggests automatic correction is qualified.
- Don't use Unicode glyphs as substitutes for the shared SVG action icons.
- Don't repeat a full original image in ordinary OCR review; make comparison an explicit choice.


### Unified file intake

Home exposes one Import action in the Files & camera block for PDF, JPEG and PNG, alongside the existing image-only Take a photo action. There is no separate PDF entry in the home heading. Native file selection dispatches to the existing format-specific workflows; the legacy PDF route remains available for its established form/recovery behavior. Keep the current acquisition panels, tokens and purpose controls.

### Shared source viewer

Adjust raster images, existing PDF canvases and OCR SVGs use one viewer for fitting, 25% zoom steps, mouse dragging and native scrolling. Fit is100%; the maximum is400% of fitted size. The zoom label explicitly states that basis. Zoom preserves the document point under the viewport center and updates real scroll dimensions so every edge remains reachable. Fit restores the complete original. Arrow keys scroll; plus/minus and0 provide zoom/fit shortcuts without intercepting browser Ctrl/Command zoom. Touch keeps native scrolling and browser pinch zoom. The mobile viewer is42dvh with a220px minimum; the desktop viewer fills its bounded pane.

Viewer buttons use the shadcn/ui Button/ButtonGroup DOM adaptation and class-variance-authority, compiled with Tailwind CSS4.3.5. Semantic tokens map to the existing Scanner palette. Existing app reset, themes and native button semantics are retained. The MIT source/notice records live underweb/vendor/shadcn-ui and the notice is included at/ui/NOTICE.txt in the delivered assets. The rest of the application retains its existing controls.
