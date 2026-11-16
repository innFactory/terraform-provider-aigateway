# Changelog

## v0.11.0 (unreleased)

- model: `context_window` and `max_output_tokens` (gateway >= 1.1.0); omitted keeps the stored value. An attribute unset in config is omitted from create and update requests, never sent as null, so a window set in the dashboard is not cleared.
