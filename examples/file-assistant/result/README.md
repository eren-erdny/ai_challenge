# TaskDesk Export

TaskDesk exports task lists. The API endpoint is `POST /api/export`.

## Export rules

Free supports CSV only. Pro supports CSV and PDF.
An unsupported format returns HTTP 400 (`UNSUPPORTED_FORMAT`).
Requesting PDF on a plan without access returns HTTP 403 (`PLAN_REQUIRED`).

## Development

Run the regression checks with `node --test test/export.test.js`.
