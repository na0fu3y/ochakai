# ochakai 設計ドキュメント 0028: compile_sql とセマンティックモデル面の撤去

Status: Accepted(2026-07-25)。§3 の「型 `Semantic Model` は推奨語彙に
残す」は [0038](0038-type-vocabulary-realignment.md) §3.1 が覆した
(語彙からは外れるが、エントリは自由型として無傷 — §5「既存エントリを
削除・移行しない」は有効)
Date: 2026-07-25

## 1. 決定

決定的な SQL コンパイル(`compile_sql` / `POST /api/v1/compile` /
`ochakai compile`)と、それを支えるセマンティックモデルのサーバー側の
振る舞いを**撤去**する。[0018](0018-semantic-model-as-knowledge.md) は
import-ossie を廃止しつつ「compile_sql は継続する」と決めた(§1)が、
その継続の判断だけを本改訂で覆す。0018 の残り — セマンティックモデルを
専用テーブルではなく通常のナレッジエントリとして置く、専用インポート経路は
持たない — は有効なまま残る。

削除するもの:

- `internal/compiler` 一式(Ossie モデルのパース・検証、BigQuery SQL 生成)
- `Service.Compile` / `CompileRequest` / `CompileResult`、および
  type=`Semantic Model` の書き込み時検証(0018 §4.2)
- `Store.ListModelsDefiningMetric` / `Store.ListMetricEntryIDs`
  (モデル解決のためだけに存在する 2 つの走査)
- `POST /api/v1/compile` と OpenAPI の `CompileRequest` / `CompileResult`
- MCP ツール `compile_sql`
- CLI `compile`(completion 含む)と `apiclient.Compile`
- Web UI の Compile タブ(メトリクスエントリの対話コンパイル)
- usage の `compiled` イベントと `Usage.Compiles`(§3)
- README / CONTRIBUTING / SECURITY / デプロイガイドの compile 面、
  `examples/semantic-model.md`

## 2. 理由

0018 の技術判断は覆っていない。覆ったのは**費用対効果の前提**であり、
0012(コネクタ撤去)と同じ形をしている。

- **利用がない。** 0.12.1 時点で compile を使っている利用者はいない。
  埋め込み提供の検討(2026-07-25)でも、ホスト側は compile を使う予定が
  ないと結論している。
- **README が自分で降格させていた。** ツール表の外に置き「optional。
  埋め込みホストは allowed-tools から外せ、他は何も劣化しない」と書いて
  あった。使わない前提でドキュメントが書かれているものを、コードだけが
  第一級で持ち続けている状態だった。
- **答えは get_context 側にある。** エージェントが必要とするのは検証済みの
  クエリとその注意点であり、両方 `get_context` から来る。compile が答える
  のは「誰も聞いたことのないメトリクス × 粒度 × フィルタの組み合わせ」
  だけで、これは実在するが聞こえるほど広くない。
- **ツールスキーマはエージェントのコンテキストである。** compile_sql は
  ここで最大のスキーマを持っていた。使わないツールの定義が全リクエストの
  コンテキストを食っていた。
- **保守面積と継続債務。** 実装 + テストで ~1,300 行、6 つのサーフェス。
  さらに Ossie 仕様への追従と BigQuery 方言の面倒を、利用ゼロのまま
  抱え続けることになる。
- **攻撃面が 1 つ減る。** SECURITY.md が「特に歓迎する報告」に挙げていた
  「compile_sql の出力が、由来を主張するセマンティック定義やゴールデン
  クエリとずれる経路」— コンパイル結果は下流で実際の warehouse 権限で
  実行される — がなくなる。

サーバーに残る保証は provenance・リビジョン・検証ワークフローになる。
0015 の役割分担(解釈と生成はクライアント、記録はサーバー)の線を、
「決定的な生成」も含めてクライアント側に引き直す、ということである。

## 3. データと互換性

**マイグレーションは不要。**

- セマンティックモデルは 0018 以降、専用テーブルを持たない**通常の
  ナレッジエントリ**である。撤去してもエントリは 1 件も動かない。
- `compiled` は `usage_events` の汎用の行であり、専用の列ではない。
  集計側の `FILTER (WHERE event = 'compiled')` が消えるだけで、既存の
  行は残す — **履歴は履歴として残す**(0001 §9 の記録の原則)。
  再実装したくなったときにも過去の使用実績は失われていない。

**残るもの。**

- 型 `Semantic Model` は推奨語彙に**残す**。0023 §3.1 は内部型を OKF の
  表示名 8 つと一致させており、振る舞いが無い型(`Insight`・
  `Glossary Term`・`Reference`)がすでに 3 つある — 語彙は振る舞いの
  レジストリではない。既存のモデルエントリは検索・export・import・
  リビジョンをこれまでどおり保ち、型の正規化(大小文字)も効き続ける。
  失うのは書き込み時検証だけであり、壊れた `attrs.spec` は誰も読まない
  ので黙って通ってよい。
- 型 `Metric` も残る。`attrs.model` / `attrs.expression` は
  サーバーが読まない自由な attrs になり、規約はクライアントのものになる。

**壊れるもの(0.13.0 の breaking change)。**

- `POST /api/v1/compile` は 404、MCP の `compile_sql` は消え、
  `ochakai compile` は usage エラーになる。
- usage レスポンスから `compiles` フィールドが消える
  (REST・CLI・Web UI・OpenAPI)。

0012 のような起動拒否ガードは要らない。消えるのはエンドポイントと
コマンドであり、叩けば即座に分かる。設定変数も増減しない。

## 4. 再実装の条件

`internal/compiler` は設計としても実装としても健全だったので、
0018 §4.2-4.3 と本コミットの revert を出発点にできる。以下が具体化したら
戻す:

- コンパイルを実際に必要とする利用者・埋め込みホストの出現
  (「聞かれたことのない組み合わせ」を SQL にする要求が、検証済みクエリの
  検索では足りないと確認できること)
- その時点で Ossie 仕様への追従を引き受ける用意があること

戻すときも 0018 の形 — モデルは通常のナレッジエントリ、1 エントリ 1
モデル、`attrs.spec` に verbatim、専用インポート経路なし — を維持する。

## 5. やらないこと

- **既存 `Semantic Model` エントリの削除・移行。** 利用者のナレッジで
  あって、サーバーの都合で消してよいものではない(0009「Your knowledge
  is yours」)。
- **`compiled` 使用イベント行の削除。** 上と同じ理由。
- **`Metric` 型の廃止。** メトリクスの定義は、コンパイルされなくても
  散文の知識として意味がある。
- **段階的な deprecation 期間。** 利用者がおらず、消える面は叩けば即座に
  分かる。0.13.0 で一度に落とす。
