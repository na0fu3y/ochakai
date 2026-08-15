---
type: Metric
title: Revenue
description: Tax-included JPY value of completed orders, by order creation date
tags: [sales, revenue, finance]
sources:
  - id: rev-policy
    resource: https://wiki.example.co.jp/finance/revenue-recognition
    title: Revenue Recognition Policy (FY2026)
    author: human:tanaka@example.co.jp
    last_modified: "2026-04-01"
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-18T05:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:15:00Z }
status: stable
grain: order
synonyms: [net sales, top line, 売上]
unit: JPY
---

Revenue is the sum of `total_price` over
[completed orders](/glossary/completed-order.md), bucketed by the order's
creation timestamp. `SUM(total_price)` is the whole of the definition; every
argument about the number is an argument about which rows are in it.

Three things are true of it and are not obvious:

- **Tax included.** `total_price` is the amount the customer paid, consumption
  tax and shipping included. Finance's own revenue line is ex-tax and roughly
  9% lower; the two are not supposed to match.
- **Refunds are not deducted.** A returned order leaves the completed set, but
  a partially returned one does not — see
  [completed order](/glossary/completed-order.md). In a normal month this
  overstates by 1-2%.
- **Bucketed by creation, not by settlement.** The accounting close uses the
  settlement date, so the last two days of a month land differently in the two
  systems.[^rev-policy]

## 呼び名

この数字を社内で指す語は **売上** で、この concept が定義しているのはそれで
ある。紛らわしい語が二つあり、どちらもこの指標ではない:

- **純売上** — 経理が使う税抜きの数字。上の "Tax included" のとおり、この
  指標より約 9% 低い。同じものだと思って突き合わせると必ず合わない。
- **流通総額 / GMV** — キャンセルと返品を含む受注ベースの合計。
  [受注が完了した注文](/glossary/completed-order.md)だけを数えていないので、
  この指標とは別物である。

The verified way to compute it is
[monthly revenue](/queries/sales/monthly-revenue.md). Before reading a movement
in it as good or bad, read
[how to read the revenue series](/insights/reading-revenue.md) — the seasonal
swing is larger than most of the changes anyone investigates — and
[月末の着地見込み](/insights/着地見込み.md) for reading a month before it is
over.

Revenue says nothing about whether the customers came back. That is
[repeat purchase rate](/metrics/repeat-purchase-rate.md), which is still a
draft.

[^rev-policy]: Revenue Recognition Policy (FY2026)
