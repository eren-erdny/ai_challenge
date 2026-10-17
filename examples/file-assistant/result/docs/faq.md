# Export FAQ

## Which formats are available?

Free includes CSV only. Pro includes CSV and PDF.

## What happens when a format is unavailable?

The API returns HTTP 400 (`UNSUPPORTED_FORMAT`) for an unsupported format, and HTTP 403 (`PLAN_REQUIRED`) when PDF is requested on a plan without access.
