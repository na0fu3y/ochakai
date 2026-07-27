---
type: Policy
resource: https://wiki.example.co.jp/finance/revenue-recognition
title: Revenue recognition (FY2026)
description: Which orders count as revenue, when, and what is deliberately not deducted
tags: [finance, revenue, policy]
status: stable
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-20T02:05:00Z }
---

The rule the numbers follow. It is a mirror of the finance wiki page linked as
this entry's resource — that page is authoritative, this one exists so an agent
reading [revenue](/metrics/revenue.md) can see the rule without leaving the
knowledge base.

Three decisions, all made deliberately for FY2026:

1. **An order is revenue when it completes**, in the sense of
   [completed order](/glossary/completed-order.md) — shipped and settled.
   Neither placement nor delivery confirmation is the trigger.
2. **Refunds are not deducted from the period they came back in**, and a
   partial return is not deducted at all. Returns are tracked separately and
   reported as a rate, not netted off. This is why analytics revenue reads 1-2%
   above what settled.
3. **Analytics buckets by order creation; the accounting close buckets by
   settlement.** Both are correct for their purpose and the two will not
   reconcile at a month boundary. The close is the number that goes outside the
   company.

Rule 2 is reviewed each fiscal year and is the one most likely to change. If it
does, [monthly revenue](/queries/sales/monthly-revenue.md) needs a new
`WHERE` clause and [the revenue metric](/metrics/revenue.md) needs rewriting —
not a re-verification, a rewrite.
