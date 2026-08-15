# Git をナレッジのレビュー経路にする

ナレッジベースは markdown + YAML frontmatter のバンドルとして丸ごと出て、
丸ごと戻る(設計ドキュメント
[0075](../design/0075-the-bundle-is-the-address-space.md) §1)。つまり
**チームがコードに対して既に持っているレビューの仕組みを、そのまま
ナレッジに向けられる** — export した木を Git に置き、変更を PR で出し、
merge してから import する。

このページはその一周の手順と、**Git が引き受けるものと引き受けないもの**
を書く。なぜ取り込みが provenance を読み戻さないかという決定そのものは
[0009](../design/0009-provenance-portability.md) にある。

**多くのチームには要らない。** エージェントが書いた draft を人が裁定する
面は Web UI のレビューキューであり([改善ループ](../loop.md))、それで
足りるならバンドルを Git に置く理由は無い。この運用が効くのは、
**ナレッジの変更にコードと同じレビューを求める**と決めたチーム —
定義の変更に二人目の目を要求したい、変更の理由を PR の議論として残したい、
ナレッジの変更を既存の CODEOWNERS に乗せたい — である。

## 一周

```sh
# 1. サーバーの今を木として取り出す。ここが出発点であって、Git ではない
ochakai export ./knowledge
git add -A knowledge && git commit -m "Sync from ochakai"

# 2. ブランチを切り、変更を書き、PR を出す
git switch -c knowledge/revenue-definition
$EDITOR knowledge/metrics/revenue.md
```

PR の CI では、**取り込みそのものを関門にする**:

```sh
ochakai import ./knowledge --dry-run --strict
```

`--dry-run` は書き込みを止めた取り込みで、`--strict` は「書かれたとおりに
読めなかったら失敗する」姿勢である。二つ合わせると、**本番の import が
出すのと同じ判定**を、何も書かずに得られる — 判定の半分はサーバーの
ものであってバンドルからは見えないので、クライアント側で作り直した
検証では偽グリーンを踏む(設計ドキュメント
[0074](../design/0074-the-document-and-the-vocabulary-that-asks-it.md) §5)。

merge されたら取り込む。実行するのは merge した人か、CI の
サービスアカウントである:

```sh
ochakai import ./knowledge
```

## 往復で何が起きないか

**同じインスタンスへの往復は、変えていない concept を一つも動かさない。**
export された文書に付いている `generated` / `verified` / `created_by` は
このインスタンス自身の観測であって主張ではないので、書き込み経路がそれを
見分けて取り除く(0075 §3.1)。結果として、変えていない concept は
`unchanged` として報告され、リビジョンも `updated_at` も動かない。
バンドルを毎日取り込む cron を置いても、履歴が同じスナップショットで
埋まることはない。

**台帳は diff に出る。ただしレビューの対象ではない。** export はこの
インスタンスの観測を書き戻すので、`generated` / `verified` /
`created_by` / `rejected_*` は .md の中にある — 誰かが `verify` を打てば、
次の export で `verified:` に一行増え、その行は検証した人の identity を
持つ。**PR でそれを書き換えても意味は無い**: 取り込みは読み戻さず、
`received:` の下へ主張として落とすだけである(0075 §3.1)。バンドルが
運ばないのは利用回数・結果報告・リビジョン履歴のほうで、レビューが
裁くのは知識の側だけでよい。

## merge は verify ではない

Git のレビューを通ったことと、このインスタンスが検証を観測したことは
**別の事実**である。PR を merge して import しても、concept の trust tier
は動かない。

frontmatter に `verified:` を書き足しても同じである。他所から来た文書の
trust family は `received:` の下に**主張として**保存され、台帳にも
trust tier にも入らない(0075 §3.1) — 文書には観測者がいないからで
ある。取り込みはそれを黙って落とさず、note として報告する。

検証は製品の裁定面から下る — `ochakai verify <id>`、または Web UI の
「検証」である。Git を経路にするチームは、この二つを**別々に置いたまま
運用する**ことになる: PR が「この文言でよいか」を裁き、`verify` が
「この知識はまだ正しいか」を裁く。後者だけが再検証フィードを空にする
([改善ループ](../loop.md#feeds))。

## サーバーは Git の外でも動く

エージェントは MCP から書き戻す。だから**ナレッジベースの真は常に
サーバーであって、Git のチェックアウトではない**。二つの帰結がある。

- **PR を出す前に export をやり直す。** 古い木の上に書いた PR は、
  その間にエージェントが書いた draft を diff の上では消して見せる。
- **import は消さない。** import はバンドルにあるものを書く操作の列で
  あって、「Git に無いものを消す」操作ではない。木からファイルを消して
  merge しても、サーバーからは消えない — 消すのは `ochakai delete` で
  ある。取り込みが片方向であることは意図した設計で、レビューを通って
  いない削除が、レビュー経路を名乗る仕組みから起きないようにしている。

## CI から取り込むとき

CI で import を実行すると、記録される actor は CI のサービスアカウント
になる。それは**正確な記録**である — そのインスタンスにその知識を入れた
のは、レビューゲートを通した CI だからである。誰の変更だったかは Git の
履歴が持ち、ochakai の provenance は「何が取り込んだか」を持つ。

認証は他のすべてと同じで、置くトークンは無い: Workload Identity
Federation で得た identity がそのまま actor になる
([REST を組み込む](rest-integration.md#認証する)、設計ドキュメント
[0065](../design/0065-identity-and-provenance.md))。誰として話している
かは `ochakai whoami` が言う。

## 別のインスタンスへ渡すとき

これは往復ではなく**移行**であり、持ち帰らないものがある — タイムスタンプ、
リビジョン履歴、利用回数、そして検証済みという状態そのもの。何が失われ、
何を先に決めておくべきかは
[デプロイの運用](operating.md#バンドルからのリストア)に
ある。
