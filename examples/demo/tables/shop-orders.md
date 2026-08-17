---
type: BigQuery Table
resource: bigquery://demo-shop.shop.orders
title: shop.orders
description: 注文1件につき1行。店舗のデータベースから毎時ロードされる
tags: [sales, orders, bigquery]
generated: { by: analysis_agent/gemini-2.5-pro, at: 2026-07-17T03:10:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-21T01:20:00Z }
status: stable
---

このバンドルの売上がすべて出てくる注文テーブル。1行は注文1件であって、
明細1行ではない — 明細は `shop.order_items` にあり、こちらはカタログ化して
いない。店舗の Postgres を Datastream で複製し、毎時ロードしている。
`created_at` で日次パーティション、`channel_code` でクラスタリング。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `order_id` | STRING | 店舗側の主キー。複製をまたいで変わらない |
| `customer_id` | STRING | **ゲスト購入では null**。行の 12% ほど |
| `status` | STRING | [完了した注文](/glossary/completed-order.md)の四つの状態。一部返品には状態がない |
| `total_price` | NUMERIC | 税・送料込み、円。null にはならない |
| `channel_code` | STRING | 4% ほどの行で null。[注文チャネルコード](/references/order-channel-codes.md)を見ること |
| `created_at` | TIMESTAMP | **UTC**、店舗側の値。`Asia/Tokyo` で切らないと月がずれる |
| `updated_at` | TIMESTAMP | UTC。返品された注文は上書きされるので、履歴はここにない |

`updated_at` が上書きされるせいで、「先週の火曜に売上をいくらだと思って
いたか」にこのテーブルは答えられない — スナップショットがない。それが要る
なら、経理側の締めが唯一の出どころである。

## 既知の問題: 06:00 JST のパーティション

毎時のロードが、06:00 JST のパーティションだけ **2〜3時間遅れて**着くこと
がある。その時間帯に走らせたクエリは、1日ぶんが 10〜15% 少なく見える。行が
失われたことはないが、誤報は何度も起こしている。直近の悪い日を信じる前に
`MAX(created_at)` を見ること — [売上の読み方](/insights/reading-revenue.md)
の手順3がこれで、あの手順があるのはこのテーブルのせいである。

このテーブルに対する検証済みのクエリは
[月次売上](/queries/sales/monthly-revenue.md)である。
