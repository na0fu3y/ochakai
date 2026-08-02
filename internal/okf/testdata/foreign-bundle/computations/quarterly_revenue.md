---
type: Attested Computation
title: 2026
description: 3.14
status: 3
tags: [1, revenue]
runtime: bigquery
parameters:
  - { name: quarter, type: integer, required: true }
executor:
  resource: ../references/skills/run-on-bq.md
attester:
  resource: ../references/attesters/revenue.py
owner: finance-team
---

# Computation

    SELECT SUM(order_total) AS revenue
    FROM shop.orders_*
    WHERE quarter = @quarter
