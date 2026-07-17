---
type: Reference
resource: https://example.com/metrics/aov
title: Average Order Value
description: The average revenue per order.
tags:
- metric
timestamp: '2026-05-28T22:51:43+00:00'
owner: analytics-team
---

The average revenue per order.

```sql
SUM(order_total) / COUNT(DISTINCT order_id)
```

# Citations
- https://example.com/metrics/aov
