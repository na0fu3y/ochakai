# ochakai の表面

ochakai を使う人が払うのは実装の行数ではなく**表面**である — 呼べる
エンドポイント、そこに渡すパラメータとヘッダ、エージェントのコンテキスト
を消費するツールスキーマ、覚えなければならないコマンド、設定を間違え
られる環境変数。この文書はその全部を一箇所で数え、**何のための表面か**
を先に決めている。

数を出すだけでなく、上限も置く。**表面が増えたことを diff に出す**のが
下の九つの節で、**増やすと決めたことを diff に出す**のが「上限」の節で
ある。エンドポイントを一本足す変更は
[api/openapi.yaml](../api/openapi.yaml) の数十行の差分に埋もれるが、
ここでは `## REST (14)` が `(15)` に変わる一行として出て、さらに上限の
行を書き換えないと CI が通らない。
`cmd/ochakai/surface_test.go` が九つの節を実物と突き合わせ、
食い違えば落ちる([0035](design/0035-verifiability.md):
規約を信じるのではなく、外から不変条件を読む)。

## 八つの条件

数える前に、**何のための表面かを決めておく**。ochakai が満たそうと
しているのは次の八つで、これで全部である。

| id | 条件 |
|---|---|
| C1 | 資産は利用者のもの — 丸ごと出て、丸ごと戻り、求められれば消える([0009](design/0009-provenance-portability.md)・[0031](design/0031-purge.md)・[0075](design/0075-the-bundle-is-the-address-space.md)) |
| C2 | Google Cloud、secret なし — Cloud Run IAM と Cloud SQL IAM で、トークンもパスワードも置かない([0065](design/0065-identity-and-provenance.md)・[0003](design/0003-gcp-only.md)) |
| C3 | 形式は Open Knowledge Format v0.2 — 保存もワイヤも往復も OKF で、その横に第二の形式を発明しない([0075](design/0075-the-bundle-is-the-address-space.md)) |
| C4 | No FDE — デプロイは自分でできて、必要な操作には自分で打てるコマンドがある([0067](design/0067-four-faces-and-what-they-decline.md)) |
| C5 | Claude Code から使える — MCP over HTTP と、それを話せないクライアントのための stdio 橋([0067](design/0067-four-faces-and-what-they-decline.md) §3) |
| C6 | 利用者が自分の Web サービスに埋められる小さな REST API — OpenAPI 一枚で、クライアントライブラリを要らなくする([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md)・[0067](design/0067-four-faces-and-what-they-decline.md) §1) |
| C7 | 人間の改善ループが測れる — 検証・結果報告・キューの長さ・答えの無かった問いを、推測ではなく数で持つ([0069](design/0069-the-loop-and-what-measures-it.md)) |
| C8 | 日本語話者にとって、類似サービスと比較したときの最適な選択肢の一つであること — trigram 索引は二文字の日本語語を引けず、埋め込みは Google Cloud 上で既定である([0080](design/0080-search-and-how-a-deployment-embeds.md)) |

**どれにも当たらない提案は、三つの問いに進むまでもなく no である。**
逆は成り立たない — 条件に当たることは必要条件であって十分条件では
ない。「C7 に当たる」は足す理由にならず(C7 に当たる機構は無限にある)、
落ちなかったものが次の三つの問いに進む。

八つは互いに独立ではない(C4 は C2 の secret-zero に支えられ、C5 と C6
は同じナレッジを別の口から出す)。それでよい — これは分類ではなく、
**「足さない」と言うための共通の物差し**である。九つめを足すのは製品を
別のものにする決定であり、表面を一つ足すのとは桁が違う。ここに行を
足す PR は、そう扱われる。

## 足す前に答える三つの問い

**既定の答えは no である。** 表面を増やす PR は、説明でこの三つに答える。

1. **誰が、何をしていて詰まったか。** 実際に起きた一件を書く。「あると
   便利」「対称性のため」「他のツールにはある」は理由ではない。
   [ROADMAP](../ROADMAP.md) が断ってきたものは、どれも使われた結果では
   なく想像から始まった機能である。
2. **既存の面で回避できるか。** 回避できるなら足さない。サーバ側の一括
   import を断った理由がこれで、既存のエンドポイントのループで足りる
   ものに二つ目の経路を作ると、買えるのは capability ではなく
   convenience だけだった([0067 §5.2](design/0067-four-faces-and-what-they-decline.md))。
3. **何が畳めるか。** 一つ足すとき、既存のどれが要らなくなるかを探す。
   何も畳めないなら、表面が単調に増えてよい理由を書く — 書けるなら
   足してよい。書けないと気づくことが、この問いの目的である。

面ごとの既定は [0067 §1](design/0067-four-faces-and-what-they-decline.md) が決めて
いる。**REST** は唯一の契約で、すべては `/api/v1` に載るか、どこにも
載らない。**CLI** は完全性の面なので既定は yes。**MCP の既定は no** —
ツールスキーマはエージェントのコンテキストから支払われるので、ツール数
は予算である。**Web UI** は人の curation に要るときだけで、BI ツールで
はない。

## 上限

下の節が数えるのは**今の大きさ**で、それがいくつであっても、文書と
ビルドが一致してさえいれば通る。数は体温計であって、温度調節器では
ない。ここがその調節器である — 面ごとに天井を宣言し、超えたら CI が
落ちる。

- REST: 11
- PARAM: 19
- HEADER: 13
- MCP: 6
- MCP-BYTES: 12000
- MCP-BYTES-SLACK: 500
- CLI: 24
- FLAG: 28
- ENV: 11
- VOCAB: 34
- DOC: 24
- DOC-LINES: 5800
- DOC-LINES-SLACK: 100

`-LINES` と `-BYTES` で終わる行だけは、一覧ではなく**量**に天井を置いて
いる。
`DOC` はページ数を数えるが、ページは太らせても名前が増えないので、
そちらにも天井が要る(DOC の節)。

**天井は一つのファイルの一行で、誰でも上げられる。それは弱点ではなく
狙いである。** この仕組みが買っているのは、上の節と同じく、**黙って
やれなくする**ことだけである。上限を動かす PR は「ochakai を大きく
する」と声に出して言っている PR であり、三つの問いはそのときのために
ある。

**下げるのは日常、上げるのは決定。** 一覧の天井(DOC-LINES を除く上の
九つ)は宣言数と実数が一致することを CI が確認するので、畳み込みで数が
減れば天井を下げないと落ちる — 黙って通る余白は残らない。`DOC-LINES`
だけは行数という量なので、編集のたびに一致させるのは神経質すぎる。
その代わり `DOC-LINES-SLACK` が「実数が天井よりどれだけ少なくてよいか」
を宣言し、それを超えて余白が空けば CI が落ちる。d28c3c8 はガイドを
縮めながら天井を 5,753 → 5,790 に上げ、実際の着地は 5,762 — 28 行分の
予算が返らないまま残った。それを塞ぐのがこの一行である。

**幅は 100 行、つまり天井は実数を切り上げた百の位である**(百の倍数で
あることは CI が確かめる — 実数ちょうどに合わせると余白が無くなり、次の
PR がまたこの行に戻ってくるからで、慣行として書くだけでは実際にそう
なった)。当初は 10 行にしていたが、それだと**マニュアルを触るすべての
PR がこの一行を書き換える** — 並行して進む翻訳が毎回ここで衝突し、
しかも 6 行の変更が天井を動かすなら「上限を動かす PR は声に出して
言っている」という上の段落が意味を失う。100 行をまたぐのは実際に
大きくなった瞬間だけで、そのときの一段落は読む価値がある。上下どちらの
向きにも同じ幅で効く。

## この文書は番号を取らない

設計ドキュメントの番号は取らない。[0048 §3](design/0048-decision-records-for-wire-contracts.md)
が「以後、プロセスについての決定は書かない」と定めており、これはその
規則の下で動く運用であって、規則の変更ではない。増やしたくないものを
数える仕組みが、自分の数え先を増やすのでは筋が通らない。

## REST (11)

- `DELETE /api/v1/bundle/{path}`
- `GET /api/v1/bundle/{path}`
- `GET /api/v1/context`
- `GET /api/v1/search`
- `GET /api/v1/stats`
- `GET /api/v1/usage/{id}`
- `POST /api/v1/move`
- `POST /api/v1/reembed`
- `POST /api/v1/review/{id}`
- `POST /api/v1/usage/{id}`
- `PUT /api/v1/bundle/{path}`

19 → 14 → 11。前半は
[0046 §3.5](design/0046-bundle-address-space.md) の畳み込みで、消えた
のは面であって能力ではなかった — concept の第二の住所
(`/api/v1/knowledge/{id}` 3 操作)、`/api/v1/backlinks/{id}`(検索の
`links_to=` へ)、`/api/v1/export`(バンドルのパスに
`Accept: application/gzip` で、ルートが全体)。一覧は `/api/v1/search`
に改名した。

後半は [0055](design/0055-one-ruling-one-face.md) の 3 操作で、こちらも
能力は落ちていない。`GET /api/v1/queues` は `stats` が同じキーで既に
返していた 3 つの数の第二の住所で、唯一できたことが部分木への絞り込み
だったので、**絞り込みのほうを `stats` に移した** — 結果として、共有
デプロイのチームは自分のキューだけでなく自分の知識も数えられるように
なった(§3.2)。`verify` / `reject` / `reject の DELETE` は
`POST /api/v1/review/{id}` 一本になった。三つは**構造的性質を全て共有
していた** — 台帳に追記する、文書と status と ETag を動かさない、人の
面に載り MCP には載らない — 違うのは記録される値だけで、それは
「パラメータを持つ一つの操作」の定義である(§3.4)。

`report_outcome`(`POST /api/v1/usage/{id}`)はそこに畳んでいない。
人の裁定ではなく機械の観測であり、[0067 §5.4](design/0067-four-faces-and-what-they-decline.md)
が Web UI に載せないと決めている当のものである。数のためだけの畳み込み
は、この文書が数えようとしているものを増やす。

## PARAM (19)

クエリパラメータも表面である。**操作の数だけを数えていた間、圧力は
ここへ逃げていた** — エンドポイントをパラメータや content type に
畳むと操作数は必ず下がり、決して上がらないので、あらゆる畳み込みが
無条件の勝ちに見えた。[0046 §3.5](design/0046-bundle-address-space.md)
の 19 → 14 は 3 本を `links_to=`・`Accept: application/gzip`・パスの
接尾辞へ移したのに、記録に残ったのは消えた 5 本だけである。

数えるのは**名前の異なり数**であって出現回数ではない。`limit` は 5 つの
操作で同じ意味を持ち、覚えるのは一度きりだから、既存の語彙を使い回す
のは無料で、**語を発明することが代金である**。これが持ちたい誘因である。

パス変数(`{path}`・`{id}`)は数えない。操作する対象の住所であって、
その横に置かれた選択肢ではない。

19 → 18 は [0058](design/0058-filters-nobody-arrived-through.md) §2.1 が
`min_score` を廃したぶんである。**畳み込みではなく削除で、能力は落ちて
いる** — この文書に並ぶ他の減り方とは種類が違う。落ちた能力は「較正した
呼び出し元がスコアの床を置く」ことで、較正の手順を製品が説明できず、
同梱の唯一の利用者が既定で切っていたものだった。

18 → 19 は [0061](design/0061-a-dry-run-is-the-write-withheld.md) の
`dry_run` である。**天井を上げる決定**であり、そう言って上げている。
`--dry-run --strict` を CI の関門にしていた利用者が偽グリーンを踏んだ —
関門が通り、本番 import が落ちた — のは、判定の半分がサーバのもので
あってバンドルからは見えないからである。クライアント側で作り直せば
パラメータは増えないが、増えないのは表面だけで、書き込み経路の判断を
第二の実装が推測することになる(0067 §2「CLI は REST の薄いクライアント」)。
ヘッダではなくクエリパラメータなのは、**落とされたヘッダは書き込みに
なる**からである。同じ PR で `Ochakai-Plan` も足しており、HEADER の
天井も上がっている。

19 のまま、PR [#407](https://github.com/na0fu3y/ochakai/issues/407)
(design doc [0064](design/0064-rest-stops-at-api-v1.md)) が `attachments`
を `files` に改めた。下の MCP の `get_attachment` → `get_file` と同じ
改名で、**変わったのは利用者が知る語そのもの**である。

- `budget`
- `cursor`
- `days`
- `dry_run`
- `files`
- `fm.{key}`
- `history`
- `limit`
- `links_to`
- `prefix`
- `purge`
- `q`
- `rejected`
- `sort`
- `source`
- `status`
- `tag`
- `trust`
- `type`

## HEADER (13)

表面が隠れる三つ目の場所。`Ochakai-Read-Only` と `Ochakai-Unchanged` は
クライアントが実装しなければならないプロトコルで、`If-Match` は
[0030](design/0030-optimistic-locking.md) の全部を運んでいる —
どれもエンドポイントの数には現れない。リクエストとレスポンスを分けず、
**ワイヤを流れる名前**で数える。読む人が出会うのはそれだからである。

9 → 10 は [0061](design/0061-a-dry-run-is-the-write-withheld.md) の
`Ochakai-Plan` で、`dry_run` と同じ一つの決定の片割れである。dry run の
答えは created / updated / unchanged の三値であり、`Ochakai-Unchanged`
はそのうちの一つしか言えない。三値を二つのヘッダに割ると、クライアント
は片方しか読まなくなる — だから dry run では `Ochakai-Unchanged` を
出さず、`Ochakai-Plan` が置き換える。

10 のまま、同じ PR [#407](https://github.com/na0fu3y/ochakai/issues/407)
が `Ochakai-On-Behalf-Of` / `Ochakai-Producer` から `X-` 接頭辞を落とし、
他の八本と同じ綴りの型([0064](design/0064-rest-stops-at-api-v1.md))に
した。

10 → 13 は `Cache-Control` / `X-Content-Type-Options` /
`Content-Security-Policy` である。**新しく出すようになったのではなく、
前から出していたのに数えていなかった。** この節は数える元をコードでは
なく `api/openapi.yaml` に置いており(`cmd/ochakai/surface_test.go`)、
サーバが立てて契約が宣言していないヘッダは、**誰も数えていない表面**と
してここを素通りしていた。三本ともファイル読み出しの応答に立っており、
後ろの二本は散文では説明されていたのにヘッダとしては宣言が無かった。
0064 が契約にその宣言を足し、この数がそれに追いついた。

これは PARAM・FLAG・VOCAB がそれぞれ塞いだのと同じ形の死角が、今度は
**数え方そのもの**に見つかった、というだけである。**上げるのは敗北では
なく、仕組みが働いた跡である** — 三本は前から利用者が受け取っていた
もので、増えたのは数であって表面ではない。数えられない表面より、数えて
天井を上げるほうがよい。除外(ENV が OS 定義の変数にしているような)に
しなかったのは、この三本が**ochakai の選択**だからである — `sandbox` を
いつ出すかは 0075 §1 が決めた挙動であり、ブラウザに何を許すかを
クライアントが読む値である。標準で定義されている名前だから数えない、
という線は、ここでは意味を運んでいる値を落とす。

- `Cache-Control`
- `Content-Disposition`
- `Content-Security-Policy`
- `ETag`
- `If-Match`
- `If-None-Match`
- `Ochakai-Note`
- `Ochakai-On-Behalf-Of`
- `Ochakai-Plan`
- `Ochakai-Producer`
- `Ochakai-Read-Only`
- `Ochakai-Unchanged`
- `X-Content-Type-Options`

## MCP (6)

- `get_concept`
- `get_context`
- `get_file`
- `put_concept`
- `report_outcome`
- `search_concepts`

8 → 6 は [0076](design/0076-two-tools-leave-mcp.md) で、`delete_concept`
と `get_concept_usage` を降ろした([0068](design/0068-how-a-face-is-added-and-removed.md)
§3「通行量の無い入口は降ろす」の、パラメータではなくツールへの適用で
ある)。**畳み込みではなく削除である** — 上の REST の 19 → 11 は住所が
減っても能力は一つも落ちなかったが、ここでは落ちる。落ちるのは**この面
からだけ**で、削除は `DELETE /api/v1/bundle/{path}`・`ochakai delete`・
Web UI に、利用回数の合計は `GET /api/v1/usage/{id}`・`ochakai usage`・
Web UI に、いずれもそのまま残る(CLI が完全性の面であるとはこのことで
ある)。**能力を他の面に残さずに降ろすことは、この規則の外である。**

降ろす理由は二本で違う。削除は**裁定**であり、この面は
`POST /api/v1/review/{id}`(verify / reject)を載せていない — 取り消せる
裁定を預けない面が、取り消せないほうを配っていた。利用回数はループの
**人間側**([0069](design/0069-the-loop-and-what-measures-it.md))で、
エージェントが頼ってよいかの判断に要る trust tier と `verified_at` は
検索ヒットと `get_concept` に既に載っている。エージェントが持ち続ける
のは書き込みの側、`report_outcome` である。

8 本のまま、[0057 §0](design/0057-concept-is-the-word-a-reader-meets.md) が 5 本を
改名した — 知識の単位は OKF SPEC §2 の語で **concept** であり、文書が
既にその語で書いていたのにツール名だけが `knowledge` を名乗っていた。
能力も引数も応答も変わらない。

**ここは本数のほかに量も数える。** ツールスキーマはエージェントの
コンテキストから支払われる、というのがこの面の既定を no にしている理由
だが、**支払っているのは本数ではなくバイトである** — クライアントは
ツール一覧と instructions を会話の間ずっと保持する。6 本という予算を
守っている間に、`put_concept` 一本の説明が
[0076](design/0076-two-tools-leave-mcp.md) の降ろした 2 本より大きく
なっていた。PARAM・FLAG・VOCAB が塞いだのと同じ逃げ道である。

上限は `MCP-BYTES`。数えるのは実セッションの `tools/list` が返すバイト
(名前・説明・入力スキーマ)と instructions の和で、16,704 → 11,733 が
最初の削減、能力もツールも落ちていない。残る大きい二つは意図したもの
である — `domain.TypesGuide()` の 1,319 バイトと、`search_concepts` と
`get_context` が同じフィルタの語を別々のスキーマに持つぶん。

8 本のまま、もう一度。PR [#407](https://github.com/na0fu3y/ochakai/issues/407)
(design doc [0064](design/0064-rest-stops-at-api-v1.md)) が
`get_attachment` を `get_file` に改めた — ワイヤの `attachments` が
`files` になったのと同じ改名で、**変わったのは利用者が知る語そのもの**
であって、能力も引数も応答も変わらない。応答の JSON キーも
`attachment` から `file` に変わっている。

## CLI (24)

- `ochakai browse`
- `ochakai completion`
- `ochakai context`
- `ochakai delete`
- `ochakai export`
- `ochakai get`
- `ochakai import`
- `ochakai list`
- `ochakai log`
- `ochakai mcp-stdio`
- `ochakai move`
- `ochakai purge`
- `ochakai put`
- `ochakai reembed`
- `ochakai reject`
- `ochakai report`
- `ochakai revisions`
- `ochakai search`
- `ochakai stats`
- `ochakai ui`
- `ochakai usage`
- `ochakai use`
- `ochakai verify`
- `ochakai whoami`

25 → 26 は [0062](design/0062-a-listing-is-not-a-search.md) の
`ochakai list` である。**天井を上げる決定**であり、そう言って上げて
いる。[0056](design/0056-one-question-one-command.md) は「能力が一つ
ならコマンドも一つ」と決めて第二の住所を畳んだが、逆向き — **一つの
コマンドが二つの能力を持つ** — は書いていなかった。`ochakai search`
がそれで、検索(順位、`--limit` で切れる、ページ二枚目が無い)と一覧
(全順序、`--cursor` で進む、既定 100・上限 1000)を `--sort` の有無で
切り替えていた。両者は [0050](design/0050-listings-page-rankings-do-not.md)
が**別物と決めている**当のものである。

代わりに畳めたものはある。`--sort` はフラグでなくなり(FLAG 29 → 28)、
`search` からは `--cursor` が落ち、**引数の有無でコマンドの意味が
変わる**規則が二つ消えた — クエリ無しの `search` が一覧に化けること、
`--source` / `--links-to` だけを渡すと順位付けをやめること。
そして `search -h` の前置き散文は 30 行から 9 行になった。

26 → 24 は [0077](design/0077-one-address-for-a-file.md) が、退役した語で
綴られていた attach / detach の二本を畳んだぶんである。**能力は落ちて
いない** — バンドルは一つのアドレス空間で、`PUT` / `DELETE` は既に両方の
オブジェクトを取るので([0075](design/0075-the-bundle-is-the-address-space.md)
§§1・4)、二本はファイルを**バンドルパスではなく「concept の id と名前」で
名指す第二の住所**だった。落ちたのは便宜だけである — 一度に複数ファイルは
シェルのループであり、`--name` はパスそのものが名前になる。貼る用の
リンクのヒントは `ochakai put` が引き継いだ: 帰属が本文からの導出に
なった以上(0075 §5)、誰もリンクしないファイルは誰にも見つからない。
`--name` は `ochakai use` にも付いているので FLAG は 28 のままである。

## FLAG (28)

コマンド数を数えることは、**REST で操作数だけを数えていたのと同じ形の
見落とし**だった。PARAM の節が書いているとおり、操作をパラメータに畳めば
操作数は必ず下がるので、そこを数えないかぎり圧力はそちらへ逃げる。CLI は
同じ形をしていて、**フラグは誰も数えていなかった**。

`ochakai search` 一本が 14 個持っていた — REST の契約全体の操作数(11)
より多い。どれも読む人が覚えるものである。

29 → 28 は [0062](design/0062-a-listing-is-not-a-search.md) が `--sort`
を畳んだぶんで、**フラグが `ochakai list` の引数になった**。語彙は減って
いない(VOCAB の `sort.*` 4 語はそのまま — 覚える語は同じで、置き場所
だけが変わった)。`search` は 12 フラグ、`list` は 13 フラグになり、
**どちらの `-h` も一つの仕事しか説明しない**。

短いフラグも数える。`-f` は他と同じく覚える名前であり、既にフラグ集合を
読んでいた箇所(補完スクリプトの検査)が短いものを飛ばしていたのは、
**スクリプトの都合であって利用者が払う額の話ではない**。

PARAM と同じく数えるのは**名前の異なり数**である。`--json` は 14 の
コマンドに、`--url` は 24 のうち 22 に付くが、覚えるのは一度きりなので
1 と数える — 既存のフラグを使い回すのは無料で、語を発明することが代金で
ある。

`serve` / `serve-ui` のフラグは数えない。CLI の節が `serve` を数えないのと
同じ理由で、バイナリの動かし方であって ochakai が知っていることではない。

28 のまま、PR [#407](https://github.com/na0fu3y/ochakai/issues/407) が
`ochakai export` の一本を `no-files` に改めた — PARAM の `attachments`
→ `files` に揃えただけで、ワイヤの外で名前だけ遅れていた。

- `budget`
- `cursor`
- `days`
- `download`
- `dry-run`
- `exit-code`
- `f`
- `fm`
- `if-match`
- `json`
- `limit`
- `links-to`
- `name`
- `no-files`
- `note`
- `once`
- `only-if-new`
- `port`
- `prefix`
- `rejected`
- `source`
- `status`
- `strict`
- `tag`
- `trust`
- `type`
- `url`
- `withdraw`

## ENV (11)

環境変数も表面である — デプロイする人が読み、間違えられる。No FDE(C4)
を掲げる以上、**設定の数は「自分で立ち上げられるか」に直接効く**。
非テストの Go ソースから読み戻すので、`os.Getenv` を一つ足せばここも
動く。**読むのは呼び出しであって接頭辞ではない** — 長らく
`OCHAKAI_*` という規約で代用していたので、Cloud Run が設定し deploy
ガイドが名指しし [configuration.md](configuration.md) が最初から載せて
いた `PORT` が、この節にだけ載っていなかった。OS が定義する変数
(`XDG_CONFIG_HOME`・`AppData`)だけを名指しで除く — どのプログラムの
設定がどこにあるかを言う OS の規約であって、ochakai を設定するために
誰かが置くものではない。

16 → 14 は [0060](design/0060-one-word-for-the-posture.md) の畳み込みで
ある。`OCHAKAI_READ_ONLY` / `OCHAKAI_PUBLIC_READ_ONLY` /
`OCHAKAI_INSECURE_DEV` の 3 つのブールが表していたのは、実際には
**排他的な 4 つの姿勢**(既定 / read-only / public / dev)であり、
`OCHAKAI_MODE` 一語になった。**減ったのは 2 本だけだが、消えたのは
それだけではない** — 起動時に拒否していた組み合わせが 1 つ、黙って
補正していた含意が 2 つ、どれも**書き下せなくなった**ので規則ごと消えた。
この文書が数えているのは名前であって規則ではないから、この行は
16 → 14 としか言わない。

15 → 11 は [0078](design/0078-one-variable-says-how-it-embeds.md) が同じ
形を埋め込みに適用したぶんである。一つの機能に五つの変数
(`OCHAKAI_EMBEDDINGS` / `OCHAKAI_VERTEX_PROJECT` /
`OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` /
`OCHAKAI_EMBEDDING_DIM`)が並び、間違えられる場所が五つあった。残るのは
`OCHAKAI_EMBEDDINGS` 一つで、取るのは未設定 / `on` / `off` と、Vertex AI
のモデル resource name — **プロジェクト・リージョン・モデルを一度に運ぶ
綴りが既に存在していた**ので、三つの変数はそれを分解して持っていただけ
である。次元は消えて戻らない: ベクトルは導出物であって運用者のつまみでは
なく([0080](design/0080-search-and-how-a-deployment-embeds.md) §3)、
モデルごとの定数としてコードに移った。

- `OCHAKAI_DATABASE_URL`
- `OCHAKAI_DB_IAM_AUTH`
- `OCHAKAI_DELEGATING_CALLERS`
- `OCHAKAI_EMBEDDINGS`
- `OCHAKAI_GCS_BUCKET`
- `OCHAKAI_IAP_AUDIENCE`
- `OCHAKAI_MODE`
- `OCHAKAI_PRODUCER`
- `OCHAKAI_RECORD_MISSES`
- `OCHAKAI_URL`
- `PORT`

## VOCAB (34)

八つ目の次元は**語**である。上の七つが数えているのは機構 — 呼べるもの、
設定できるもの — であって、**キュレーターが頭に入れておくもの**は一つも
数えていなかった。型の綴り、ライフサイクル値、信頼の段、一覧モード、
裁定、キューの名前、リビジョンが記録される動詞。どれもドキュメントが
教え、誰かが覚えるものなのに、増やしても上の七つは 1 も動かない。

**圧力はここへ逃げていた。** [0055](design/0055-one-ruling-one-face.md)
は REST 3 操作を 1 本に畳んだが、覚える語は 3 つのままである(操作は
1 つ、値が 3 つ)。[0054](design/0054-concept-is-the-okf-word.md) は
MCP ツール 5 本を改名したが、本数は 8 のままで、**変わったのは利用者が
知る語そのもの**だった。この文書は自分についてそう書いていた —
「数え方を一つ足すたびに、次の逃げ道が一つ見えるようになるだけである」。
これがその次の一つである。

値は `internal/domain` から読む。各面が既にそこから語彙を取っている
ので、**製品が言えるようになった瞬間に数に出る**。表記は
`族.値`(`status.draft`、`sort.failed`)で、理由は二つある — 素の値を
アルファベット順に並べても読めないこと、そして**一つの綴りが二つの族で
別の意味を持つとき、それは覚える語が 2 つある**ということ(`failed` は
一覧モードであり、同時に outcome の値でもある — 報告する行為と、一覧を
求める行為は別である)。

**逆に、一つのものが二つの名前を着ていたときは 1 と数える。**
38 → 36 は [0059](design/0059-a-queue-is-named-by-its-listing.md) が
`queue.reported_wrong` / `queue.past_expiry` を `failed` / `stale_after`
に改名したぶんで、`stats` が数える集合と `sort=` が並べる集合は**同じ
一つ**だから、キューは自分の名前を持たない。ここで減ったのは機構では
なく**覚える語そのもの**である — 数えていなければ、改名は「本数が動か
ない変更」として通っていた。`queue.drafts` だけが残るのは、それが単一
の sort ではないからである(`sort=usage` + `status=draft`)。

型は閉じた集合ではない([0038](design/0038-type-vocabulary-realignment.md):
一行の値ならどれでも型になる)。ここで数えているのは**製品が教える
推奨語彙**であって、書ける値の全体ではない。それでも数えるのは、
9 の綴りを `--type` のヘルプにも MCP スキーマにも
[architecture.md](architecture.md) の表にも書いている以上、それは利用者が
払っているものだからである。`Playbook` と `API Endpoint` は
[0063](design/0063-two-unused-recommended-types-leave.md) がここから外した
— SPEC の例示にはあったが、ochakai 自身の examples にも OKF 公式の
リファレンスバンドルにも一度も書かれていなかった。

- `change.add_file`
- `change.create`
- `change.delete`
- `change.move`
- `change.reject`
- `change.remove_file`
- `change.update`
- `change.verify`
- `change.withdraw`
- `outcome.failed`
- `outcome.worked`
- `queue.drafts`
- `ruling.rejected`
- `ruling.verified`
- `ruling.withdrawn`
- `sort.failed`
- `sort.stale_after`
- `sort.usage`
- `sort.verified_at`
- `status.deprecated`
- `status.draft`
- `status.stable`
- `trust.human-reviewed`
- `trust.machine-confirmed`
- `trust.unverified`
- `type.Attested Computation`
- `type.BigQuery Dataset`
- `type.BigQuery Table`
- `type.Glossary Term`
- `type.Insight`
- `type.Metric`
- `type.Policy`
- `type.Reference`
- `type.Skill`

## DOC (24)

九つ目の次元は**読まされる文書**である。上の八つが数えているのは利用者が
**使うもの** — 呼べる操作、渡す語、覚えるコマンド、設定する変数 — で
あって、それを使えるようになるために**読むもの**は一つも数えていな
かった。下の「数えていないもの」は実装の行数も Web UI も、数えない理由を
書いて挙げている。ドキュメントはどちらの側にも無かった — 除外されていた
のではなく、表面だと思われていなかった。

ページは利用者が払うものである。README は 20 以上の文書へ送り出し、
デプロイする人は
[deploy/cloudrun/README.md](../deploy/cloudrun/README.md) の 517 行を
通る。No FDE(C4)がドキュメントを正当化するが、C4 が求めるのは
**自分で立ち上げられること**であって行数ではない。

**そこは 2026-07 まで 983 行だった**([#355](https://github.com/na0fu3y/ochakai/issues/355))。
private IP・team web UI・hardening checklist・upgrade path の全文を
[docs/guides/operating.md](guides/operating.md) に移し、短い参照だけを
残した。ページは増えず(`DOC` は当時の 25 のまま) — 総行数への効果は下の
天井の節にある。

**そしてここは、どの次元より速く増えてきた。** v0.10.0 から現在まで、
REST は 19 → 11 に減り、非テストの Go は 2.3 倍になったが、このページ群は
2,971 行から、下の天井の節にある `DOC-LINES` に迫るところまで増えた。
畳み込みは本物だったが、**畳んだことの説明が、畳んだものより速く増えて
いた** — 表面を一つ減らす決定が、記録と
二つの索引と英語要約とこの文書の段落を生む。数えていなければ、それは
どの節にも出ない。これは PARAM・FLAG・VOCAB が塞いだのと同じ形の
逃げ道である。

**ここだけは量を数える。** 上の八つは名前を数え、`limit` を五箇所で
使い回すのは無料だった — 覚えるのは一度きりだからである。読む労力は
そう働かない。ページを一枚足すのは決定なので `DOC` に出るが、**既存の
ページを太らせるのは名前を一つも増やさない**ので、総行数にも天井を
置いた(上限の節の `DOC-LINES`)。0059 のキュー改名がこの文書に段落を
足したとき、動いた数は一つも無かった。

**この天井は、置いた後に五度上がり、一度下がっている。**
[0062](design/0062-a-listing-is-not-a-search.md) が `ochakai search` を
二つに割ったとき、[cli.md](cli.md) は 9 つのフィルタを二度書くことに
なった — コマンドを割るのは**参照文の重複を買う**という、それまでどこ
にも出ていなかった代金である。5,700 → 5,760。仕組みが狙っていたのは
まさにこれで、畳み込みの PR が散文を増やしていたことが、初めて数として
出た。[#355](https://github.com/na0fu3y/ochakai/issues/355) の
deploy/cloudrun/README.md 分割は逆方向でも同じ代金を払った — 直前の
フィルタ畳み込み([#352](https://github.com/na0fu3y/ochakai/issues/352))
で 5,760 から 5,753 まで下がっていた天井を、5,790 まで上げている。
その間に天井を動かさなかった PR が本文を伸ばし、天井を数えていなければ
5,790 は静かに 5,808 まで踏み越えられていた。[#369](https://github.com/na0fu3y/ochakai/issues/369)
はそれを数え直した上で、REST を自分のサービスに埋め込む開発者のための
docs/guides/rest-integration.md を一枚足し、deploy/cloudrun/README.md
の §5c(委譲された provenance の全文)をそこへの一段落に畳んだ。ページが
増えたぶんは畳み込みで相殺されず、5,808 から 5,942 まで上げている —
C6 が八つの条件の一つであり、これまで YAML のコメント一つに委ねられて
いたことが、その代金である。

[#374](https://github.com/na0fu3y/ochakai/issues/374) は逆方向をもう一度
たどった。アクセスモデル・secret-zero・read-only/public の姿勢は README・
configuration・architecture・faq・deploy の各ページに同じ話が繰り返し
書かれ、二本のテキストが同じことを言えばどちらが古いか分からなくなる
([faq.md](faq.md) 自身の規則)という代金を払っていた。持ち主を一つに決め、
残りを一行のリンクに畳んで、#374 の削減が出ている。

C8 の翻訳が始まってから、この節は PR ごとの増減を一段落ずつ書いていた
— 5,000 → 5,015、5,015 → 4,980、5,019 → 5,032、5,032 → 5,050。**それを
やめる。** 天井は 100 行単位になり(`DOC-LINES-SLACK: 100`)、動かすのは
その区切りをまたぐときだけである。理由は二つあり、どちらも仕組み自身の
ものである。

一つ。**毎回鳴る警報は何も知らせない。** この節が置いた規則は「上限を
動かす PR は『ochakai を大きくする』と声に出して言っている」だったが、
6 行の翻訳が天井を動かすなら、その声は日常の物音になる。100 行をまたぐ
のは実際に大きくなった瞬間であり、そこで書く一段落は読む価値がある。

二つ。**一行と一段落が、並行作業の唯一の衝突点になっていた。** マニュアル
のどのページを触る PR も `DOC-LINES` の同じ一行と、この節の同じ場所に
段落を足す。翻訳を 9 ページ並行で進めると、9 本が同じ二箇所で毎回
衝突する。100 行単位なら 6 行の変更が区切りをまたぐ確率は 6% で、
段落は区切りをまたいだときだけである。

PR ごとの増減は [CHANGELOG](../CHANGELOG.md) の仕事で、ここではない。
5,200 → 5,300 が、その規則を置いてから最初の区切りまたぎである。またい
だのは一つの PR ではなく**二つの畳み込みが同時に着地したから**で、0076
(MCP 6 本)と 0078(`OCHAKAI_EMBEDDINGS` 一語)がそれぞれ天井を動かさ
ずに数十行ずつ足した。畳み込みが散文を買うことは DOC の節が既に言って
いるが、**別々の PR の散文が合流して境界を越える**のは初めてで、区切り
単位にしたからこそ一度だけ鳴った — 10 行単位なら両方が個別に鳴っていた。

**5,300 → 5,400 は毛色が違う。畳み込みの説明ではなく、出口の案内で
ある。** BigQuery カタログの取り込みは手順も job も attester も揃って
いたのに、README がそれを名指すのは「やらないこと」の表の括弧内一箇所
だけで、deploy ガイドと docs の索引はゼロ件だった — **自分のテーブルを
どう入れるかを探した人が、揃っているものに辿り着けない**。C4(No FDE)
が買っているのは「自分で立ち上げられること」であって、手順が存在する
ことではない。三つのページに入口を足したぶんがこの区切りで、**天井を
上げてよい理由としてはこれが典型**である: 増えたのは畳み込みの弁明では
なく、既にある能力への到達経路である。

**5,400 → 5,500 は、その一つ前と同じ型である(DOC 23 → 24)。**
[0009](design/0009-provenance-portability.md) は「Git はレビュー経路で
ある」を規範化しながら、手順を書く先を #43 の CI レシピに預けており、
#43 は断られた。残ったのは、**手順が「未採択」と表示された記録の中に
しかない**という状態で、ROADMAP はそれを「決めるのではなく書く」仕事
として持ち越していた。畳めたぶんは記録の側にある — 0009 は差し替えで
133 → 121 行になり、design corpus は増えていない。それでもマニュアルは
一枚ぶん増えていて、その一枚は畳み込みの弁明ではなく**既に決まっている
運用への到達経路**である。

**5,500 → 5,600 も到達経路である。** 最初の企業導入が、専用インスタンス
を新設せず既存の Cloud SQL に同居させる経路で詰まった — 手順は二つの
文書に散らばり、private IP のみのインスタンスに要る Direct VPC egress
は、コネクタのタイムアウトからは辿れなかった。機構は既にあり(運用
ガイドの Hardening が全文を持つ)、足したのはデプロイガイドの §2c と
トラブルシューティング一項、つまり症状から既にある答えへの道である。

**5,600 → 5,700 は、手本の置き場所である。** 推奨の 9 型は綴りだけが
四つの面に出ていて、**何を書けばその型になるのか**を言う文は
[architecture.md](architecture.md) の表にしかなかった — concept を書くのは
MCP の `put_concept` と `ochakai put` で、そのどちらも書く前にマニュアルを
開かない。型ごとの一行を `domain.TypesGuide()` 一箇所に置いて両方へ渡し、
`put_concept` に散らばっていた 4 型ぶんの助言(と、そこだけ生き残っていた
退役キー `attrs.question`)はそこへ畳んだ。この節が数えるページで増えたのは
examples/claude-code/CLAUDE.md の 10 行で、**型の一覧そのものはその中から
消えている** — 複製された語彙は古びる、というのが
[0071 §6](design/0071-the-recommended-type-vocabulary.md) の言ったことで
ある。`ochakai put -h` の 25 行はここに出ない: [cli.md](cli.md) はヘルプから
生成される参照であって、書いて太らせられる散文ではない。

**5,700 → 5,800 は、数える次元が十になったぶんである。** MCP の節が
本数のほかにバイトを数え始め(`MCP-BYTES`)、その説明が上の一段落として
入った。天井を上げてよい理由としては、到達経路と同じ型である — 増えた
のは畳み込みの弁明ではなく、**これまでどこにも出ていなかった代金**を
出すようにした説明である。

残すのは**次に効く発見**だけ:

- **翻訳は行数を減らさない。** 日本語は語間に空白を置かないので一行が
  多くを運ぶ、と見込んでいたが、全角文字は同じ 70 桁に半分しか入らない。
  loop.md 114 → 120、configuration.md 75 → 84。ただし
  docs/README.md は 101 → 96 と**減った** — 地の文を積むページは増え、
  リンクの一行要約を並べるナビゲーションページは減る。増減はページの
  形で決まる。返してくれるのは翻訳ではなく畳み込みだけである。
- **翻訳の代金はページの外にも出る。** configuration.md は三つの
  アンカーを九つの文書から指されており、そのすべてに `(Japanese)` を
  足す必要があった。被リンクの無い loop.md では見えなかった代金である。
- **日本語の索引は、英語のページをリンクごとに印さない。** 13 項目中
  10 に印が付き、翻訳が進むたびに剥がすことになる。冒頭の一行で状態を
  言い、終わったら一度で消す。**終わった** — [docs/README.md](README.md)
  の冒頭は、いま進捗ではなく決定を言っている: 英語で残るのは玄関と契約の
  七つで、印が付くのはその七つから日本語のページを指すリンクだけである。
  最後の 6 ページ(faq・positioning・examples の 4 枚)で +33 行、
  一つ目の発見のとおり翻訳は行数を返さなかった。
- **下げ方向のラチェットは実際に働いた。** [#447](https://github.com/na0fu3y/ochakai/issues/447)
  の architecture.md の畳み込みで 46 行返ったとき、天井をそのままにした
  PR は `DOC-LINES-SLACK` を超えて落ちた。規約ではなく CI がそう言った
  ([0035](design/0035-verifiability.md))。100 行単位にしてもこの向きは
  残る — 区切りを下にまたげば、やはり落ちる。

数えないものを決めるのは、数えるものを決めるのと同じだけ決定である。

- **[docs/design](design) の記録。** 利用者ではなく、変えようとする人が
  読む。何が番号を**取る資格**を持つかは
  [0048](design/0048-decision-records-for-wire-contracts.md) が既に
  狭めているが、取った後に**何冊積み上がるか**は別の問いで、これは
  数えていなかった — v0.10.0 の 19 記録から、この文書の
  どの次元よりも速く増えている。総行数の天井は
  [CONTRIBUTING.md](../CONTRIBUTING.md) の
  `RECORD-CORPUS-LINES` に置く(冊数を数える `RECORD-COUNT` もあったが、
  この節が `DOC-LINES` について書いたのと同じ理由 —
  毎回鳴る警報と、並行 PR の唯一の衝突点 — で退役した)。読むのは利用者ではなく変える人で、
  `RECORD-LINES`(一冊の天井)と同じ読者の同じ種類の天井だから、ここに
  十一本目の上限を足すより、その天井の隣に置くほうが筋が通る。
- **OKF ドキュメント。** `examples/demo` の 10 件も
  `examples/bigquery-catalog/bundle` も、ochakai が**保存するもの**で
  あって ochakai についての説明ではない。frontmatter を持つ md は
  知識であり、ここでは数えない。
- **CHANGELOG。** 過去の台帳で、読むのは一エントリである。通して読む
  ものを数えるこの節に、追記だけで伸びるものを混ぜない。
- **CONTRIBUTING・CLAUDE.md・行動規範・`.github`・`.claude`。**
  変える人の面であって、使う人の面ではない。

- `README.md`
- `ROADMAP.md`
- `SECURITY.md`
- `SUPPORT.md`
- `deploy/cloudrun/README.md`
- `deploy/terraform/README.md`
- `docs/README.md`
- `docs/architecture.md`
- `docs/compatibility.md`
- `docs/configuration.md`
- `docs/faq.md`
- `docs/guides/git-review.md`
- `docs/guides/golden-query-canary.md`
- `docs/guides/mcp-clients.md`
- `docs/guides/operating.md`
- `docs/guides/rest-integration.md`
- `docs/guides/troubleshooting.md`
- `docs/loop.md`
- `docs/positioning.md`
- `docs/surface.md`
- `examples/README.md`
- `examples/bigquery-catalog/README.md`
- `examples/claude-code/CLAUDE.md`
- `examples/claude-code/README.md`

## 数えていないもの

- **Web UI。** 自分のアドレス空間を持たず、`/api/v1` の client として
  動く([0072](design/0072-the-web-ui-serves-and-edits-documents.md) §2)ので、増えるとすれば REST
  の節に出る。ページ自身の予算は「ビルドステップなし・フレームワーク
  なし・CDN なし」の一枚という形の制約で、数ではない。
- **`serve` / `serve-ui` / `version` / `help`。** バイナリの動かし方で
  あって、ochakai が知っていることではない。
- **実装の行数、内部パッケージ、依存。** 外から見えないものは、増えても
  使う人は払わない。困るのは維持者だけで、それは別の問題である。
- **生成された参照ページ。** ビルドから生成され、テストが往復を保証し、
  固定ヘッダー以外に手書きの散文がないページ。誰も前から通して読まない
  一方で、正確さに絶対に狂いが出ない — [docs/cli.md](cli.md) が今日
  唯一これに当たる(`TestCLIReferenceIsCurrent`、
  [#371](https://github.com/na0fu3y/ochakai/issues/371))。基準は三条件
  だけで、狭く保つのがこの箇条書き自体の目的である。
- **`deploy/terraform` の変数。** このモジュールは Cloud Run + Cloud SQL
  という一つのホスティング経路の上に立つ任意の便宜レイヤーで、
  [deploy/terraform/README.md](../deploy/terraform/README.md) 自身が
  「gcloud ガイドは引き続きリファレンスであり正とする情報源」であり
  二つが食い違えばこちらにバグがあると宣言している —
  [0067 §1](design/0067-four-faces-and-what-they-decline.md)
  が既定を決めている REST・CLI・MCP・Web UI の四つの面のどれでもない。
  ochakai 自身に届く変数(`enable_gcs_files` → `OCHAKAI_GCS_BUCKET`、
  `read_only` / `public_read_only` → `OCHAKAI_MODE`)は、ENV が既に
  数えている一語が IaC の綴りを着ているだけであり、二度数えれば VOCAB
  が退けた「一つのものが二つの名前」を今度は次元をまたいで踏む。届かない
  変数(`region`・`image_tag`・`database_backups`・`maintenance_users`
  など)は Cloud SQL・Cloud Run・GCS 側の資源の選択であって、`serve` /
  `serve-ui` の引数を数えないのと同じ理由で ochakai が知っていることでは
  ない。読む代金は `deploy/terraform/README.md` として DOC が既に数えて
  いる — 数えていないのは HCL の変数名で、それはこのモジュールを使わない
  という選択で丸ごと避けられる代金だからである。

## この仕組みが持てないもの

テストが読めるのは数だけで、**足したものがその場所に値したかは読め
ない**。三つの問いに答えたふりをして節を書き換えるのは何秒もかからず、
CI は通る。この仕組みが買っているのは、それを**気づかないままやれなく
する**ことだけである。判断はレビューに残り、理由は PR に残る。

数える次元を増やしても、それは変わらない。PARAM と HEADER が塞いだのは
「操作を畳めば無条件に得」という**特定の逃げ道**であって、逃げ道一般では
ない。VOCAB が塞いだのも一つで、「機構を畳んで語を増やせば得」という
逃げ道である — `sort=` の 4 つのモードは `sort` 一語で 1 と数えられて
いたが、いま `sort.*` として 4 語に数えられる。

DOC が塞いだのはさらに一つで、これは他とは向きが違う —
**「表面を畳んで、畳んだ説明を書けば得」**という逃げ道である。上の八つ
だけを見ていると、面を一つ減らす PR は必ず勝ちに見える。その PR が記録と
索引と要約と段落を残していくことは、どの数にも出なかった。減った面と
増えた行が同じ表に並ぶのは、この節が足されて初めてである。

FLAG が塞いだのはもう一つで、「REST を畳んで CLI に逃がせば得」という
逃げ道である。[0075](design/0075-the-bundle-is-the-address-space.md) と
[0068](design/0068-how-a-face-is-added-and-removed.md) の畳み込みで REST と CLI
のコマンドは減ったが、**その間 CLI のフラグは一度も数えられていな
かった** — 数え始めた時点で 29 で、うち 14 が `ochakai search` 一本に
付いている。

**そして次の逃げ道が見えている。** 4 つのモードは 4 語と数えられるように
なったが、それぞれが別の limit 既定値と別の cursor 可否を持つことは
まだどこにも数えられていない。`GET /api/v1/bundle/{path}` は Accept と
パスの接尾辞で 6 通りの別物を返して 1 と数えられる。エラーの文言も、
`ochakai://` という URI 形式も、`human:` / `process:` という actor の
綴りも数えていない。**数え方を一つ足すたびに、次の逃げ道が一つ見える
ようになるだけである。** それでよい。これは判断を置き換える装置では
なく、判断すべき瞬間を見えるようにする装置である。
