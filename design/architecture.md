# Rules for Pennies — Architecture & Design Decisions

## Overview

"Ask the Arbiter" — an interactive service where users describe penny encounters (text or photo) and receive mock-legalistic rulings from an AI persona steeped in the Rules for Pennies corpus. The rules are never exposed directly; rulings reference rule names (rendered in small caps) without reproducing full text. The goal is to entertain, go viral, and sell the book.

Book sales link: https://rules-for-pennies.stonylanepress.com

## Architecture

```
┌─────────────────────────────────────────────┐
│  rulesforpennies.ai (Hugo static shell)     │
│  - Landing page / book CTA                  │
│  - "Ask the Arbiter" JS frontend            │
│  - Results display + share                  │
│  - Mobile-responsive (camera upload via     │
│    browser for in-the-field use)             │
└──────────────┬──────────────────────────────┘
               │ /api/*  (Caddy reverse proxy)
               ▼
┌─────────────────────────────────────────────┐
│  arbiterd (Go binary on gateway)            │
│  - POST /api/ruling  (text + optional image)│
│  - Rate limiting (session-based)            │
│  - Daily spend tracking + cost cap          │
│  - Input filtering (jailbreak defense)      │
│  - Output filtering (rule leak prevention)  │
│  - OpenAI GPT-4o API calls                  │
│  - Usage tracking / analytics               │
│  - SQLite database (WAL mode)               │
│  - Systemd managed service                  │
└─────────────────────────────────────────────┘
```

## Decisions

### LLM

- **Provider**: OpenAI GPT-4o (text + vision)
- **No audio**: Text responses only. Users can use screen readers if needed.
- **System prompt**: Full rule corpus loaded. AI responds as "the Arbiter" in deadpan mock-legalistic register.
- **Rule references**: Displayed in small caps (CSS `font-variant: small-caps`), not ALL CAPS.

### Input Modes (v1)

| Mode | Input | Processing |
|------|-------|-----------|
| Text | User describes penny situation | LLM matches against rules, delivers verdict |

Photo upload and combined input deferred to v2.

### Rate Limiting

- **Session-based**: Cookie/localStorage token, 10 rulings per day per session.
- **Per-IP backstop**: 30/day to catch bots that don't hold cookies.
- **No login or email required**: Zero friction.

### Cost Control

- **Daily budget**: Configurable, default $10/day.
- **Soft cap at 80% ($8)**: Adds 10-second delay, shows "The Arbiter is deliberating..."
- **Hard cap at 100% ($10)**: Returns in-character shutdown message with book link.
- **Spend tracking**: Server-side, per-ruling cost recorded from OpenAI response usage data.

### Image Handling (deferred to v2)

v1 is text-only. Photo upload, image cards, and gallery deferred.

### Abuse / Jailbreak Defense (Heavy)

1. **System prompt hardening**: Explicit instructions to never reveal rules, never break character.
2. **Input filtering**: Reject prompts containing known jailbreak patterns ("ignore previous instructions", "list all rules", etc.).
3. **Output filtering**: Scan responses before sending; if they contain long verbatim rule text, block and return a canned Arbiter deflection.

### Backend Deployment

- **Go binary** (`arbiterd`) cross-compiled for Linux amd64.
- **Systemd service** on gateway (167.71.187.196), same pattern as `signupd`.
- **Port**: 9092 (or similar, not conflicting with signupd on 9091).
- **Caddy proxies** `/api/*` → `localhost:9092`.
- **Deploy workflow**: Build locally → scp to server → systemctl restart arbiterd.
- **No Docker**.

### Database

- **SQLite** in WAL mode, single file on gateway.
- **Tables**: sessions, rulings (text + image path + cost + timestamp), daily_spend, blocked_inputs.


### Frontend

- **Hugo static site**: Landing page + "Ask the Arbiter" interactive section.
- **Mobile-responsive**: Works well from phone browsers.
- **No separate iPhone app** (deferred). PWA candidate for future upgrade if traction warrants.

### Repository

- **Repo**: github.com/TrueBlocks/rulesforpennies.io
- **Submodule**: `pennies/` in trueblocks-art
- **Hugo site source**: `pennies/` root (hugo.toml, content/, layouts/, static/)
- **Backend source**: `pennies/cmd/arbiterd/` (Go)
- **Siteman config**: source points to `pennies/`, deploys to `/var/www/rulesforpennies.ai` on gateway.

## Not Yet Decided

- Exact system prompt wording and persona tuning
- v2: Photo upload, image cards, gallery, moderation
- Terms of use / privacy policy language
- Whether to redirect rules-for-pennies.stonylanepress.com → rulesforpennies.ai
