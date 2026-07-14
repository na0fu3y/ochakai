# ochakai 設計ドキュメント 0001: 全体アーキテクチャ

Status: Accepted(2026-07-14 の議論で主要論点を決定)
Date: 2026-07-14

## 1. ochakai とは

ochakai は、データエージェント(Claude Code / 各種 Data Agents)に「メトリクスの定義」だけでなく
「その値をどう見るか」までを提供する **Context Provider** である。

- メトリクス定義(セマンティック定義)
- 検証済みゴールデンクエリ
- 解釈ナレッジ(メトリクスの見方: ベースライン、季節性、注意点)
- 用語集
- テーブルカタログ

を 1 つのナレッジベースに集約し、MCP 経由で複数のエージェントに共有する。

原則として **LLM を使用しない**。返すものは、セマンティック定義からの決定論的コンパイル結果か、
人間が検証したナレッジそのものである。解釈はクライアント側エージェントの仕事であり、
エージェントは学びをナレッジとして書き戻すことが推奨される。

## 2. 先行事例からの設計要件

### 社内データエージェント

| 事例 | ochakai が引き継ぐべき点 |
|---|---|
| OpenAI 社内データエージェント | 6 層のコンテキスト(スキーマ、人手アノテーション、コード由来メタデータ、社内ドキュメント、自己学習メモリ、実行時クエリ)。ゴールデンクエリ(質問と検証済み SQL のペア)を評価カナリアとして常時運用 |
| Meta 社内分析エージェント | Cookbook / Recipe / Ingredient の三層でドメイン知識を構造化。ユーザーのフィードバックから蓄積される「memories」(4,500+ のコミュニティ作成レシピが 15 万回利用)。**書き戻しが機能している実例** |
| Airbnb Minerva | メトリクスは GitHub リポジトリで中央定義、コードレビュー + 静的検証 + 認証(certification)のワークフロー |
| Uber uMetric | View / Metric / Dimension の三部定義。検証委員会とティア制。Definition → Discovery → … → Consumption のライフサイクル |
| Snowflake Cortex Analyst | **Verified Query Repository**: 質問 + 検証済み SQL + 検証日時をセマンティックモデルに同梱。類似質問が来たら検証済みクエリを優先 |

共通する成功パターンは 2 つ:

1. **検証ステータスが一級市民**(draft → verified → deprecated)。誰がいつ検証したかが常に残る。
2. **エージェントからの書き戻し + 人間による認証**の双方向フロー。

→ ochakai のスキーマは `status` と `provenance`(人間/エージェント、作成者、検証者)をすべてのナレッジに持たせる。

### 類似 OSS との違い

- **dbt-mcp / Cube / Lightdash**: セマンティックレイヤの MCP 化。ただし「定義と実行」が中心で、解釈ナレッジ・用語集を持たない。また自社プラットフォームへの接続が前提。
- **Vanna / WrenAI**: text-to-SQL のための学習データ(質問-SQL ペア)管理を持つが、LLM パイプラインと一体で、ナレッジ単体を他エージェントに提供する設計ではない。
- **Airbnb knowledge-repo**: 分析結果(ノートブック/ポスト)の共有。人間向けであり機械可読なメトリクス定義を持たない。

ochakai のポジション: **実行しない・LLM を持たない・ナレッジと決定論的コンパイルだけを提供する** Context Provider。この領域の単機能 OSS は現状見当たらない。

### 用語の使い分け: knowledge と context

- **knowledge(ナレッジ)**: ochakai に蓄積される実体(metric / query / insight / term / table)。
  ツール名・API パス・スキーマなど、実体を操作する場面ではすべて `knowledge` を使う
  (`search_knowledge`、`/api/v1/knowledge`)。OKF(Open Knowledge Format)とも揃う。
- **context(コンテキスト)**: ナレッジから組み立てられ、エージェントに届けられる側の概念。
  「ochakai は Context Provider である」というポジショニングや、MCP(Model Context
  Protocol)経由で提供する行為を指す場面でのみ使う。

つまり「Context Provider として knowledge を提供する」が一貫した言い方であり、
`context` を実体名(ツール名・テーブル名・型名)に使わない。

## 3. ナレッジモデル

全ナレッジは共通エンベロープ + タイプ別の構造化属性 + Markdown 本文で表現する。
これは OKF(Open Knowledge Format: YAML frontmatter + Markdown)と 1:1 で相互変換できる形にする。

### 共通エンベロープ

```yaml
id: revenue            # slug。URI は ochakai://metric/revenue
type: metric           # metric | query | insight | term | table
title: 売上
description: 一行説明(検索結果に表示)
tags: [sales, core]
status: verified       # draft | verified | deprecated
provenance:
  created_by: {kind: agent, name: claude-code}   # kind: human | agent
  verified_by: {kind: human, name: na0}
  verified_at: 2026-07-01
links:                 # 型付きリンク。[[id]] 形式で本文からも張れる
  - {rel: measures, target: table/orders}
created_at: ...
updated_at: ...
```

### タイプ別の構造化属性

| type | 構造化属性 | 参考 |
|---|---|---|
| `metric` | Apache Ossie core-spec の metric(expression + dialect、ai_context.synonyms)。所属する semantic model への参照 | Ossie 0.2.x |
| `query` | question(自然言語の質問)、sql、dialect、関連 metric、期待結果の要約 | Snowflake VQR |
| `insight` | 対象 metric/table、種別 `kind`。本文に「100 なら良いのか悪いのか」を書く。`kind` の語彙はインスタンス設定で調整可能(デフォルト: baseline / seasonality / caveat / threshold) | Meta memories |
| `term` | 同義語、正式定義、関連 term | — |
| `table` | 物理テーブル参照(project.dataset.table)、カラム説明、既知の品質問題、推奨フィルタ | OpenAI annotations |

セマンティック定義本体(datasets / relationships / metrics)は Ossie の semantic model YAML を
そのまま格納する。ochakai 独自方言を作らない。エクスポートは OKF バンドル(git 管理可能な
Markdown ディレクトリ)として全タイプ対応。

### リビジョン

ナレッジは人間とエージェントの共同所有物なので、**全変更をリビジョンとして保存**する
(誰が・いつ・何を)。delete は soft-delete。

## 4. アーキテクチャ

```
┌──────────────┐  MCP (streamable HTTP)   ┌─────────────────────────┐
│ Claude Code / │◄────────────────────────►│ ochakai (Go 単一バイナリ) │
│ Data Agents   │                          │  ├ MCP server            │
└──────────────┘                          │  ├ REST API (/api/v1)    │
┌──────────────┐  REST                    │  ├ compiler (Ossie→SQL)  │
│ 自作 Web UI    │◄────────────────────────►│  └ store (pgx)           │
└──────────────┘                          └───────────┬─────────────┘
                                                      │
                                               PostgreSQL のみ
```

- **言語: Go**(後述 §7)。単一バイナリで MCP と REST を同一ポートで提供。
- **依存は PostgreSQL のみ**。Redis やベクトル DB は持たない。
- **SQL を実行しない**。ochakai はウェアハウスへの認証情報を持たない。コンパイル済み SQL を
  返し、実行はクライアント(エージェント側の BigQuery MCP 等)が行う。攻撃面とサプライ
  チェーンを最小化し、「Context Provider に徹する」を構造で保証する。

### MCP ツール(最小)

| tool | 説明 |
|---|---|
| `search_knowledge` | 全タイプ横断検索(type / tags / status で絞り込み)。verified を上位に |
| `get_knowledge` | id 指定で 1 件取得(リンク先の要約を同梱) |
| `create_knowledge` | ナレッジ作成。エージェント作成分は自動的に `draft` + provenance 記録 |
| `update_knowledge` | 更新(リビジョン保存) |
| `delete_knowledge` | soft-delete |
| `compile_sql` | metric + dimensions + filters + time grain → SQL(検証済み類似クエリがあればそれを優先して返す) |

ツール名は `動詞_対象` 形式とする。複数の MCP サーバーを併用するエージェントから見て
`search` のような汎用名は他サーバーのツールと紛らわしく、名前だけで用途が特定できることを優先。

REST は同じ操作の `/api/v1/knowledge`, `/api/v1/compile`, `/api/v1/export`(OKF)。
OpenAPI 仕様を `api/openapi.yaml` にコミット。

### 検索(LLM なし)

- **Phase 1**: PostgreSQL `pg_trgm`(日本語は FTS のトークナイズが効かないため trigram 主体)
  + Ossie `ai_context.synonyms`・tags・title/description のフィールド重み付け。
  呼び出し元が LLM エージェントであることが前提なので、エージェント側の言い換え・再検索で
  語彙ギャップの多くは吸収できる(人間向け検索窓とは要求水準が異なる)。
- **ハイブリッド検索(オプトイン)**: pgvector によるハイブリッド検索(trigram + ベクトルを
  RRF 統合)。意味検索が特に効くのは「自然言語の質問 ↔ ゴールデンクエリの question」の照合。
  - 埋め込みドライバは当面 **Vertex AI Embeddings ネイティブ**(2026-07-14 決定)。
    初期スコープが Google Cloud 前提であることに加え、Cloud Run 上では ADC
    (Application Default Credentials)で認証できるため **API キーの発行・保管が不要**になる。
    ローカル開発は `gcloud auth application-default login` で同じコードパスが動く。
  - 実装は小さな `Embedder` インターフェースに切る。将来 OpenAI 互換
    `/v1/embeddings` ドライバ(Ollama・TEI 等のセルフホスト対応)を追加して
    Google Cloud 以外でも意味検索を使えるようにする余地を残す。
    (注意: Vertex AI 自体の OpenAI 互換エンドポイントは chat.completions のみで
    embeddings を持たないため、互換方式では Vertex を吸収できない)
  - **未設定時は trigram 検索のみで動作**する。「イメージ + Postgres だけで動く」原則は維持し、
    埋め込みは設定した場合にのみ有効化される。
  - pgvector は Cloud SQL でもプレーンな Postgres でも使えるため追加インフラは不要。
  - デフォルトは無効。「デフォルトでは外部 API を一切呼ばない」を守る。
  - 埋め込みは決定論的なエンコーダであり「解釈をしない」原則には抵触しないが、
    有効化の判断は利用者に委ねる。

### compile のスコープ(段階制)

Ossie は 0.2.0.dev のドラフトであり、Go 実装は存在しない(Python/Java のみ)。よって:

1. **Phase 1**: Ossie core-spec のサブセット(単一ファクト + スター結合、metric expression 展開、
   GROUP BY / WHERE / 時間粒度)を Go で決定論的にコンパイル。対応 dialect は BigQuery + ANSI から。
   Ossie の `osi-schema.json` をそのままバリデーションに使う。
2. コンパイルできない要求は**明確なエラーで拒否**し、関連するゴールデンクエリを提示する。
   曖昧な SQL を推測で作らない(LLM を使わない原則の帰結)。
3. Ossie の ontology 層(concept / ValueType / requires 制約)は当面採用しない。core-spec の
   semantic_model / datasets / relationships / metrics のみ。

## 5. リポジトリ構成

```
cmd/ochakai/          # エントリポイント(serve / export / import / migrate)
internal/
  domain/             # ナレッジモデル
  store/              # pgx + マイグレーション
  compiler/           # Ossie subset → SQL
  mcpserver/
  restapi/
api/openapi.yaml
migrations/
examples/
  webui/              # サンプル Web UI(コアイメージには含めない)
  knowledge/          # サンプル OKF バンドル
deploy/
  cloudrun/           # gcloud / Terraform スニペット
  compose.yaml        # ローカル・非 GCP 向け
docs/design/
```

## 6. 配布とサプライチェーン

- GitHub Actions → GHCR にマルチアーキイメージ。`CGO_ENABLED=0` + `-trimpath` の静的バイナリを
  distroless static(または scratch)に載せる。ベースイメージ依存ゼロに近い構成。
- SBOM 生成 + SLSA provenance(`actions/attest-build-provenance`)+ cosign keyless 署名。
  Actions は SHA ピン留め。Go はビルド再現性が高く「誰もが再現可能」に適する。
- 利用開始手順: イメージ + `DATABASE_URL` + 認証トークン設定のみ。推奨構成として
  Cloud Run + Cloud SQL の手順を docs に置くが、依存は Postgres のみなのでどこでも動く。
- 認証: 初期はクライアント毎の Bearer トークン。Cloud Run 前段の IAM/IAP との併用可。

## 7. Go の採用理由

- 静的単一バイナリ → 最小イメージ・再現可能ビルド・監査容易(§6 の目標に直結)
- MCP 公式 Go SDK(modelcontextprotocol/go-sdk)が存在
- 決定論的コンパイラは Go の得意領域。LLM/ML ライブラリが不要なので Python の生態系優位が効かない
- トレードオフ: Ossie のリファレンス実装(Python)を再利用できず、パーサ/バリデータを自作する。
  ただし JSON Schema 検証は流用可能で、サブセット戦略により影響は限定的

## 8. 決定事項(2026-07-14)

1. **SQL 非実行の原則**: 採用。ochakai はウェアハウス認証情報を持たず、実行はクライアントが行う。
2. **verified への昇格権限**: 人間のみをデフォルトとし、設定で緩和可。
3. **compile Phase 1 の範囲**: スター結合サブセット + BigQuery/ANSI から。
4. **日本語検索**: pg_trgm を基本とし、オプトインのハイブリッド検索を初期実装に含める。
   埋め込みドライバは当面 Vertex AI ネイティブ(ADC 認証、API キー不要)。未設定時は
   trigram のみで動作。OpenAI 互換ドライバは将来拡張(§4 検索を参照)。
5. **insight の粒度**: デフォルト語彙(baseline/seasonality/caveat/threshold)を利用者が
   インスタンス設定で調整可能とし、実運用の知見で見直す。
6. **MCP ツール名**: `動詞_対象` 形式(`search_knowledge` 等、§4 を参照)。
