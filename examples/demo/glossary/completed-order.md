---
type: Glossary Term
title: Completed order
description: The order state every revenue figure here counts, and the states it leaves out
tags: [sales, orders, glossary]
status: stable
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:10:00Z }
---

An order is **completed** when it has shipped and its payment has settled.
In [shop.orders](/tables/shop-orders.md) that is `status = 'completed'`, and
it is the only state [revenue](/metrics/revenue.md) counts.

Three other states exist, and none of them is revenue:

| State | Meaning | Why it is not revenue |
|---|---|---|
| `pending` | placed, payment not settled | may still fail authorization |
| `cancelled` | withdrawn before shipping | nothing was delivered |
| `returned` | shipped, then returned in full | the money went back |

**A partial return is not a state.** An order with one item of three sent back
stays `completed` and keeps its original `total_price`, so an order that shipped
at 10,000 JPY and came back a third returned is still counted whole. That is a
deliberate choice of
[the revenue recognition policy](/policies/revenue-recognition.md), not a bug in
the query, and it is the single biggest reason this number sits above what
finance settled. [How to read the revenue series](/insights/reading-revenue.md)
says how much it moves.

"Completed" is a state of the *order*, not of the shipment. A split shipment
completes once, when the last parcel leaves.
