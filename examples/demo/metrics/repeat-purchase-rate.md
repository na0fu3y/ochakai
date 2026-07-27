---
type: Metric
title: Repeat purchase rate
description: Share of a month's buyers who had bought at least once before (draft — the lookback is not settled)
tags: [sales, retention, customers]
status: draft
grain: customer-month
unit: ratio
---

Of the customers with a [completed order](/glossary/completed-order.md) in a
month, the share that had a completed order at any earlier date. Drafted by an
agent while looking into why [revenue](/metrics/revenue.md) held up in a month
with fewer new customers.

**Not verified, and here is what is unsettled.** Do not quote this number in a
report until somebody decides:

1. **The lookback.** "At any earlier date" makes the rate rise forever as the
   store ages. A 365-day lookback is the usual choice and would make FY2025 and
   FY2026 comparable; it would also cut the current figure by about a third.
2. **Guest checkout.** Guests have no customer id, so every guest purchase
   looks like a first purchase. Guests are roughly 12% of orders, which is
   enough to matter.
3. **The denominator.** Buyers, not orders — so a customer who ordered four
   times in a month counts once. That part is not controversial, but it is
   worth writing down because the obvious SQL gets it wrong.

There is no verified computation for this yet. The draft
[revenue by channel](/queries/sales/revenue-by-channel.md) is a closer look at
the same question from the acquisition side.
