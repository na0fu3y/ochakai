# ochakai 設計ドキュメント 0105: concept は書き手が与えた別名にも答える

Status: Accepted(2026-08-15)。
[0080](0080-search-and-how-a-deployment-embeds.md) の lexical 索引が読む
範囲を広げる(id・title・description・tags・body・名前空間内のファイル名に、
`synonyms` を加える)。
[0043](0043-document-first.md) §3.6 の「producer のキーは書かれたまま保つ」は
動かない — **読むことと所有することは別**である(§4)。
[0071](0071-the-recommended-type-vocabulary.md) が `Metric` を
「定義と同義語」と説明していることの、実装側の欠落を埋める。
Date: 2026-08-15

## 0. この記録が決めたこと

1. **`synonyms` を lexical haystack に入れる。** 配列でも単一の文字列でも
   読む(§2)。
2. **他の producer キーは入れない。** 名前でないものは索引しない(§3)。
3. **キーは producer のものであり続ける。** 検証も、ワイヤのフィールドも、
   専用のフィルタも増やさない(§4)。

## 1. 製品が書けと言い、製品が見つけられなかった

型語彙は `Metric` をこう説明している — CLI のヘルプにも、MCP のスキーマ
にも、[architecture.md](../architecture.md) の表にも、同じ一文が出る:

> what a number means and what it is called — **its definition and
> synonyms**, one concept per metric

`examples/demo` はそのとおりに書いている:

```yaml
synonyms: [net sales, top line, 売上]
```

そして `ochakai search 売上` も `ochakai search "net sales"` も、この
metric を**返さなかった**。haystack は id・title・description・tags・body
と名前空間の中のファイル名だけで(移行 `0016_search_text` 以来、
`0029` と `0033` が触れた形)、producer のキーは `attrs` に入り、そこは
誰も読んでいなかった。

**日本語でいちばん痛い。** 倉庫の列は `revenue` で、会議で出る語は
`売上` である。その二つを結ぶために書く場所として `synonyms` は最も自然
で、書いても引けなかった。条件 C8(日本語話者にとって最適な選択肢の一つ)
から見て、これは索引の性能の問題ではなく**索引が読んでいない**問題だった。

## 2. どこで足すか

トリガが**ファイル名を足しているのと同じ場所**で足す(移行 0040)。
`ochakai_search_text` の引数はエントリ自身の列で、`attrs` はファイルが
持たない列なので、関数の形は変えない。

受ける形は二つ: 配列(examples の形、複数ある人が書く形)と、単一の
文字列(別名が一つの人が書く形)。それ以外 — 数値、オブジェクト、null —
には索引すべき名前が無いので、黙って空になる。**書き方を間違いだと言って
拒む**のは、誰にも伝えていない規則を後から適用することになる。

## 3. なぜ `synonyms` だけか

他の producer キーは、concept の**別名**ではなく concept **について**の
値である — モデル名、receipt の項目、executor の resource、別の concept
が持つ id。それらを haystack に入れると、**あるコンセプトを検索したときに、
それを参照しているだけのコンセプトが返る**。それは
`--links-to` と `--source` が正しく答える問い
([0075](0075-the-bundle-is-the-address-space.md) §6、
[0096](0096-a-listing-is-not-a-search-here-either.md))であって、
全文検索がついでに答えてよい問いではない。

`synonyms` は、**存在理由そのものが「検索されること」である唯一の attr**
である。だから一語だけを名指す。

## 4. 読むが、所有しない

`synonyms` は OKF が定義するキーではなく、ochakai が定義するキーにもしない。
frontmatter の構造体にフィールドを足さず、`attrs` に入ったまま、書かれた
とおりに round trip する。ochakai がすることは**索引に写す**ことだけで、
値の形を検証せず、ワイヤに専用のフィールドを作らず、`--synonym` のような
フィルタも足さない。

これは 0043 §3.6 の延長であって例外ではない。「書かれたまま保つ」は、
**読んではいけない**とは言っていない — body や title を索引するのと同じ
ことを、writer が名前だと宣言したテキストにもする、というだけである。

## 5. 却下した案

- **`attrs` を丸ごと索引する。** §3 のとおり、参照が一致として返り始める。
- **`synonyms` を第一級の frontmatter キーにする。** 形式を一つ発明する
  ことになり、条件 C3(OKF の横に第二の形式を作らない)に触る。読むだけ
  なら発明は要らない。
- **索引せず、型の説明から「同義語」を外す。** 約束の側を削る案。書ける
  場所が無くなるのではなく、**既に書いた人の記述が無駄になる**ので採らない。
- **別名は `tags` に書けと案内する。** tags は主題のラベルであって名前では
  なく、しかも既存のバンドルを書き換えさせることになる。

## 6. 表面と互換性

新しいエンドポイント・ツール・コマンド・フラグ・環境変数はどれも増えない。
移行 0040 が既存の haystack を一度だけ再計算するので、運用者の操作も要らない。

**検索結果は変わる** — それがこの記録の目的である。変わり方は
`internal/service` の検索評価ハーネスが数で持つ(recall@10 と MRR)。
`ochakai search "net sales"` が demo の metric を返すことは、その golden
set の一件として入る。
