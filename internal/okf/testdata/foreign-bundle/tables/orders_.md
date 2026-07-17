---
type: BigQuery Table
resource: https://bigquery.googleapis.com/v2/projects/demo/datasets/shop/tables/orders_*
title: Orders table
description: Sharded orders export.
tags:
- orders
- BigQuery
timestamp: '2026-05-28T22:53:05+00:00'
---

# Overview
The `orders_YYYYMMDD` sharded table.

# Metrics
- [Average Order Value](../references/metrics/avg_order_value.md) — The average revenue per order.

# Schema
- `order_id` (STRING): Unique order identifier.
- `order_total` (NUMERIC): Order total in JPY.
