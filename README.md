# News desk for traders

GoNewsDesk is a terminal news desk for traders. It merges real-time market news from multiple providers, applies headline filters, and displays the result in an interactive TUI.

![Screenshot](gonewsdesk-demo.gif)

## Features

- Merge multiple news streams into one feed
- Filter headlines with include/exclude keyword lists
- Highlight important headlines using keyword matches
- Open a detailed modal view for each news item
- Toggle an in-app log panel for debugging and monitoring

## News sources

### Stocklabs

Stocklabs provides a stream of market-related posts (primarily from X-focused news profiles). In GoNewsDesk, this source can be enabled independently and optionally backfilled with recent historical items at startup.

- Config key: `stocklabs.enabled`
- Historical backfill key: `stocklabs.includeHistorical`

However you don't need to use this app to see the news from Stocklabs, you can just visit their news page https://stocklabs.com/news and see the news there.

### Alpaca

Alpaca provides a real-time news WebSocket feed (configured in this project via API keys) from Benzinga. This source is optional and only starts when all of the following are true:

- `alpaca.enabled` is `true`
- `alpaca.apiKeyID` is set
- `alpaca.apiKeySecret` is set

Alpaca is stream-only in this app (no historical backfill in the current implementation).

## Filters

Filters are applied to the merged stream, regardless of source.

- `excludeKeywords`: if any keyword matches the headline, the item is dropped.
- `includeKeywords`: if this list is non-empty, at least one keyword must match.
- `highlightKeywords`: if any keyword matches, the news tile is highlighted in the UI.

Matching is case-insensitive and based on substring checks.

## Controls

- `F1`: open/close the logs panel
- `q`: quit the application
- `Enter`: open the selected news details modal (and close it when modal is focused)
- `Esc`: close modal/logs panel
- `Arrow Up` / `Arrow Down`: move selection
- `Page Up` / `Page Down`: scroll by one page
- `Home`: jump to the first item
- `End`: jump to the latest item

## Configuration

By default, the app reads configuration from `config.yaml` in the current directory.

You can override the config path with:

- `--config <path>`
- `CONFIG=<path>` environment variable

Example configuration is available in `config.yaml`.

## Installation

You can run a downloaded release binary, or install from source with Go:

```bash
go install github.com/barandras/gonewsdesk@latest
```

Then run:

```bash
gonewsdesk
```

