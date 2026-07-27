---
type: Reference
resource: https://storefront.example.co.jp/docs/enums/channel-code
title: Order channel codes
description: The storefront's channel_code enum, mirrored (deprecated — superseded by the FY2026H2 taxonomy)
tags: [orders, marketing, enum]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-15T02:00:00Z }
status: deprecated
status_note: The storefront moved to a two-level taxonomy in FY2026H2; kept because historical rows still carry these codes.
---

A copy of the `channel_code` enum as `shop.orders` wrote it up to 2026-06-30.
Mirrored here so a query result can be read without opening the storefront
docs.

| Code | Means |
|---|---|
| `web` | organic or direct visit to the storefront |
| `web_direct` | the same thing under the name the 2024 storefront used |
| `search_ad` | paid search |
| `social` | paid and organic social, not distinguished |
| `app_ios`, `app_android` | native apps |
| `wholesale` | B2B portal orders |
| `NULL` | mostly pre-2024 orders, before the column existed |

**Deprecated, not deleted.** The FY2026H2 taxonomy replaces these with a
`source`/`medium` pair, and the backfill has not run, so orders written after
2026-07-01 carry codes that are not in this table. Both facts are why
[revenue by channel](/queries/sales/revenue-by-channel.md) is still a draft.

`web` and `web_direct` were never different channels. Any grouping of
[shop.orders](/tables/shop-orders.md) by this column should coalesce them.
