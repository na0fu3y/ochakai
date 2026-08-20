---
type: BigQuery Table
resource: bigquery://bigquery-public-data.thelook_ecommerce.users
title: users
description: 顧客。traffic_source は獲得時のチャネルで、同名の列が events に別の意味で存在する
tags: [customers, marketing, bigquery]
generated: { by: analysis_agent/claude-fable-5, at: 2026-07-25T04:20:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-29T01:10:00Z }
status: stable
---

顧客。1行は会員1人で、[注文](/tables/orders.md)と
[明細](/tables/order-items.md)の両方が `user_id` で指してくる。合成
データなので、名前も email も実在しない。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `traffic_source` | STRING | **獲得時**のチャネル。下の罠を読むこと |
| `created_at` | TIMESTAMP | **会員登録**の時刻であって、初回購入ではない。登録だけして買っていない客がいる |
| `age` / `gender` | INT64 / STRING | デモグラ。合成なので分布はきれいすぎる |
| `country` / `state` / `city` | STRING | 所在地。緯度経度の列もある |

## traffic_source の罠: 同じ名前で別物が二つある

この列は**その客がどこから来たか**(獲得時のチャネル)である。値は
5つ:

| 値 | 意味 |
|---|---|
| `Search` | 検索広告 |
| `Organic` | 自然流入 |
| `Facebook` | Facebook |
| `Display` | ディスプレイ広告 |
| `Email` | メール |

同じ名前の `traffic_source` 列が `events`(行動ログ、カタログ外)にも
あり、そちらは**セッションの流入元**である。**値の集合すら重なって
いない**: events 側は `Email` / `Adwords` / `YouTube` / `Facebook` /
`Organic` で、`Search` と `Display` は無く、代わりに `Adwords` と
`YouTube` がある。同じ列名で同じ「チャネル」を指しているように見えて、
語彙が違う。客は Search で獲得され、その後の注文は Email 経由の
セッションから来る —
どちらの列を JOIN するかで「チャネル別売上」の意味が変わる。
[獲得チャネル別売上](/queries/sales/revenue-by-traffic-source.md)が
draft のまま止まっているのは、この二つのどちらに答えるべきかが
決まっていないからである。

会員登録と初回購入が別である件は、
[リピート購入率](/metrics/repeat-purchase-rate.md)の分母の議論に
直接効いてくる。
