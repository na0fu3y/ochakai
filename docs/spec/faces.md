# 面の配分(REST / MCP / CLI / Web UI)

同じナレッジを、**どの面からどう出し、どの面に何を載せないか**。何が数え
られているかは [surface.md](../surface.md) が持ち、ここは各面の役割と規則を
持つ。

## 四つの面の役割

- **REST は唯一の契約である。** すべての機能は `/api/v1` に載るか、どこにも
  載らない。他の三面はそのクライアントで、上限・enum・既定値はサーバー側に
  一つだけある。契約は [api/openapi.yaml](../../api/openapi.yaml) 一枚で、
  凍結の範囲は [compatibility.md](../compatibility.md) が持つ。
- **MCP はエージェントの目的特化入口である。** ツールスキーマと instructions
  はエージェントが毎ターン払うので、**ツール数は予算**である。REST の
  1:1 のミラーは目指さない。
- **CLI は完全性の面である。** REST の純クライアントとして能力を覆う。
  完全性とは**能力**の完全性であって、畳まれた面をコマンドとして残すことでは
  ない。
- **Web UI はループの人間側である。** 検索・ブラウズ・裁定・履歴・書くこと、
  そしてデプロイ自身のエージェントに問うこと。**BI ツールではない** —
  ダッシュボードを持たない。ページは認証を知らず、自分では何も判断しない。

**デプロイ自身のエージェントは REST の一本(`POST /api/v1/agent`)の後ろに
いる**([architecture.md](architecture.md))。Web UI はそのクライアントで、
どの面から問うても同じ操作の答えである。

## 足す規則と降ろす規則

- **能力とコマンドは一対一である。** 能力が一つならコマンドも一つ(二本目が
  買うのは convenience だけ)、能力が二つならコマンドも二つ(一本に押し込むと
  規則がフラグのヘルプに移るだけ)。
- **通行量の無い入口は降ろす。** 最初から誰も通らなかった入口は、能力が
  落ちてもそう言って降ろす(例: スコアの床 `min_score`、MCP の `fm.`)。
- **能力を MCP から降ろしてよいのは、それが他の面に残るときだけ**である。
- 面を足す PR は [surface.md](../surface.md) の三つの問いに答え、どの条件
  (C1–C8)に当たるかを言う。既定の答えは no である。
- 0.x の改名と削除は別名で橋渡しせず、CHANGELOG が **BREAKING** と印す
  (設計記録 [0152](../design/0152-a-rename-at-0x-is-announced-not-bridged.md))。

## 同じ機能は、どの面でも同じ意味を持つ

- **エージェントの読みは search → get である。** 検索 + 全文 + リンク + バイト
  予算を一回で返す pack はどの面にも無い。何を読むかを決めるのは読む側で
  ある。
- **検索の hits は順位**で、ポインタである([search.md](search.md))。
- **単読は `linked_from` を運ぶ。** REST の単読・`get_concept`・`ochakai get`
  に、その concept を本文から指している concept の**行**(id・type・title・
  description・status・trust)が付く。住所順、20 件で静かに切れ、却下
  (削除)された linker は出ない。行はポインタであって配達ではない。完全で
  ページングできる逆引きは `links_to=` である。
- **一覧は検索ではない。**

  | | 検索(順位) | 一覧 |
  |---|---|---|
  | ワイヤ | `GET /api/v1/search?q=` | `q` の無い `GET /api/v1/search`(`sort=` のフィード、`source=`・`links_to=` の逆引き、住所順) |
  | `limit` 既定 / 上限 | 10 / 50 | 100 / 1000 |
  | `cursor` | 400 | keyset で進む |
  | CLI | `ochakai search <query>` | `ochakai list [feed]` |
  | MCP | `search_concepts` | `list_concepts` |

  ワイヤは一本、人とエージェントが打つ口は二本である。カーソルは不透明で
  署名しない。フィルタはカーソルに入らないので次のページにも同じフィルタを
  添える。総件数は返さない(`cursor` の不在が終わり)。カーソルは
  スナップショットではない。
- **裁定は一つの面から、一つの語で下す。** `POST /api/v1/review/{id}` が
  `{"ruling": "verified"}` を記録する(CLI `ochakai verify`、Web UI の確認)。
  **却下は削除である** — `DELETE` に `note` を添えると、理由がリビジョンと
  `log.md` に載る(CLI `ochakai delete --note`)。裁定は文書にも ETag にも
  触れない。
- **書き込みは何をしたかを本文で言う。** `plan: created | updated |
  unchanged`(ヘッダ `Ochakai-Plan` と同じ三語)。MCP にはヘッダが無いので
  本文が要る。

## MCP

**ツールは 6 本**: `search_concepts`・`list_concepts`・`get_concept`・
`get_file`・`put_concept`・`report_outcome`。

- **一つの concept を名指す引数は、どのツールでも `id`** である
  (`get_concept`・`put_concept`・`report_outcome`)。ファイルは `path`。
  綴りがツールごとに違えば、エージェントは一度間違えてから読み直す。

- **答えは text のコンテントブロック一つで返る。** `outputSchema` を宣言せず、
  同じ JSON を `structuredContent` に二重に載せない。常駐の予算
  (`MCP-BYTES`)は wire が運ぶスキーマを入力・出力の両側とも数える。
- **書き戻しの hint は `get_concept` の応答に乗る**(常駐ではなく、知識が
  配達される場所に)。
- **`/mcp` は stateless の streamable HTTP**(プロトコル 2026-07-28)で、
  `Mcp-Session-Id` を発行も参照もせず、GET と DELETE は 405。一回の POST が
  自分の名乗りと能力を運ぶ。producer は呼び出しごとに読む。
- 構築時に決まる三つの一覧(`tools/list`・`resources/list`・
  `resources/templates/list`)は **5 分持つ**と答え、`resources/read` は
  0 のままである。
- **`ochakai mcp-stdio`** は stdin/stdout と remote の `/mcp` を JSON-RPC の
  メッセージ単位で素通しする橋で、面ではなく経路である。ツール定義を写さず、
  JSON-RPC を解釈せず、stdout はプロトコル専用で診断は stderr に出す。
- **人が判断を下した concept は、エージェントから動かせない。**
  `put_concept` は検証済み・deprecated を拒み、墓標の復活も拒む(draft の
  墓標は復活できる)。MCP には前提条件(`If-Match`)の通り道が無く、後勝ちは
  draft には許せるが、人が整えたものが黙って置き換わると誰も気づかない
  からである。デプロイ自身のエージェントにも同じ制限を課す。これは認可では
  ない — 人の面からは動かせる。
- `put_concept` で status を省いた文書は `draft` として書かれる(OKF の
  既定の `stable` にはしない) — draft のキューに入り、人が見るように。

### MCP に載せないもの

ブラウズ・履歴・被リンクのツール(`linked_from` が運ぶ)、ファイルの書き込み、
バルク(export / import)、purge、reembed、`stats`、`fm.` のフィルタ、削除
(削除は裁定である — 誤りなら `report_outcome` failed、良いものがあれば
別 id の draft)、利用回数の合計、裁定、ポリシー、そしてデプロイ自身の
エージェント(MCP の向こうは既にエージェントである)。

## CLI

- **REST の薄いクライアント**で、同一バイナリ。`internal/apiclient` は
  `internal/service` を import せず、契約をミラーした型を持つ。サーバー側に
  CLI 専用の面を作らない。DB の隣でしか動かないコマンドは `serve` だけで
  ある(例外として、`ochakai import` は既存の書き込みを一件ずつ叩いて合成
  する)。
- **フラットな動詞コマンド**(`ochakai search`)。引数なしの `ochakai` は
  ヘルプで、`serve` へは落ちない。
- **接続先は `--url` > `OCHAKAI_URL` > `ochakai use` の設定**の順に解決する。
  設定が持つのは URL だけでトークンは無い。`ochakai login` は作らず、
  `ochakai whoami` が「どこに・誰として・届くか」を見せる。
- **終了コードは 0 / 1**(`stats --exit-code` と `eval --exit-code` が 2 を
  足す)。
- **`put`・`delete`・`get` はバンドルの両方のオブジェクトを扱う。** concept の
  住所は `<id>.md`、ファイルはそのパスで、拡張子の無い引数は id として読む。
  書き込みはバイト列が concept かファイルかを決め、削除と読みは綴りが指した
  住所を問い、無ければもう一方を試す。`ochakai get <ファイルのパス>` は
  バイト列を stdout に出す(テキストでないバイト列は端末には出さない)。
- **行の第一列は、訊いた鍵である。** 検索の行は `uri / status / title —
  description` で、スコアを出さない(スコアはモードごとに尺度が違い、行と行を
  比べられない — `--json` には残る)。フィードの第一列は並べた鍵そのもの
  (回数や日付)である。
- **住所は太字、行末の散文は dim、間は素のまま。** 標準出力が端末のときだけ
  飾り、パイプ・リダイレクト・`--json` と、`NO_COLOR` が空でないときは飾らない。
  `stats` と `get` は飾らない。

### CLI に載せないもの

DB 直結コマンド(`serve` を除く)、レビューキューのヒューリスティックの
再実装、TUI・対話モード、ローカルキャッシュ・オフラインモード、そして
**エージェントへの問い** — CLI を使う人はたいてい自分のエージェントを持ち、
能力は REST に残る。見直す条件は、シェルはあるが自分のエージェントを持たない
人が、問えずに実際に詰まった一件である。

## Web UI

役割と配信・ページの形・編集は設計記録
[0130](../design/0130-the-web-ui-and-the-fields-of-a-document.md) が持つ
(現行仕様の `webui.md` は未着手)。面の配分として決まっていることは次の
とおりである。

- **答えは自分の結果を見せる。** エージェントが提案し、人が自分の身元で
  走らせたクエリの結果を、表(50 行まで)で必ず描き、**結果の形が一つの
  読み方しか許さないときだけ**グラフを描く — 最初の列がラベルで残りの列が
  すべて数(1〜4 本)のとき、ラベルが日付・時刻なら時間軸の縦棒、文字列なら
  返った順の横棒(20 本まで)。折れ線は描かない(線は値の無いところもつなぐ)。
  軸は一本で、桁が 20 倍以上違う系列は小さなグラフに分ける。色は列の位置に
  付く。描くのはページ自身のコードで、外のライブラリを読まない。**グラフは
  会話と一緒に消える。** モデルは軸を選ばない。
- **SQL を以後クリック無しで走らせることに、会話ごとに一度同意できる。**
  本人の身元・読み取りだけ・同じ課金上限で、BigQuery のエラーはエージェントに
  返り、人が何も書かずに続けて走るのは 6 本までである。
- **結果報告のボタンは、エージェントの答えへの判定としてだけ置く。** 実行して
  いない画面からの報告は、再検証フィードの信号を汚す。

### Web UI に載せないもの

ダッシュボード、モデルが設計するグラフ、サーバーでの SQL 実行、concept に
置く結果報告のボタン、OKF import のアップロード、ログインフォーム、purge、
reembed、`fm.` のフィルタ、producer の申告。

## REST に載せないもの

サーバーでの SQL 実行、LLM に裁定させる機能(自動検証、LLM の順位付け、
知識の代わりに配る要約)、認証とユーザー管理、Web UI の配信(`serve-ui` が
別に配る)、バルクの OKF import(踏み切るのは、バイナリを持ち込めない環境
からのリストア要求が実際に来たとき)。

## やらないこと

- 縮小版の pack(hit に本文の冒頭、get に関連 concept を同梱) — 見直す条件は、
  一回の HTTP 呼び出しで読むもの一式を要するホストが実際に詰まったとき。
- 被リンクのツール、`linked_from` の行に文書を載せること、行の順序に順位を
  持ち込むこと。
- エージェントの第二の入口(会話をサーバーに持つ面、ストリーミングの別経路、
  MCP のツール)。
- 新しいサーバー面(CLI 専用エンドポイント、トークン)、公開エンドポイント、
  橋でのリトライやキャッシュ。

## 経緯

設計記録 [0068](../design/0068-how-a-face-is-added-and-removed.md)・
[0096](../design/0096-a-listing-is-not-a-search-here-either.md)・
[0097](../design/0097-a-write-says-what-it-did-in-the-body.md)・
[0103](../design/0103-the-tool-result-travels-once.md)・
[0110](../design/0110-the-first-column-is-the-key-you-asked-for.md)・
[0111](../design/0111-weight-for-the-eye-and-only-for-an-eye.md)・
[0135](../design/0135-a-rejection-is-a-deletion.md)・
[0140](../design/0140-one-address-reads-as-well-as-writes.md)・
[0145](../design/0145-four-faces-and-an-answer-that-shows-its-result.md)。
