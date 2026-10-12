# Webhook processing metrics

`GET /api/webhooks/metrics` returns JSON for the shared PostgreSQL inbox. It
requires an authorized dashboard session outside loopback demo mode, following
the same login redirect behavior as the other APIs. Other methods return 405.
Responses use `Cache-Control: no-store`; a database error or a query exceeding
five seconds returns 503. The endpoint always returns JSON, including HTMX requests.

| Field | Meaning |
| --- | --- |
| `total` | Number of retained handled deliveries, excluding duplicate acknowledgements. |
| `pending` | Deliveries waiting for a claim, including delayed retries. |
| `ready` | Pending deliveries eligible to be claimed now. |
| `delayed` | Pending deliveries with a future retry deadline. |
| `processing` | Deliveries with an active claim, including expired leases. |
| `expired_leases` | Processing deliveries whose lease has expired and can be recovered. |
| `succeeded` | Deliveries successfully completed. |
| `failed` | Terminal failures, including records retained from the earlier inbox version. |
| `attempts` | Total claims, including retries, lease recovery and interrupted work. |
| `handler_failures` | Recorded handler errors, panics and timeouts since retry support was installed. |
| `oldest_pending_seconds` | Age since receipt of the oldest pending delivery, including delayed retries; zero if none. |

All fields describe retained inbox records. Totals persist across process restarts
and are shared across replicas; operator redelivery resets the retry budget but
preserves attempts and cumulative handler failures. Failed rows from before retry
support have no historical failure count. Removing records reduces these totals,
so they are not monotonic Prometheus counters. No automatic retention cleanup is
implemented yet. The endpoint exposes no payloads, delivery IDs or error strings.

## Operational checks

- A growing ready backlog or oldest pending age beyond expected handler and retry
  times can indicate unavailable workers, slow handlers or database contention.
- Delayed work is expected while handlers retry. Compare it with handler failure
  totals and logs before treating it as a stalled queue.
- Expired leases can appear briefly during restart recovery. Persistent expired
  leases suggest workers cannot recover accepted work.
- Investigate terminal failures in operator-controlled database access and logs,
  fix their cause, then redeliver from GitHub. Successful rows remain deduplicated.
- Use `/api/health` for database readiness and this authenticated endpoint for
  processing state. A database outage prevents reading inbox metrics as well.

This is an authenticated JSON endpoint, not a Prometheus scrape endpoint. Existing
session storage and routing requirements apply to any client. Protect database
backups and retained failed payloads as application data. See
[webhook processing and redelivery](WEBHOOKS.md) for the retry policy.
