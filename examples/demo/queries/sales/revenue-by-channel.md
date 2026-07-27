---
type: Attested Computation
title: Revenue by acquisition channel
description: Monthly revenue split by the channel the order came through (draft — the channel codes are mid-migration)
tags: [sales, revenue, marketing, bigquery]
status: draft
stale_after: "2026-06-30"
runtime: bigquery
parameters:
  - { name: from_date, type: DATE, required: true }
  - { name: to_date, type: DATE, required: true }
question: Which channel is revenue growing in?
---

The same [revenue](/metrics/revenue.md) as
[monthly revenue](/queries/sales/monthly-revenue.md), split by
`orders.channel_code`. Written by an agent during a campaign review and left as
a draft, because the split is only as good as the codes.

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  o.channel_code,
  SUM(o.total_price)              AS revenue
FROM `demo-shop.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= @from_date
  AND o.created_at <  @to_date
GROUP BY month, o.channel_code
ORDER BY month, revenue DESC
```

# Why this is not verified

- `channel_code` is null for about 4% of orders, mostly older ones, and those
  rows silently form their own group. The totals still add up to
  [monthly revenue](/queries/sales/monthly-revenue.md); the split does not tell
  you that a twenty-fifth of it is unattributed.
- The codes themselves are being replaced. The list this query's output is read
  against — [order channel codes](/references/order-channel-codes.md) — is
  deprecated, and the storefront has already started writing the new taxonomy
  for some orders. Until the backfill lands, `web` and `web_direct` are the
  same channel under two names.
- `stale_after` was set to the end of the migration window and has passed, so
  this entry shows up in the stale feed. That is the point: nobody has come
  back to it, and the feed is how you find out.
