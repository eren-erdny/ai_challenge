# TaskDesk Export

TaskDesk exports task lists. The API endpoint is `POST /api/export`.

## Export rules

Free supports CSV and PDF. Pro supports CSV only.
An unsupported format returns HTTP 500.

## Development

Run the regression checks with `node --test test/export.test.js`.
