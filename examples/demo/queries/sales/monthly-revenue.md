---
type: Attested Computation
title: Monthly revenue
description: Revenue by calendar month for the current fiscal year
tags: [sales, revenue, bigquery]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: Revenue Recognition Policy (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-04-01"
usage_window: { from: "2026-06-01", to: "2026-06-30" }
status: stable
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:30:00Z }
stale_after: "2026-12-31"
runtime: bigquery
question: What is our monthly revenue this year?
---

The sanctioned way to compute [revenue](/metrics/revenue.md) per month. It
answers the question above verbatim; anything that reads a monthly revenue
number off a dashboard should be able to reproduce this.

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  SUM(o.total_price)              AS revenue,
  COUNT(*)                        AS orders
FROM `demo-shop.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= DATE_TRUNC(CURRENT_DATE('Asia/Tokyo'), YEAR)
GROUP BY month
ORDER BY month
```

# Notes

- `status = 'completed'` is the definition in
  [completed order](/glossary/completed-order.md), and the reason this query
  cannot be simplified to "all orders".
- `created_at` is UTC in the table but the truncation is done in
  `Asia/Tokyo`, so a 08:30 JST order on the first of the month lands in the
  right month. [shop.orders](/tables/shop-orders.md) explains why the column is
  UTC.
- Refunds are not deducted, per
  [the revenue recognition policy](/policies/revenue-recognition.md).
- The output is one row per month with a hole where a month had no orders —
  there is no calendar join. Nobody has needed one yet.

Read the result with
[how to read the revenue series](/insights/reading-revenue.md) before calling a
month good or bad. `stale_after` is the end of the fiscal year because the
`shop.orders` partitioning changes then; re-check it rather than trusting it in
January.
