---
type: Skill
title: BigQuery の計算を実行する
description: このバンドルで runtime bigquery の Attested Computation をどう動かすか、そして実行が何を持ち帰るか
tags: [bigquery, executor, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-07-24T07:40:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-07-28T01:40:00Z }
status: stable
---

このバンドルの `runtime: bigquery` の concept が `executor` として名指す
手順である。名指しているのは
[月次売上](/queries/sales/monthly-revenue.md)、deprecated の
[月次受注額](/queries/sales/monthly-bookings.md)、draft の
[獲得チャネル別売上](/queries/sales/revenue-by-traffic-source.md)の三つ。
アクションについては[アクションの実行](/skills/run-an-action.md)が、この
手順の上に段を足す。

## 実行する

1. concept の `# Computation` フェンスから SQL を読む。**そのまま実行し、**
   フィルタも LIMIT も列も足さない。書き換えたクエリは別の計算であり、
   attester がそう判定する。
2. `parameters` の各項目を、宣言された型のクエリパラメータとして束縛する。
   `revenue-by-traffic-source` は `from_date` と `to_date` が要る。
   `monthly-revenue` は取らない。
3. 自分の Google Cloud プロジェクトで実行する。
   [thelook_ecommerce](/references/thelook-dataset.md) は公開データセット
   なので読む許可は要らないが、課金は実行側のプロジェクトに付く。
   ロケーションは `US` である。
4. 結果と一緒に、下の receipt を返す。

## receipt

どの concept も `receipt` に最低この三つを宣言していて、返らなければ
ならない。

| フィールド | どこから来るか |
|---|---|
| `job_id` | BigQuery のジョブ。監査の跡であり、あとから実行を読み直す手段 |
| `executed_sql` | 送られたとおりのクエリ。送るつもりだったものではない |
| `row_count` | 返った行数。0 行は結果であって、失敗ではない |

attester が実際に読むのは `executed_sql` である。SQL を送っておいて別の
ものを報告する、という失敗をこの仕掛けは捕まえようとしている。この
データセットでは receipt にもう一つ役割がある。数字が日々動くので、
job_id が「その日その数字だった」ことの唯一の証拠になる。

## ochakai の側でやらないこと

計算を実行することも、receipt を確かめることも、クエリを走らせる側(この
バンドルの想定では Claude Code)の仕事である。ochakai はこの手順と、それを
指す契約を保存するだけで、どちらも実行しない。ナレッジベースの中で SQL は
動かない。
