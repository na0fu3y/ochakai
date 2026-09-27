# 空のベースを埋める

入れたばかりのベースは空で、空のベースにエージェントは何も問えない。
**最初の数十件を、人が自分で、コネクタ無しで作れる**ようにすること(C4)。

## 出発点はスキーマで、出るのは draft である

- **テーブル一つにつき `BigQuery Table` の draft が一つ**できる。列は本文の
  markdown の表(列名・型・モード・説明)、`resource` はテーブルの住所。
  日付でシャードされたテーブル(`events_20260101`…)は二つ以上そろえば
  `events_` の一件にまとめ、`events_*` と名乗る。
- **description は空のまま出す。** スキーマは骨格であって、テーブルが何の
  ためにあるか、どの列が嘘をつくか、いつロードが遅れるかを知らない。
  誰も書いていない一文を frontmatter に置けば、その後の全員がそれを信じる。
  列の説明は、BigQuery の中で誰かが書いた文なので、表の Note 列に出所の
  まま入れる。
- 空の description を埋めるのは、テーブルを知っている人の言葉と、実際に
  数えた事実である。デプロイ自身のエージェントがそれを手伝う — 目的を訊き、
  行数・鮮度・NULL を本人の身元で数え、次の版を**提案**し、問うた人が
  適用する([architecture.md](architecture.md))。

## 二つの入口

### `ochakai seed`(CLI)

- **ウェアハウスに接続しない。** 入力は運用者自身が撃った
  `INFORMATION_SCHEMA.COLUMNS` の答え(JSON の配列か、一行一オブジェクト)
  で、出力は OKF バンドルの tar.gz。書き込むのは既存の `ochakai import`
  である。ウェアハウスに依らないので、同じ行を出せれば Snowflake でも
  Postgres でも使える。
- `--prefix` で置き場所を決める(既定 `tables`)。

### Web UI の「BigQuery から取り込む」(`#/seed`)

- **同じ投影を、ページが本人の身元で読んだスキーマから作る。** 読み方は
  エージェントの SQL と同じ(同じサインイン、`bigquery.readonly`、一回
  10 GiB の課金上限。手元の `ochakai ui` ではプロキシが本人として走らせる)。
- 入力は三つ: 取り込み元(`project.dataset`、または `project.dataset.table`
  で末尾の `*` は名前の先頭で絞る)、課金するプロジェクト
  (`OCHAKAI_BIGQUERY_PROJECT` があれば出さない)、置き場所。
- 読むのは `INFORMATION_SCHEMA.COLUMNS` に列の説明を結んだ一本だけで、
  **一覧が 50,000 行を超えたら、一部を作らずに断る**(途中で切れた一覧は
  小さなデータセットと見分けがつかない)。
- 書くのは既存の `PUT` で、**既にある住所は上書きしない**
  (`If-None-Match: *`)。
- ホームの入口は、書けるデプロイで、BigQuery を読む手段(OAuth クライアント
  か `ochakai ui`)があるときだけ出る。`OCHAKAI_OAUTH_CLIENT_ID` と
  `OCHAKAI_BIGQUERY_PROJECT` はエージェント無しでも効く。
- **サーバーは何も新しく提供しない** — REST・MCP・CLI・環境変数のどの数も
  動かない。

**投影は Go(`ochakai seed`)と JS(ページ)の二か所にあり、一つの
ゴールデンファイルが両方を縛る。** 二つの入口が違う draft を作ることは
ない。

## クエリ履歴

「よく使われている集計は?」と問われたときにだけ、デプロイ自身の
エージェントが**問うた人自身の**クエリ履歴を数え、繰り返し走っている集計を
一回 5 件までの draft にする([architecture.md](architecture.md))。チーム
全体の履歴から下書きを作るのは、運用者が置く定期ジョブ
(`examples/bigquery-catalog` の `draft-from-query-history`)の仕事である。

## やらないこと

- コネクタによる自動収集、定期の取り込み、刈り取り。
- サーバーがウェアハウスに接続すること、サーバー側の投影エンドポイント。
- description を推測で埋めること。
- 既にある concept を上書きすること。

## 経緯

設計記録 [0148](../design/0148-the-empty-base-fills-from-the-page-too.md)
(それ以前は [0085](../design/0085-the-empty-base-and-what-fills-it.md))・
[0149](../design/0149-the-agent-proposes-and-the-person-applies.md)。
手順は [最初のひと月](../guides/onboarding.md)。
