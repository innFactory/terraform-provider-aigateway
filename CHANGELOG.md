# Changelog

## v0.11.0 (unreleased)

- model: `context_window` and `max_output_tokens` (gateway >= 1.1.0); omitted keeps the stored value. An attribute unset in the configuration is not sent on create, update or adopt, and never sent as null, so a window set in the dashboard is not cleared. Removing the attribute keeps the current value; clear it in the dashboard. A gateway older than 1.1.0 is reported with a clear error instead of an inconsistent-result error.
