# ochakai 設計ドキュメント 0003: Google Cloud 前提化

Status: Accepted(2026-07-16)
Date: 2026-07-16

## 1. 決定

ochakai は **Google Cloud(Cloud Run + Cloud SQL、任意で Vertex AI)を前提**とし、
それ以外の実行環境をサポートしない。0001 の「他の環境での利用を妨げない」方針を
supersede する。

動機は簡略化。実運用(ochakai-example)で確立した構成 —
組織 IAM・トークンレス認証・パスワードレス DB・特権なし SA — が
GCP のアイデンティティ基盤に全面的に依存しており、非 GCP との両対応を保つと
認証まわりに分岐(静的トークン、CORS、プロバイダ切替)が増え続けるため。

## 2. 削除するもの(v0.3.0、破壊的変更)

| 削除 | 理由 |
|---|---|
| `OCHAKAI_AUTH` / `clients` モード / `OCHAKAI_CLIENTS`(静的トークン) | 認証は cloudrun-iam 一本。トークンという概念ごと廃止 |
| `OCHAKAI_CORS_ORIGINS` と CORS ミドルウェア | 静的ホスティング UI(非 GCP 経路)の廃止。webui は同一オリジンプロキシ方式のみ |
| `OCHAKAI_EMBEDDING_PROVIDER` | Vertex AI のみ。`OCHAKAI_VERTEX_PROJECT` の有無で有効化 |
| 手順書の §3-alt(公開 + トークン)等の非 GCP 分岐 | 本線一本化 |

後方互換: 旧環境変数が設定されたまま起動した場合、意味が変わらないもの
(`OCHAKAI_AUTH=cloudrun-iam`、`OCHAKAI_EMBEDDING_PROVIDER=vertex`)は無視し、
挙動が失われるもの(`OCHAKAI_CLIENTS`、`OCHAKAI_CORS_ORIGINS` 等)は
移行先を示すエラーで起動を拒否する。

## 3. 残るもの

- **ローカル開発**: プレーンな PostgreSQL(docker compose)で動く。認証は
  `OCHAKAI_INSECURE_DEV=true` の明示オプトインで human:anonymous として動作
  (名前どおり本番禁止。未設定なら ID トークンがないリクエストは 401)。
- **ポータビリティの理念**: 「あなたのナレッジはあなたのもの」は OKF エクスポート
  (git 管理可能な Markdown バンドル)で担保する。ランタイムの可搬性ではなく
  データの可搬性で約束を守る。
- Issue #5(MCP OAuth)は公開エンドポイント向けの将来オプションとして残す。

## 4. 環境変数(v0.3.0 の全一覧)

| 変数 | 意味 |
|---|---|
| `OCHAKAI_DATABASE_URL` | Cloud SQL 接続文字列(必須) |
| `OCHAKAI_DB_IAM_AUTH` | `true` で IAM DB 認証(パスワードレス) |
| `OCHAKAI_VERTEX_PROJECT` | 設定するとハイブリッド検索有効(Vertex AI 埋め込み) |
| `OCHAKAI_VERTEX_LOCATION` / `OCHAKAI_VERTEX_MODEL` / `OCHAKAI_EMBEDDING_DIM` | 埋め込み詳細(既定: us-central1 / gemini-embedding-001 / 768) |
| `OCHAKAI_INSECURE_DEV` | ローカル開発専用: 認証なしで human:anonymous |
| `PORT` / `OCHAKAI_ADDR` | 待受アドレス |
