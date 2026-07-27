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

## アーキテクチャと基盤

- [0001 全体アーキテクチャ](0001-architecture.md) — **Accepted**。
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
- [0027 呼び出し元によるエンドユーザー identity の委譲](0027-delegated-provenance.md)
  — **Accepted**。信頼済みの呼び出し元が `X-Ochakai-On-Behalf-Of` で
  エンドユーザーを名乗り、provenance がサービスアカウント 1 つに
  潰れる問題を解く。
- [0009 OKF/Git 往復と provenance の所有権](0009-provenance-portability.md)
  — **Proposed**(§4 の「SPEC への先回りをしない」は 0036 が写像を決定)。
  export → Git → import の往復で provenance が誰のものに
  なるかの整理(バンドルは知識のみを運び、provenance はインスタンス固有)。

## ナレッジモデル(構造・ID・型・名前・リンク)

- [0037 宣言した期限と引用元から引けるようにする](0037-stale-and-source-lookup.md)
  — **Accepted**(実装済み、0.14.0 で出る)。0036 が封筒に載せた
  `stale_after` / `sources` を引けるようにする: `sort=stale_after` の
  期限切れフィード(verify ではなく編集で空になる — 0025 の 2 フィードとの
  違いは §2.2)と、引用元からの逆引き `source` フィルタ(+ GIN index)。
- [0036 OKF のスキーマを真とする](0036-okf-schema-first.md) — **Accepted**
  (実装済み、次のリリース 0.14.0 で出る)。**OKF 互換領域の現行ドキュメント**
  (§5 の 2 項目は 0037 が撤回して実装した)。
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
  **Accepted**(ID 文字種は 0019 §2、型の語彙は 0023 が改訂)。
  ID がアドレスであり、パスはもはや型を主張しない。
- [0019 v0.10.0 リリース前の整合調整](0019-release-review-adjustments.md)
  — **Accepted**(§4 は 0028 で対象消滅)。ID 文字種の OKF 同等化、
  バンドルインポートは拒否文書をスキップしてレポート。
- [0022 ファイル名が名前](0022-filename-as-name.md) — **Accepted**。
  title の任意化、ID の検索対象化、NFC 正規化。
- [0023 型の語彙を OKF に一本化する](0023-okf-type-vocabulary.md) —
  **Accepted**(§3.1 の一覧に 0036 が `Attested Computation` を追加)。
  内部スラグと OKF 表示名の二重語彙を廃止し、型の値は OKF の綴りそのものに。
- [0024 リンクは本文から導出する](0024-links-from-body.md) —
  **Accepted**。構造化 `links` フィールドを廃止し、リンクは本文
  markdown から導出する。

## 添付ファイル

- [0008 画像添付](0008-image-attachments.md) — **Superseded in part by
  0011/0013**。画像添付の導入。「原本ではなく根拠」の位置づけと
  エクスポート正準レイアウトは有効。
- [0011 添付画像バイト列の GCS 移行](0011-gcs-attachment-storage.md) —
  **Superseded in part by 0013**。バイト列の GCS 移行(bytea との
  二重化はその後廃止)。
- [0013 添付ファイルの一般化と GCS 一本化](0013-attachment-files-gcs-only.md)
  — **Accepted**。任意ファイル形式への一般化と GCS 一本化。
  現行の添付仕様の本体。
- [0020 添付ファイルの検索](0020-attachment-search.md) — **Accepted**。
  ファイル名のレキシカル一致と全添付のベクトル検索。

## ブラウズと Web UI

- [0006 Web UI の二つの配信経路](0006-web-ui-serving.md) — **Accepted**
  (2026-07-19 改訂、§6 の非目標のうち per-user provenance は 0032 が
  覆した)。自己完結 1 ファイルの UI を、認証モデルの異なる
  二経路(`ochakai ui` / `ochakai serve-ui`)で配信する。
- [0014 フォルダブラウズ](0014-folder-browse.md) — **Accepted**
  (API パラメタは 0017 §4.7、UI は常設サイドバーへ改訂)。prefix に
  よるツリー閲覧。
- [0021 ナレッジの move と Web UI の微調整](0021-move-and-webui-refinements.md)
  — **Accepted**。move(パス変更)の導入と表示の整理。
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
  **Accepted**(§4 の verify 糖衣の判断は 0025 §6 が覆した)。
  4 サーフェスの役割分担と、意図して実装しないもの。
- [0033 context の hits は順位に徹する](0033-context-hits-are-a-ranking.md)
  — **Accepted**。バイト予算の決定を記録し、`hits` から知識の複製を外す
  (全サーフェスで `id`/`type`/`title`/`status`/`score` のみ)。REST の
  応答形が変わる。

## 検証ループと利用測定

- [0025 書き戻しループを締める](0025-closing-the-loop.md) —
  **Accepted**(§6 は 0015 §4 の verify 糖衣の判断を覆した。フィードは
  0037 が 3 つめを足した)。
  検証の時効と、failed 報告・利用実績による再検証の優先順位づけ。
  再検証を記録する `POST /api/v1/verify/{id}` はここが出所。
- [0029 利用測定を読み取りパスから外す](0029-usage-recording-off-the-read-path.md)
  — **Accepted**。利用イベントをメモリにバッファして定期フラッシュし、
  利用統計は best-effort と明示する(上限超過は破棄、シャットダウンは
  ドレイン後に最終フラッシュ)。

## 同時実行と削除

- [0030 If-Match による楽観ロック](0030-optimistic-locking.md) —
  **Accepted**。`updated_at` を版として ETag / If-Match で条件付き更新を
  opt-in で提供する。MCP は通り道を持たず、キュレーション保護が内部で使う。
- [0031 purge](0031-purge.md) — **Accepted**。ソフト削除済みエントリを
  完全に破棄して id を解放する二段階目の削除。REST / CLI のみ、監査行を
  残し、GCS の blob は回収しない。

## セマンティックモデルと compile

- [0018 import-ossie の廃止](0018-semantic-model-as-knowledge.md) —
  **Accepted**(compile 面と書き込み時検証は 0028 が撤去)。
  セマンティックモデルは専用機構を持たず、通常のナレッジエントリ。
- [0028 compile_sql とセマンティックモデル面の撤去](0028-retire-compile-sql.md)
  — **Accepted**。決定的 SQL コンパイルとその周辺(API・MCP・CLI・UI)を
  すべて撤去。

## MCP OAuth コネクタ(撤去済み)

- [0010 MCP OAuth コネクタサービス](0010-mcp-oauth-connector.md) —
  **Superseded by 0012**。claude.ai / ChatGPT リモートコネクタ向けの
  公開第二サービス。再実装時はこの設計が出発点。
- [0012 MCP OAuth コネクタサービスの撤去](0012-retire-mcp-oauth-connector.md)
  — **Accepted**。同サービスの撤去。0002 §4 の「必要になったら」に回帰。
- [0038 stdio クライアントへの橋](0038-mcp-stdio-bridge.md) —
  **Accepted**。0012 が残した穴(stdio しか話せないクライアントに
  経路が無い)を、公開サービスではなく CLI の `mcp-stdio` で埋める。
  JSON-RPC をメッセージ単位で素通しするので、ツール定義を複製しない。
