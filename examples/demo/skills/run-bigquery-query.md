---
type: Skill
title: BigQuery の計算を実行する
description: このバンドルで runtime bigquery の Attested Computation をどう動かすか、そして実行が何を持ち帰るか
tags: [bigquery, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-07-17T07:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-21T01:40:00Z }
status: stable
---

このバンドルの `runtime: bigquery` の concept が `executor` として名指す
手順である。名指しているのは二つ:
[月次売上](/queries/sales/monthly-revenue.md)と、draft の
[チャネル別売上](/queries/sales/revenue-by-channel.md)。

## 実行する

1. concept の `# Computation` フェンスから SQL を読む。**そのまま** —
   フィルタも LIMIT も列も足さない。書き換えたクエリは別の計算であり、
   attester がそう言う。
2. `parameters` の各項目を、宣言された型のクエリパラメータとして束縛する。
   `revenue-by-channel` は `from_date` と `to_date` を要る。
   `monthly-revenue` は取らず、期間を自分で決める。
3. `demo-shop` プロジェクトで、`analytics-reader` サービスアカウントとして
   実行する。このアカウントはそこで BigQuery Job User を持ち、読めるのは
   `shop` データセットだけである。ロケーションは `asia-northeast1`。
4. 結果と一緒に、下の receipt を返す。

## receipt

どちらの concept も `receipt: [job_id, executed_sql, row_count]` を宣言して
おり、三つとも返ってこなければならない:

| フィールド | どこから来るか |
|---|---|
| `job_id` | BigQuery のジョブ。監査の跡であり、あとから実行を読み直す手段 |
| `executed_sql` | 送られたとおりのクエリ。送るつもりだったものではない |
| `row_count` | 返った行数。0 行は結果であって、失敗ではない |

attester が実際に読むのは `executed_sql` である。SQL を送っておいて別の
ものを報告する — この仕掛け全体が捕まえようとしている失敗がそれである。

## これは何ではないか

計算を実行することも、receipt を確かめることも、クエリを走らせる側の仕事で
ある。ochakai はこの手順と、それを指す契約を保存するだけで、どちらも実行
しない。ナレッジベースの中で SQL は動かない。
