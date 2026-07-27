---
type: Attested Computation
title: Monthly recognized revenue
description: Monthly revenue from completed orders (example)
status: draft
runtime: bigquery
question: What is our monthly revenue this year?
tags: [sales, revenue]
---

# Computation

```sql
SELECT
  DATE_TRUNC(o.created_at, MONTH) AS month,
  SUM(o.total_price)              AS revenue
FROM `myproject.shop.orders` AS o
WHERE o.status = 'completed'
  AND o.created_at >= DATE_TRUNC(CURRENT_DATE(), YEAR)
GROUP BY month
ORDER BY month
```

# Caveats

- **Completed orders only.** Anything other than `status = 'completed'` —
  cancelled, or held pending a return — is excluded. Use a different query
  if you want a same-day preliminary figure.
- Amounts are JPY, tax included. Refunds are not deducted, so a month with
  a high return rate reads higher than it settled.
- The month boundary is the order creation time (`created_at`, UTC), which
  can be a few hours off from the accounting close.
