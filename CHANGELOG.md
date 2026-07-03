# Changelog

## 1.0.0 (Unreleased)

Initial release.

- Add a datasource for ERDDAP tabledap endpoints, configured with a single ERDDAP base URL (public
  servers only; no API keys or secrets).
- Add a query editor with Dataset ID, Variables (comma-separated), and Constraints (raw ERDDAP
  constraint expression) fields.
- Map the dashboard time range to `time>=`/`time<=` constraints in RFC3339 UTC.
- Execute queries in the Go backend, so this datasource supports Grafana alerting.
- Treat ERDDAP's "no matching results" response as an empty result rather than an error.
- Add a "Save & test" health check that verifies the configured URL responds like an ERDDAP server.
