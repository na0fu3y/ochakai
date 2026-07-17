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
→ **2026-07-16 追記**: 下記のとおり業界全体が「Context Layer」を掲げ始めたため、この主張を更新する。

### 2026-07-16 追記: Context Layer 化するカタログとウェアハウス内蔵セマンティックレイヤ

初版の調査後、データカタログ各社が一斉に「AI エージェント向け Context Layer」へ
ポジションを移した。0001 初版の「単機能 OSS は見当たらない」は文言としては今も正しいが、
**大手プラットフォームがこの領域を主戦場と宣言した**という前提の変化を記録する。

| 事例 | 概要 | ochakai との対比 |
|---|---|---|
| OpenMetadata | 「the #1 open context layer for humans, AI assistants, and agents」。Context / Semantics / Memory の三本柱。MCP サーバー内蔵(読み書き・RBAC 継承・利用分析)。用語集は 6 ステータス(Draft / In Review / Approved / Deprecated / Rejected / Unprocessed)+ owners / reviewers + 承認ワークフロー。Metric エンティティは定義保存のみで SQL コンパイルなし。memory 相当(question/answer + type + primaryEntity、ハイブリッド検索)は商用 Collate 2.0 の Context Center が中心 | 思想が最も近い。ただし Java サーバー + Elasticsearch + ingestion(Airflow)の重量級で、130+ コネクタによる自動収集が本体。ochakai の insight/query 相当は商用側にある |
| DataHub | 「The Context Platform for your Data and AI Stack」。MCP サーバーでメタデータグラフ(リネージ・品質・オーナーシップ・利用パターン)をエージェントに提供 | カタログ由来の技術メタデータが中心。解釈ナレッジ・ゴールデンクエリの概念はない |
| Atlan | 「The Context Layer for AI」。MCP で検索・リネージ・メタデータ更新。Context Agents が説明文・メトリクス・オントロジーを自動生成し、人間が認証(certify)して活性化 | 「エージェント生成 → 人間認証」のループは ochakai の draft → verified と同型。商用 SaaS |
| dbt MCP(更新) | 60+ ツール。Semantic Layer ツールセットに `get_metrics_compiled_sql`(MetricFlow によるコンパイル済み SQL 取得)と `list_saved_queries` を含む | 決定論的コンパイル済み SQL の提供では最も近い。ただし dbt Cloud / dbt プロジェクトが前提で、解釈ナレッジ・用語集・書き戻しはない |
| Snowflake Semantic Views / Databricks Unity Catalog Metric Views(2026-04 GA) | ウェアハウス自身がセマンティック定義(メトリクス・ディメンション・synonyms)をスキーマオブジェクトとして内蔵。SQL・BI・エージェントから一貫して参照可能 | 単一ウェアハウス内で完結する世界では ochakai の metric 型と競合。ウェアハウス横断・解釈ナレッジ・ウェアハウスに書けない知見(見方・注意点)が ochakai の残余価値 |

この状況での ochakai の差別化(構造的に他が持たないもの):

1. **LLM ゼロ・実行ゼロ・ingestion ゼロ**。カタログ各社は NLQ・埋め込み・AI エージェントを
   本体に取り込む方向で、攻撃面と運用コストが増え続けている。ochakai は
   Go バイナリ + Postgres のみ(~$10/月)を維持する。なお実行環境は
   Google Cloud(Cloud Run + Cloud SQL)のみをサポートする([0003](0003-gcp-only.md))。
   軽さの比較はアーキテクチャの話であり、マルチクラウド対応を意味しない。
2. **解釈ナレッジ(insight)と書き戻しループが OSS のコア**。OpenMetadata では
   この層(memories)が商用 Collate 側にある。
3. **キュレーテッドなナレッジベースであってカタログではない**。コネクタで自動収集せず、
   人間とエージェントが検証しながら書く。規模より信頼密度を取る。

裏返しのリスク: 既にカタログを運用する組織には「カタログの MCP で足りる」と映る。
ochakai は単独導入(カタログ未導入の小規模チーム)か、カタログと併用する
「エージェント向け検証済みナレッジ層」として立つ。

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
type: metric           # 推奨: metric | query | insight | term | table(自由タイプ可 → 0005)
title: 売上
description: 一行説明(検索結果に表示)
tags: [sales, core]
status: verified       # draft | verified | deprecated | rejected(§9.1)
status_note: 現ステータスの理由(自由記述。却下・非推奨の理由に使う)
provenance:
  created_by: {kind: agent, name: claude-code}   # kind: human | agent
  verified_by: {kind: human, name: na0}
  verified_at: 2026-07-01
  rejected_by: ...     # rejected のとき。verified_by と対称(§9.1)
  rejected_at: ...
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
  → **2026-07-16 supersede**([0003](0003-gcp-only.md)): Google Cloud
  (Cloud Run + Cloud SQL)前提化。ローカル開発(docker compose)のみ非 GCP で動く。
- 認証: 初期はクライアント毎の Bearer トークン。Cloud Run 前段の IAM/IAP との併用可。
  → **2026-07-16 supersede**([0002](0002-authn-authz.md) / [0003](0003-gcp-only.md)):
  cloudrun-iam 一本化、静的トークンは廃止。

## 7. Go の採用理由

- 静的単一バイナリ → 最小イメージ・再現可能ビルド・監査容易(§6 の目標に直結)
- MCP 公式 Go SDK(modelcontextprotocol/go-sdk)が存在
- 決定論的コンパイラは Go の得意領域。LLM/ML ライブラリが不要なので Python の生態系優位が効かない
- トレードオフ: Ossie のリファレンス実装(Python)を再利用できず、パーサ/バリデータを自作する。
  ただし JSON Schema 検証は流用可能で、サブセット戦略により影響は限定的

## 8. 決定事項(2026-07-14)

1. **SQL 非実行の原則**: 採用。ochakai はウェアハウス認証情報を持たず、実行はクライアントが行う。
2. **verified への昇格権限**: 人間のみをデフォルトとし、設定で緩和可。
   → **2026-07-15 撤廃**([0002](0002-authn-authz.md)): 昇格制限は設けず、
   `verified_by` の provenance 記録で担保する。
3. **compile Phase 1 の範囲**: スター結合サブセット + BigQuery/ANSI から。
4. **日本語検索**: pg_trgm を基本とし、オプトインのハイブリッド検索を初期実装に含める。
   埋め込みドライバは当面 Vertex AI ネイティブ(ADC 認証、API キー不要)。未設定時は
   trigram のみで動作。OpenAI 互換ドライバは将来拡張(§4 検索を参照)。
5. **insight の粒度**: デフォルト語彙(baseline/seasonality/caveat/threshold)を利用者が
   インスタンス設定で調整可能とし、実運用の知見で見直す。
6. **MCP ツール名**: `動詞_対象` 形式(`search_knowledge` 等、§4 を参照)。

## 9. 書き戻しループの運用強化(2026-07-16)

本設計の中核仮説「エージェントからの書き戻し + 人間による認証」を実運用で回すために、
OpenMetadata 比較調査(§2 の 2026-07-16 追記)で特定した 3 つの欠落部品を追加した。
いずれも [0002](0002-authn-authz.md)「認可ではなく記録で担保する」の範囲内である。

### 9.1 rejected ステータス

`status` に `rejected` を追加(draft / verified / deprecated / rejected)。

- **意味の区別**: `deprecated` = かつて正しかったが今は非推奨。`rejected` =
  正しいと認められなかった。却下の記録が残ることで、エージェントが同じ提案を
  繰り返すのを防ぐ。
- **provenance**: `verified_by` / `verified_at` と対称に `rejected_by` /
  `rejected_at` を自動スタンプ(rejected への遷移時のみ。rejected のままの編集では
  再スタンプしない)。0002 に従い遷移の権限制限はしない。
- **却下理由**: 共通エンベロープの `status_note`(自由記述)に書く。rejected 専用に
  せず、deprecated の理由にも使う。
- **検索**: status フィルタ未指定の検索・一覧から rejected を除外する
  (deprecated は含めて下位: 廃止定義は「昔こう呼んでいた」照合に有用だが、
  却下された知識は誤答の種になるため)。`status=rejected` を明示すれば取得でき、
  エージェントが起草前に「同種の提案が過去に却下されていないか」を確認する経路
  として MCP ツール説明に明記している。
- OpenMetadata の 6 ステータス(Draft / In Review / Approved / Deprecated /
  Rejected / Unprocessed)からは **rejected のみ**採用。In Review / Unprocessed は
  承認ワークフロー機構が前提であり、reviewer という役割を持たない ochakai には過剰。

### 9.2 利用テレメトリ

どのナレッジが実際に使われたかを記録する。draft 昇格判断の材料
(「この draft は 20 回参照されている」)、verified の陳腐化シグナル
(「半年ヒットしていない」)、書き戻しループ全体の健全性測定
(Meta memories の成功指標は「4,500+ レシピが 15 万回利用」)が目的。

- **イベント**: `search_hit`(検索結果に含まれた)/ `fetched`(get で取得)/
  `compiled`(compile_sql が参照)。actor(human/agent + 名前)付きで
  `knowledge_event` に append-only 記録し、同時に `knowledge_usage` の累計
  (件数・最終利用日時)を更新する。生イベントは 180 日で刈り取り、累計は無期限。
- **公開 API**: `GET /api/v1/knowledge/{type}/{id}/usage`。
- 記録の失敗はリクエストを失敗させない。検索結果への利用数同梱・ランキングへの
  反映は**実運用データを見てから**(初期から人気バイアスを入れない)。
- すべて自 DB 内で完結し「デフォルトで外部 API を呼ばない」を維持する。
  OTel トレース(MCP semantic conventions 準拠、OTLP エンドポイント設定時のみ)は
  レイテンシ・エラー監視用の将来オプション。

### 9.3 ゴールデンクエリのカナリア運用

verified なクエリもスキーマ変更で腐る。§2 の OpenAI 事例「ゴールデンクエリを
評価カナリアとして常時運用」に相当する運用を、**非実行原則を守ったまま**
ガイドとして提供する: 実行主体はクライアント(CI / エージェント)であり、
ochakai 本体の追加は `sort=verified_at`(検証日時の古い順に列挙、未検証は最後)
のみ。手順・判定基準・CI スニペットは
[docs/guides/golden-query-canary.md](../guides/golden-query-canary.md)。

### 9.4 決定事項(2026-07-16)

1. `status` に `rejected` を追加。検索デフォルトから除外、`rejected_by` /
   `rejected_at` / `status_note` を共通エンベロープに追加(OKF frontmatter にも出力)。
2. ナレッジ利用イベントを DB 内に記録し `/usage` で集計を公開。ランキング反映は保留。
   OTel はオプトインの将来オプション。
3. カナリア運用はガイドとして提供。本体追加は `sort=verified_at` のみ。
4. リンク・関係の語彙の標準化(SKOS 風)と OpenMetadata インポータは**採用しない**
   (必要が生じたら再検討)。
