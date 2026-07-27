---
type: Insight
title: How to read the revenue series
description: Baselines, the seasonal shape, the two artifacts that fake a move, and when a drop is worth escalating
tags: [sales, revenue, seasonality, interpretation]
status: stable
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-21T01:00:00Z }
---

[Monthly revenue](/queries/sales/monthly-revenue.md) tells you the number. This
is how to tell whether the number is bad news. Most of what looks alarming in
this series is the calendar, and most of what is genuinely alarming is small.

## Baseline

A normal month in FY2026 runs **42-48M JPY** across about 9,000
[completed orders](/glossary/completed-order.md). Within a month, weekends run
roughly 20% above weekdays, and the 25th — payday for most salaried customers —
is reliably the single biggest day. A month-over-month move inside ±6% is
inside the noise this series has always had; it is not a signal, and reading
one as a signal is the most common mistake made with it.

## Seasonality

The shape repeats every year and is larger than almost anything you will be
asked to investigate:

| Period | Versus a normal month | Why |
|---|---|---|
| December | +30 to +40% | year-end gifting; the peak is Dec 10-20, not the 24th |
| March | +10 to +15% | fiscal year-end, mostly B2B buyers spending budget |
| August | **-15%** | Obon; the whole country stops buying for a week |
| Golden Week | flat | traffic drops, order value rises, and the two cancel |

August being down 15% is the single fact most worth knowing here. It comes back
every year, it has never meant anything, and it generates a review every year
anyway.

## Two artifacts that fake a move

- **A bulk order.** One B2B customer ordering 3-5M JPY at once is a 7-10%
  month. It happens a few times a year, usually in March. Before explaining a
  spike, sort the month's orders by `total_price` and look at the top row.
- **The month boundary.** [Revenue](/metrics/revenue.md) buckets by order
  creation and the accounting close buckets by settlement, so orders placed in
  the last two days of a month land in different months in the two systems.
  A 1-3% gap against finance at month end is expected. A gap that persists
  mid-month is not, and means something else is wrong.

Both are the same mistake in different clothes: the series moved for a reason
that has nothing to do with demand.

## Threshold

Escalate when **two consecutive months are more than 8% below the same months
last year**, after taking the seasonal row above out. That combination has come
up three times since FY2023 and was a real problem all three times. One month
below, however far below, has never been — including the month a payment
provider outage cost a full day.

## When it is down, check in this order

1. Is it August, or the two days either side of a month boundary? Stop there
   most of the time.
2. Did order *count* fall, or only order *value*? Count falling is demand;
   value falling with count flat is usually mix, and
   [revenue by channel](/queries/sales/revenue-by-channel.md) is the next look —
   read its caveats first, it is a draft.
3. Did the pipeline actually load? A late partition in
   [shop.orders](/tables/shop-orders.md) reads exactly like a bad day, and it
   has fooled people more often than any real decline.

Only after those three is a revenue drop a business question.
