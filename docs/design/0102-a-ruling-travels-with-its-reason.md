# ochakai 設計ドキュメント 0102: 裁定は理由ごと運ばれる

Status: Accepted(2026-08-15)。
[0055](0055-one-ruling-one-face.md) が一本にまとめた裁定の面と、
[0009](0009-provenance-portability.md) §3.2・
[0043](0043-document-first.md) §3.3 が決めた「rejection はこの
インスタンスの裁定であって bundle が運ぶ主張ではない」を**そのまま**
使う — 変えるのは、その裁定のうち**どこまでが運ばれるか**である。
[0075](0075-the-bundle-is-the-address-space.md) §3.1 の観測と主張の
分離は動かない。
Date: 2026-08-15

## 0. この記録が決めたこと

1. **export の frontmatter に `rejected_note` を出す。**
   `rejected_by` / `rejected_at` の隣、同じ拡張キーの扱いで、
   import は読み戻さない(§2)。
2. **`ochakai get` は裁定を言う** — 誰がいつ、そして**なぜ**。
   stdout は文書のままで、観測は stderr に出す(§3)。
3. **`search` / `list` の結果に却下された concept が混じるとき、
   stderr が一行でそう言う。** 行の形は変えない(§3)。

## 1. 「no の記憶」の、使える方の半分が落ちていた

README は却下の記録をこう書いている —

> Proposals that don't make it are kept as `rejected` with the reason,
> so agents stop re-proposing them — a memory of *no*.

`ochakai reject --note` のヘルプはもっと直接的で、note を
「the next agent reads this before proposing again」と説明している。
その note が実際にどこにあったか:

| 経路 | 却下されたと分かるか | 理由が読めるか |
|---|---|---|
| `ochakai get <id>` | ✗ | ✗ |
| `ochakai search --rejected` | ✗ | ✗ |
| `ochakai context` | ✓ | ✓ |
| `get -json` / REST | ✓ | ✓ |
| MCP `get_concept` | ✓ | ✓ |
| Web UI | ✓ | ✓ |
| export した bundle | ✓(by / at) | ✗ |

**`ochakai get` は、却下された concept をただの draft として印字して
いた。** これは本文のリンクを辿った読み手が着く場所であり、README が
Claude Code への推奨インターフェースだと書いている面である
([0067](0067-four-faces-and-what-they-decline.md) §2: CLI は完全性の面)。
そして bundle は、誰かが却下したことは運ぶのに、**なぜ**を運ばなかった。

裁定の三つの要素のうち、次の書き手が使えるのは理由だけである。
「誰が」と「いつ」は監査の情報で、**「何が変われば次は通るのか」を
言うのは note しかない**。落ちていたのは、いちばん使う半分だった。

## 2. bundle は理由まで運ぶ。読み戻さないのは同じ

`rejected_note` は `rejected_by` / `rejected_at` と同じ ochakai 拡張キー
(SPEC §4.1)であり、扱いも同じである:

- 書き出す。export した bundle を読む人間は、裁定を**全部**読める。
- **読み戻さない。** import は server-owned キーとして受け取らず、
  他所から来た文書がこれを名乗っていれば `received:` の下に主張として
  残す。0009 §3.2 の「bundle が運ぶのはナレッジであって一つの
  インスタンスの判断ではない」は動かない — 動くのは、**この
  インスタンスの判断が bundle の中で完全に読める**ようになったこと
  だけである。

C1(資産は利用者のもの — 丸ごと出て、丸ごと戻る)から見ると、これは
「丸ごと出る」側の穴だった。別のインスタンスに移した人が読めたのは
「誰かが却下した、理由は不明」であり、その状態から復元できる判断は
無い。

## 3. CLI は裁定を言う。行の形は変えない

`ochakai get` は文書を stdout に、観測を stderr に印字する
([0009](0009-provenance-portability.md)、
[0043](0043-document-first.md) §3.5: `ochakai get | … | ochakai put`
が一本のパイプであること)。裁定はこの観測の側であり、`created by …` の
次の行に出る。**stdout は一バイトも変わらない。**

`search` / `list` は行を変えない。列は concept のものであり、裁定は列
ではない — 理由を行に載せるには REST の凍結された応答を太らせることに
なり、しかもランキングの一行に散文を入れることになる
([0084](0084-a-hit-says-why-it-matched.md) が snippet に置いた線の外側で
ある)。代わりに stderr が一行、**どこに理由があるか**を言う。

## 4. 却下した案

- **検索ヒットに note を載せる。** 上のとおり、凍結された応答の行を
  太らせる。裁定は concept に属し、ランキングの行に属さない。
- **`get` の stdout に出す。** 文書の往復が壊れる。観測を文書に混ぜない
  のは 0009 の決定そのものである。
- **`--note` を必須にする。** 理由の無い裁定を禁じるのは筋が通るが、
  それは裁定を記録する行為自体を重くする — レビューの途中で「これは
  違う」と分かった人に、その場で説明を書かせることになる。ここで
  決めているのは**書かれた理由が失われないこと**であって、書かせること
  ではない。

## 5. 表面

新しいコマンド・フラグ・エンドポイント・ツールはどれも増えない。
CLI は既に持っている情報を印字するようになり、export は既に持っている
値を書き出すようになる。増えるのは frontmatter のキー一つで、それは
既にある `rejected_*` の族の三つ目である。
