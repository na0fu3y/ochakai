---
type: Reference
resource: https://storefront.example.co.jp/docs/enums/channel-code
title: 注文チャネルコード
description: 店舗側の channel_code enum の写し(deprecated — FY2026H2 の新分類に置き換え)
tags: [orders, marketing, enum]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-15T02:00:00Z }
status: deprecated
status_note: 店舗側は FY2026H2 に二階層の分類へ移った。過去の行がこのコードを持ったままなので残してある。
---

`shop.orders` が 2026-06-30 まで書いていた `channel_code` enum の写し。
クエリの結果を、店舗のドキュメントを開かずに読めるようにここへ置いてある。

| コード | 意味 |
|---|---|
| `web` | 店舗サイトへの自然流入・直接訪問 |
| `web_direct` | 同じもの。2024 年の店舗サイトが使っていた名前 |
| `search_ad` | 検索広告 |
| `social` | SNS。広告と自然流入を区別していない |
| `app_ios`, `app_android` | ネイティブアプリ |
| `wholesale` | 法人ポータルからの注文 |
| `NULL` | 主に列ができる前、2024 年より古い注文 |

**deprecated であって、削除ではない。** FY2026H2 の分類はこれを
`source` / `medium` の組に置き換えるが、backfill が走っていないので、
2026-07-01 以降に書かれた注文はこの表にないコードを持っている。
[チャネル別売上](/queries/sales/revenue-by-channel.md)がいまだに draft で
あるのは、この二つが理由である。

`web` と `web_direct` が別のチャネルだったことは一度もない。
[shop.orders](/tables/shop-orders.md)をこの列で集計するなら、まとめること。
