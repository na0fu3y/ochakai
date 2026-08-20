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

顧客である。1行は会員1人で、[注文](/tables/orders.md)と
[明細](/tables/order-items.md)の両方が `user_id` で指してくる。合成
データなので、名前も email も実在しない。

## 注記のある列

| 列 | 型 | 注記 |
|---|---|---|
| `traffic_source` | STRING | 獲得時のチャネル。下の注意点を読むこと |
| `created_at` | TIMESTAMP | 会員登録の時刻であって、初回購入ではない。登録だけして買っていない客がいる |
| `age` / `gender` | INT64 / STRING | デモグラ。合成なので分布はきれいすぎる |
| `country` / `state` / `city` | STRING | 所在地。緯度経度の列もある |

## traffic_source は同じ名前の列が二つある

この列が持つのは、その客がどこから来たか(獲得時のチャネル)である。
値は5つある。

| 値 | 意味 |
|---|---|
| `Search` | 検索広告 |
| `Organic` | 自然流入 |
| `Facebook` | Facebook |
| `Display` | ディスプレイ広告 |
| `Email` | メール |

同じ名前の `traffic_source` 列が `events`(行動ログ、カタログ外)にも
あり、そちらはセッションの流入元である。**値の集合も重なっていない。**
events 側は `Email` / `Adwords` / `YouTube` / `Facebook` / `Organic`
で、`Search` と `Display` は無く、代わりに `Adwords` と `YouTube` が
ある。同じ列名で同じ「チャネル」を指しているように見えて、語彙が違う。

客は Search で獲得され、その後の注文は Email 経由のセッションから来る、
ということが起きる。どちらの列を JOIN するかで「チャネル別売上」の
意味が変わり、
[獲得チャネル別売上](/queries/sales/revenue-by-traffic-source.md)が
draft のまま止まっているのも、二つのどちらに答えるべきかが決まって
いないからである。

会員登録と初回購入が別である点は、
[リピート購入率](/metrics/repeat-purchase-rate.md)の分母の議論に直接
効いてくる。
