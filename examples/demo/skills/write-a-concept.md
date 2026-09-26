---
type: Skill
title: このベースでの concept の書き方
description: どの型をどこに置き、何の節を持たせるか。そして最初の数枚をどこから書き始めるか
tags: [conventions, runbook]
generated: { by: human:sato@example.co.jp, at: 2026-08-10T02:00:00Z }
verified:
  - { by: human:tanaka@example.co.jp, at: 2026-08-12T01:00:00Z }
status: stable
---

このベースに concept を足すときの決めごとである。型は OKF のサンプルの
バンドルと同じものを使い、独自の型は作らない。

| 書くもの | `type` | 置き場所 | 持たせる節 |
|---|---|---|---|
| データセット | `BigQuery Dataset` | `datasets/` | `# Tables`、全体に効く注意 |
| テーブル | `BigQuery Table` | `tables/` | `# Schema`、`# Joins`、`# Metrics` |
| 数字の定義 | `Metric` | `metrics/` | 定義、分解、読み違えやすい点 |
| 承認された計算 | `Attested Computation` | `computations/` | `# Computation`、読み方 |
| 行動 | `Attested Computation` | `actions/` | `# Computation`、検証、副作用の境界 |
| 知見 | `Insight` | `insights/`(診断は `insights/cases/`) | 分かったこと、根拠、次に見る先 |
| 規則 | `Policy` | `policies/` | 規則そのもの |
| 手順 | `Skill` | `skills/` | 手順、やらないこと |

## 関係はリンクで書く

ochakai が辺として数えるのは本文の markdown リンクだけである。
frontmatter にキーを足しても `linked_from` には現れない。

- **結合**は、テーブルの `# Joins` 節に相手のテーブルへのリンクと `ON` の
  条件を一行で書く。
- **結合について分かったこと**、つまり名前どおりに結べない・キーが無い
  のに結べる、といった知見は Insight にし、`# Joins` からリンクする。
- **指標どうしの因果**は、指標の本文の「分解」と、それを読む Insight に
  書く。どの指標が沈んだら次に何を見るかは、表にして Insight に置く。

## 最初の数枚

全部そろえてから始める必要は無い。このバンドルも、最初は次の五枚から
育った。

1. データセット 1 枚。何が 1 行か、何に気をつけるか。
2. 一番使うテーブル 1 枚。`# Schema` は、つまずいた列の注記だけでよい。
3. 一番訊かれる指標 1 枚。定義と、何で揉めるか。
4. その指標の承認された計算 1 枚。
5. その指標の読み方の Insight 1 枚。

あとは、エージェントが答えられなかった問いと、答えを間違えた問いから
足していく。行き止まった問いは[不足しているデータ](/insights/missing-data.md)
に、診断の結果は `insights/cases/` に draft で書き戻され、人が verify
したものから信用されていく。
