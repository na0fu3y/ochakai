# Web UI

**ループの人間側**の面 — 人が探し、読み、書き、裁定し、境界を引き、
デプロイ自身のエージェントに問う場所である。BI ツールではない。何を載せ、
何を載せないかの配分は [faces.md](faces.md) が持ち、ここはページそのものを
書く。

## 配信: 一枚のページ、二つの経路

**ページは認証を知らない。** 自分のオリジンの `/api/v1` を叩くだけで、
資格情報は手前のプロキシが付ける。だから同じ一式を二通りに配れる。

| | `ochakai ui` | `ochakai serve-ui` |
|---|---|---|
| 用途 | 一人で使う。デプロイ不要 | チームの常設 URL(Cloud Run) |
| トークンの出所 | 利用者の gcloud / ADC | サービスのメタデータサーバー |
| 記録される名前 | `human:<本人>` | サービスアカウント。IAP を前に置けば本人 |
| 待受 | 127.0.0.1 だけ | `:$PORT`(守るのは Cloud Run IAM / IAP) |

- 同じバイナリ・同じイメージに同居し、UI とサーバーの版は常に一致する。
  **`ochakai serve` は UI を配信しない**(Cloud Run IAM の後ろではブラウザが
  ID トークンを付けられないので)。
- `ochakai ui` は**利用者のトークンで書けるプロキシ**なので、127.0.0.1 に
  固定し、`Host` を検証し(DNS リバインディング対策)、ブラウザ由来の
  `Authorization` と委譲ヘッダを捨てる。`/mcp` もプロキシするので、手元の
  MCP クライアントを本人として繋げる。
- **両方の経路が、別のサイトから来た書き込みを断る**(`Sec-Fetch-Site` か
  `Origin` を見る。どちらも無い要求 — CLI・エージェント・curl — は通す)。
- 手元の `ochakai ui` は、エージェントが提案した SQL を本人の身元で走らせる
  (dry run で SELECT と確かめ、課金上限 10 GiB)。共有の `serve-ui` では
  ページが Google のサインインで本人の短命なトークンを得て、BigQuery を
  直接呼ぶ(`OCHAKAI_OAUTH_CLIENT_ID` が要る)。
- **Content-Security-Policy の下で配る**: `script-src 'self'`、
  `connect-src` は自分と BigQuery だけ、`frame-ancestors 'none'`(他人の
  フレームに入らない)。`style-src` は `'unsafe-inline'` を残している。

## ページの作り

- 配るのは `index.html`・`app.css`・`js/` の ES モジュールで、**ビルドも
  フレームワークも CDN も npm の依存も無い**。ブラウザが読むのは書かれた
  そのものである。グラフも自前の SVG で描く。
- 静的資産の ETag は一式のハッシュで、`Cache-Control: no-cache`(API より
  古いモジュールをキャッシュから出さない)。
- ブラウザを要らないモジュールは Node の `node --test` で試す
  (`internal/webui/jstest`)。画面の日本語は CONTRIBUTING.md の規則に
  従い、`copy.test.js` が読む。

## 画面

| 画面 | できること |
|---|---|
| ホーム・ディレクトリ | その階層の `index.md` を描く。サブディレクトリ(件数と、書かれていれば説明)・concept・ファイル。サイドバーは同じ木 |
| 検索 | 問いとフィルタ、ヒットの一節。Ctrl+K(⌘K)でどこからでも開く |
| concept の詳細 | 文書・provenance・trust・ファイル・履歴・結果報告と利用・`linked_from`。検証・削除(却下は理由付き)・移動 |
| 編集 | frontmatter の欄と文書の二つのペイン。一つの文書を両方から編集する |
| レビュー | 四つのキューと `stats` の数、検証の週ごとの図 |
| アクセス(`#/access`) | ディレクトリ単位の付与を表として読み、置き換える。ポリシーを読めた呼び出し元にだけ出る |
| BigQuery から取り込む(`#/seed`) | データセットのスキーマを本人の身元で読み、テーブルの draft を作る([seeding.md](seeding.md)) |
| 問う | デプロイ自身のエージェントとの会話、結果の表とグラフ、👍/👎、修正案の差分と「適用する」([architecture.md](architecture.md)) |

## 編集は文書である

- **保存されるのは文書そのもの**である。編集画面はサーバ所有のキーを
  持たない正準形を渡し、`If-Match` に読んだ版を付けて書く
  ([concurrency.md](concurrency.md))。
- **frontmatter には欄がある。** 欄に読むのも書き戻すのもサーバー側の
  `POST /api/v1/frontmatter` で、ブラウザは YAML を読みも書きもしない。
  この操作は文書を受け取り文書を返す関数で、保存はしない。
- 書き戻しは**名指したキーのブロックだけの差し替え**で、キーの順・
  コメント・空行・他の値のクォート・本文は動かない。欄の無い producer の
  拡張キーはその場所に残り、画面がそれを名指して言う。
- 欄にならないもの: `generated` と `verified`(観測なので書けない)、
  `okf_version`、producer の拡張キー。型ごとに欄を出し分けない(型ごとの
  スキーマを持たないので)。

## このデプロイができないことを、出さない

押すと必ず失敗するボタンは嘘である。

- `read-only` では書くボタン(検証・削除・編集・新規)を出さない。
- `sandbox` は全ページにバナーを出す([deployment.md](deployment.md))。
- ファイルの置き場所が無いサーバー(`stats` の `files.enabled: false`)では
  ファイルを足す面を出さず、直し方(変数の名前)はバンドル全体を持つ
  呼び出し元にだけ見せる。
- エージェント・取り込み・アクセスの画面は、それが使えるデプロイと呼び出し
  元にだけ出る。

## やらないこと

- UI のための認証機構(ブラウザの本人は IAP の仕事)、`serve` からの配信。
- バンドラ・フレームワーク・npm 依存、文書のシンタックスハイライト。
- ダッシュボード、保存するグラフ、サーバーでの SQL 実行、concept に置く
  結果報告のボタン、OKF のアップロード取り込み、purge・reembed・`fm.` の
  フィルタ・producer の申告([faces.md](faces.md))。
- 型ごとの欄の出し分け、エクスポート形の編集。

## 経緯

設計記録 [0130](../design/0130-the-web-ui-and-the-fields-of-a-document.md)・
[0065](../design/0065-identity-and-provenance.md) §5・
[0094](../design/0094-the-page-runs-under-a-policy.md)・
[0131](../design/0131-a-deployment-says-what-it-cannot-do.md)・
[0145](../design/0145-four-faces-and-an-answer-that-shows-its-result.md)・
[0148](../design/0148-the-empty-base-fills-from-the-page-too.md)。
