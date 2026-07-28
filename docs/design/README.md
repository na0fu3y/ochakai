# ochakai 設計ドキュメント index

番号付きの設計ドキュメント(decision record)の一覧。各ドキュメントは
**決定の記録**であり、採択後は書き換えない。後続の設計が決定を覆した・
改訂したときは、旧ドキュメントの `Status:` ヘッダに改訂元へのリンクが
追記される(例: [0011](0011-gcs-attachment-storage.md)、
[0018](0018-semantic-model-as-knowledge.md))。

**ある領域の現在の姿を知るには、その領域の全ドキュメントを Status の
注記に従って読めば足りる**状態を保つ。これがこの index の目的であり、
維持ルールは [CONTRIBUTING.md](../../CONTRIBUTING.md) の「Design docs」
節にある。番号には欠番があり得る — 採択・ランドに至らなかった提案は
PR の中に残る。

英語話者向けに、各ドキュメントの決定と利用者への影響を要約した
[README.en.md](README.en.md) を置いている。**正典は各ドキュメント本体**で、
要約が食い違えば本体が正しい。新しいドキュメントを足したら要約も足す —
足し忘れは `TestEnglishDesignIndexCoversEveryRecord` が落ちる。

## アーキテクチャと基盤

- [0001 全体アーキテクチャ](0001-architecture.md) — **Accepted**(§3 の
  エンベロープ表現と §9.1 の rejected ステータスは 0043 が改訂、§9.1 の
  一覧面と検索フィルタの語彙は 0046 が言い直した)。
  LLM を内蔵せず SQL を実行しないナレッジストアという中核。双方向ループ
  (エージェントが下書きし、人が検証する)、Postgres + pgvector 一本、
  利用テレメトリ。
- [0003 Google Cloud 前提化](0003-gcp-only.md) — **Accepted**。
  Cloud Run + Cloud SQL IAM を前提にし、トークン・パスワードを持たない
  (secret-zero)。

## 実装の品質ゲート

- [0035 不変条件を機械に検査させる](0035-verifiability.md) — **Accepted**。
  型に載らない不変条件(網羅性・wire 契約・往復の正確さ)を、
  golangci-lint / openapi 契約テスト / fuzz の三層で外から検査する。
  linter の選定基準は「clean tree で 0 件」。

## 認証・認可と provenance

- [0002 認証・認可](0002-authn-authz.md) — **Accepted**(§4 の
  「IAP JWT 検証」は 0032 が実装済み)。認可機構を
  持たない —「到達できた者は読み書きできる」。ヘッダから読むのは
  provenance だけで、信頼の判断は参照側が provenance を見て行う。
- [0040 デプロイ単位の read-only](0040-read-only-mode.md) —
  **Accepted**。`OCHAKAI_READ_ONLY` でデプロイ全体を読み取り専用にする。
  呼び出し元を区別しないので 0002 の「認可を持たない」は維持される。
  強制点はサービス層 1 箇所、各面は自分の語彙で告げる(REST は 403 と
  ヘッダ、MCP は書き込みツールを出さない)。
- [0042 公開読み取り専用という姿勢](0042-public-read-only.md) —
  **Accepted**。誰でも到達できるデプロイのために、identity を一切読まない
  (トークンも委譲ヘッダも見ない、全員 anonymous、401 を返さない)。
  read-only を含意するので「公開かつ書き込み可能」は設定として存在しない。
- [0027 呼び出し元によるエンドユーザー identity の委譲](0027-delegated-provenance.md)
  — **Accepted**。信頼済みの呼び出し元が `X-Ochakai-On-Behalf-Of` で
  エンドユーザーを名乗り、provenance がサービスアカウント 1 つに
  潰れる問題を解く。
- [0009 OKF/Git 往復と provenance の所有権](0009-provenance-portability.md)
  — **Proposed**(§4 の「SPEC への先回りをしない」は 0036 が写像を決定。
  §3.1 の世界観は 0043 が保存形とワイヤにまで貫徹)。
  export → Git → import の往復で provenance が誰のものに
  なるかの整理(バンドルは知識のみを運び、provenance はインスタンス固有)。

## ナレッジモデル(構造・ID・型・名前・リンク)

- [0046 バンドルがアドレス空間](0046-bundle-address-space.md) —
  **Accepted**(実装は後続 PR、0.15.0 の次のリリースで 0043 の実装と
  束ねて出る)。**OKF 互換領域の現行ドキュメント**。バンドルを
  「パス → オブジェクト」の写像そのものとし、概念でないファイルも
  往復させる(取り込みで消えるものが無くなる)。保存は**受け取った
  バイト列**で正準形は導出値、`index.md` / `log.md` は SPEC §8 / §9 の
  導出面(browse と revisions を吸収)、本文リンクは SPEC §6 の形だけ、
  trust は SPEC §5.3 の 3 段、索引は frontmatter の jsonb。REST は
  `/api/v1/bundle/{path}` を中心とした 8 面に畳まれ、添付という概念が
  対象消滅する。
- [0043 文書を真とする](0043-document-first.md) —
  **Superseded by 0046**(§2 の世界観と status / 台帳 / actor の決定は
  0046 が引き継ぎ、保存形・ハッシュの対象・ワイヤ・正準化・リビジョン・
  サーフェスを 0046 が改める。§3.11 の Web UI のフォーム糖衣は 0044 が
  先に撤回した)。保存もワイヤも正準化した OKF 文書
  そのものにし、DB は索引とインスタンス台帳(検証・却下・利用・履歴)に
  徹する。status は OKF の 3 値、検証は台帳への追記(複数可)、却下は
  rejection 台帳、ETag は内容ハッシュ、actor は `process:`。写像が恒等に
  なり、attrs と封筒の二重底・rejected の非可逆往復・外来 `stable` の
  draft 降格・`updated_at` の三役が対象消滅する。
- [0041 住所で検索の範囲を絞る](0041-path-scoped-search.md) — **Accepted**。
  0017 の「パスが住所」を検索と context に届かせる `prefix` フィルタ
  (繰り返し可・セグメント境界で照合・サブツリー全体)。サブツリーを
  1 コールで引き分けるための機構であって、閲覧の制限ではない
  (§4)。索引もマイグレーションも足さない。
- [0037 宣言した期限と引用元から引けるようにする](0037-stale-and-source-lookup.md)
  — **Accepted**(実装済み、0.14.0 で出る)。0036 が封筒に載せた
  `stale_after` / `sources` を引けるようにする: `sort=stale_after` の
  期限切れフィード(verify ではなく編集で空になる — 0025 の 2 フィードとの
  違いは §2.2)と、引用元からの逆引き `source` フィルタ(+ GIN index。
  0046 §3.11 で frontmatter の jsonb 上の containment になる)。
- [0036 OKF のスキーマを真とする](0036-okf-schema-first.md) —
  **Superseded by 0043**(document-first への全面置き換え。0043 の実装が
  ランドするまでコードとリリースは 0036 の姿。§5 の 2 項目は 0037 が撤回して
  実装した。§3.6 の型の一覧は 0038 が
  11 型に組み替えた — 型の語彙は「ナレッジモデル」節の 0038 が現行)。
  SPEC が定義するキーは封筒に持つ、と基準を引き直し、§5.1(`sources` /
  `usage_window`)と §10.2(`runtime` / `parameters` / `computation` /
  `executor` / `attester`)を attrs から封筒フィールドへ。trust/lifecycle の
  v0.2 キーへの移行と往復規則の全体像も 1 本にまとめ直し、過去の ochakai との
  互換より OKF との互換を優先する。
- 0034 OKF v0.2 準拠 — **Withdrawn**(欠番)。v0.2 対応として書かれ
  main にマージされたが、封筒の基準(§2.3)が誤りと分かり、リリースを
  迎えないまま 0036 に統合してファイルごと削除した。文面は PR #176 の
  履歴に残る。経緯は 0036 §1.1。
- [0005 OKF 互換とナレッジの構造](0005-okf-compatibility.md) —
  **Superseded by 0036**(改訂の履歴として 0016 / 0017 も参照)。
  OKF バンドルとの双方向互換の出発点 — 任意ネストのパス、自由文字列タイプ、
  バンドルインポート、サーバー所有キー。
- [0016 knowledge-catalog リファレンスバンドルへの準拠](0016-knowledge-catalog-alignment.md)
  — **Superseded by 0036**(型の語彙は 0023 が先に置き換え、
  §2.5 の compile `dialect` は 0028 で対象消滅)。
  リファレンスバンドルへの準拠(`resource` の封筒フィールド化ほか)。
- [0017 パスが住所、タイプは属性](0017-path-addressing.md) —
  **Accepted**(ID 文字種は 0019 §2、型の語彙は 0023 が改訂、住所での
  絞り込みは 0041 が追加)。
  ID がアドレスであり、パスはもはや型を主張しない。
- [0019 v0.10.0 リリース前の整合調整](0019-release-review-adjustments.md)
  — **Accepted**(§4 は 0028 で対象消滅)。ID 文字種の OKF 同等化、
  バンドルインポートは拒否文書をスキップしてレポート。
- [0022 ファイル名が名前](0022-filename-as-name.md) — **Accepted**。
  title の任意化、ID の検索対象化、NFC 正規化。
- [0023 型の語彙を OKF に一本化する](0023-okf-type-vocabulary.md) —
  **Superseded by 0038**(改訂の履歴: §3.1 の一覧に 0036 が
  `Attested Computation` を追加)。内部スラグと OKF 表示名の二重語彙を
  廃止し、型の値は OKF の綴りそのものに。
- [0038 推奨型の語彙を OKF の証拠に合わせ直す](0038-type-vocabulary-realignment.md)
  — **Accepted**(書き込み時の case 正規化は 0046 §3.9 が撤去)。
  **型の語彙領域の現行ドキュメント**。`Semantic Model` と
  `Golden Query` を外し、`Skill` / `Playbook` / `Policy` / `API Endpoint` を
  足して 11 型に(退役した綴りは自由型として存続、マイグレーションなし)。
  語彙を述べる 11 箇所を `domain.TypesHint()` と外側のテストで固定する。
- [0024 リンクは本文から導出する](0024-links-from-body.md) —
  **Accepted**(本文の `ochakai://` は 0046 §3.6 が退役させ、SPEC §6 の
  バンドル絶対と相対だけが正準形になる)。構造化 `links` フィールドを
  廃止し、リンクは本文 markdown から導出する。

## 添付ファイル(0046 でバンドルのオブジェクトになった)

- [0008 画像添付](0008-image-attachments.md) — **Superseded by 0046**
  (先行して Superseded in part by 0011/0013)。画像添付の導入。
  「原本ではなく根拠」の位置づけと `<id>/<name>` は、0046 では帰属の
  **導出規則**として残る。
- [0011 添付画像バイト列の GCS 移行](0011-gcs-attachment-storage.md) —
  **Superseded by 0046**(先行して Superseded in part by 0013)。
  バイト列を GCS に置くという判断だけが残る。
- [0013 添付ファイルの一般化と GCS 一本化](0013-attachment-files-gcs-only.md)
  — **Superseded by 0046**。任意ファイル形式への一般化と GCS 一本化。
  sniff によるメディアタイプ判定と markdown-only 運用は 0046 も維持し、
  SVG / HTML の書き込み拒否だけが配信側の防御に置き換わる。
- [0020 添付ファイルの検索](0020-attachment-search.md) — **Accepted**
  (鍵は 0046 §3.3 でバンドルパスになる)。ファイル名のレキシカル一致と
  全ファイルのベクトル検索。

## ブラウズと Web UI

- [0006 Web UI の二つの配信経路](0006-web-ui-serving.md) — **Accepted**
  (2026-07-19 改訂、§6 の非目標のうち per-user provenance は 0032 が
  覆した)。自己完結 1 ファイルの UI を、認証モデルの異なる
  二経路(`ochakai ui` / `ochakai serve-ui`)で配信する。
- [0014 フォルダブラウズ](0014-folder-browse.md) — **Superseded by 0046**
  (browse は SPEC §8 の `index.md` の導出面になる。API パラメタは
  0017 §4.7、UI は常設サイドバーへ改訂されていた)。prefix に
  よるツリー閲覧。
- [0021 ナレッジの move と Web UI の微調整](0021-move-and-webui-refinements.md)
  — **Accepted**(0046 §3.3 が `<id>/` 名前空間のオブジェクトも一緒に
  動かすよう改訂)。move(パス変更)の導入と表示の整理。
- [0044 Web UI は文書を編集する](0044-web-ui-edits-documents.md) —
  **Accepted**。0043 §3.11 のフォーム糖衣を撤回し、編集を正準 OKF 文書の
  テキスト編集一本にする(詳細画面は投影のまま)。ブラウザに YAML パーサを
  抱えるか、読み取りの既定形を二重にするかの二択を避けた判断で、
  キュレーション面が形式そのものを教えるという理由づけ。行エディタを失う
  ことは支払いだと明記し、戻すときはサーバ側に「文書 → 構造」面を足すと
  条件を書いてある。
- [0032 Web UI の書き込みを IAP の identity で記録する](0032-webui-iap-identity.md)
  — **Accepted**。両プロキシはブラウザ由来の委譲ヘッダを常に削り、
  serve-ui は IAP の検証済み JWT からのみ作り直す(0027 §6 の実装)。

## サーフェス(REST / MCP / CLI / Web UI)

- [0004 リモート CLI](0004-cli.md) — **Accepted**(コマンド表の
  `compile` 行と終了コード 2 は 0028 で対象消滅)。REST API の
  薄いクライアントとしての CLI。
- [0007 DB 直結コマンドの廃止](0007-api-only-cli.md) — **Accepted**。
  データの出し入れを API 経由に一本化。DB 直結で残るのは `serve` のみ。
- [0015 サーフェス一貫性の方針](0015-surface-consistency.md) —
  **Accepted**(§4 の verify 糖衣の判断は 0025 §6 が覆し、サーフェス表は
  0046 §3.14 が現在の姿に言い直した)。
  4 サーフェスの役割分担と、意図して実装しないもの。
- [0033 context の hits は順位に徹する](0033-context-hits-are-a-ranking.md)
  — **Accepted**(0046 §3.10 が行に trust tier を足す)。バイト予算の決定を記録し、`hits` から知識の複製を外す
  (全サーフェスで `id`/`type`/`title`/`status`/`score` のみ)。REST の
  応答形が変わる。

## 検証ループと利用測定

- [0025 書き戻しループを締める](0025-closing-the-loop.md) —
  **Accepted**(§6 は 0015 §4 の verify 糖衣の判断を覆した。フィードは
  0037 が 3 つめを足した。§6 の verify の記録は 0043 が検証台帳への
  追記に改め、再検証が履歴として残る)。
  検証の時効と、failed 報告・利用実績による再検証の優先順位づけ。
  再検証を記録する `POST /api/v1/verify/{id}` はここが出所。
- [0029 利用測定を読み取りパスから外す](0029-usage-recording-off-the-read-path.md)
  — **Accepted**。利用イベントをメモリにバッファして定期フラッシュし、
  利用統計は best-effort と明示する(上限超過は破棄、シャットダウンは
  ドレイン後に最終フラッシュ)。

## 同時実行と削除

- [0030 If-Match による楽観ロック](0030-optimistic-locking.md) —
  **Accepted**(§3.1 の「版は `updated_at`」は 0043 が内容ハッシュに、
  0046 がその対象を保存バイト列に改訂。機構そのものは維持)。ETag / If-Match で条件付き更新を
  opt-in で提供する。MCP は通り道を持たず、キュレーション保護が内部で使う。
- [0031 purge](0031-purge.md) — **Accepted**(面の綴りは 0046 §3.5)。ソフト削除済みエントリを
  完全に破棄して id を解放する二段階目の削除。REST / CLI のみ、監査行を
  残し、GCS の blob は回収しない。

## セマンティックモデルと compile

- [0018 import-ossie の廃止](0018-semantic-model-as-knowledge.md) —
  **Accepted**(compile 面と書き込み時検証は 0028 が撤去、推奨タイプ
  `Semantic Model` は 0038 が語彙から外した)。セマンティックモデルは
  専用機構を持たず、通常のナレッジエントリ。
- [0028 compile_sql とセマンティックモデル面の撤去](0028-retire-compile-sql.md)
  — **Accepted**(§3 の「`Semantic Model` を語彙に残す」は 0038 が覆した)。
  決定的 SQL コンパイルとその周辺(API・MCP・CLI・UI)をすべて撤去。

## MCP OAuth コネクタ(撤去済み)

- [0010 MCP OAuth コネクタサービス](0010-mcp-oauth-connector.md) —
  **Superseded by 0012**。claude.ai / ChatGPT リモートコネクタ向けの
  公開第二サービス。再実装時はこの設計が出発点。
- [0012 MCP OAuth コネクタサービスの撤去](0012-retire-mcp-oauth-connector.md)
  — **Accepted**。同サービスの撤去。0002 §4 の「必要になったら」に回帰。
- [0039 stdio クライアントへの橋](0039-mcp-stdio-bridge.md) —
  **Accepted**。0012 が残した穴(stdio しか話せないクライアントに
  経路が無い)を、公開サービスではなく CLI の `mcp-stdio` で埋める。
  JSON-RPC をメッセージ単位で素通しするので、ツール定義を複製しない。
