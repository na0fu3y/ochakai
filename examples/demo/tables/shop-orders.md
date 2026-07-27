---
type: BigQuery Table
resource: bigquery://demo-shop.shop.orders
title: shop.orders
description: One row per order, loaded hourly from the storefront database
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-17T03:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-21T01:20:00Z }
status: stable
---

The order table every revenue number in this bundle comes from. One row per
order, never per line item — line items are in `shop.order_items`, which is not
catalogued here. Loaded hourly by a Datastream replica of the storefront
Postgres, partitioned on `created_at` (day) and clustered by `channel_code`.

## Columns worth a note

| Column | Type | Note |
|---|---|---|
| `order_id` | STRING | storefront primary key, stable across the replica |
| `customer_id` | STRING | **null for guest checkout**, about 12% of rows |
| `status` | STRING | the four states in [completed order](/glossary/completed-order.md); a partial return does not get one |
| `total_price` | NUMERIC | tax and shipping included, JPY, never null |
| `channel_code` | STRING | null on ~4% of rows; see [order channel codes](/references/order-channel-codes.md) |
| `created_at` | TIMESTAMP | **UTC**, from the storefront. Truncate in `Asia/Tokyo` or months come out wrong |
| `updated_at` | TIMESTAMP | UTC; a returned order is updated in place, so history is not here |

`updated_at` moving in place is the reason the table cannot answer "what did we
think revenue was last Tuesday" — there is no snapshot. If you need that, the
close in the finance warehouse is the only source.

## Known issue: the 06:00 JST partition

The hourly load occasionally lands the 06:00 JST partition **two to three hours
late**, and a query run in that window returns a day that looks 10-15% short. It
has never lost rows; it has repeatedly caused a false alarm. Check
`MAX(created_at)` before believing a bad recent day — this is step 3 of
[how to read the revenue series](/insights/reading-revenue.md), and it is there
because of this table.

The verified query over it is
[monthly revenue](/queries/sales/monthly-revenue.md).
