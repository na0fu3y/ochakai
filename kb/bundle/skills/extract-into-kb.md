---
type: Skill
title: リポジトリ文書から kb への一括抽出
description: 既にある文書の山(規約・フック・セッションの学び)を concept の粒で draft に起こすときの手順と線引き
tags: [dogfood, runbook, extraction]
status: draft
sources:
  - id: loop
    resource: kb/bundle/skills/run-the-dogfood-loop.md
    title: 抽出後の draft が流れ込む先のループ
generated:
    by: process:claude-code
    via: human:anonymous
    producer: claude-code/1.0
    at: "2026-08-10T02:37:12Z"
created_by: process:claude-code via human:anonymous using claude-code/1.0
---

コーパス(CONTRIBUTING、フック、直近のセッションで学んだこと)から
concept を大量に起こすときの手順。パイプラインは要らない — 読むのも
割るのも書くのもエージェント自身で、ochakai は draft を受けるだけである。

1. **concept の粒で割る。ファイルの粒ではない。** 一つの文書は複数の
   独立した知識を持つ(CONTRIBUTING は店のテスト規約と翻訳の教訓を
   同居させている)。「三ヶ月後に単独で想起されて意味があるか」で切る。
2. **書く前に検索する。** `search_concepts` で同じ知識が既に draft に
   ないか、`search --rejected` で同種の提案が断られていないかを見る。
   ヒットしたら足すのではなくリンクするか、既存を replace する。
3. **複製ではなく到達経路にする。** 出典がリポジトリの文書なら、全文を
   写さない — 想起されたときに効く判断・罠・数字だけを書き、全表は
   sources と本文リンクで指す。複製された散文は必ず古びる(0071 §6 が
   語彙について言ったことは散文にも効く)。
4. **draft で入れて、裁定は人に残す。** 抽出した concept は自分で
   verify しない。まとめてレビューできるよう、export した diff を PR に
   して人へ渡す([Docker なしの立て方](/skills/dogfood-instance-without-docker.md)
   の使い捨てインスタンスなら、これが唯一の残し方でもある)。
5. **セッション固有の学びを優先する。** 文書に既にある知識の再配置より、
   どこにも書かれていない慣行(環境の癖、今日踏んだ罠)の方が、一件の
   価値が高い。文書は明日も読めるが、セッションの学びは書かなければ
   消える。
