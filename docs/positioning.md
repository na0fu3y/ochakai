# ポジショニング

「X を使えばいいのでは」への答えを一箇所に集めたページ。正直な答えは
たいてい**「使えばよいし、両立する」**である — ochakai はデータ
エージェントのコンテキストの一層であって、ウェアハウスやカタログ、
メモリ層やあなたのノートアプリの代わりではない。主張しているのは、
そのどれも埋めていない一つの枠である: *人が検証したナレッジを、それを
使うエージェント自身が書き、すべてのクライアントに一つの契約で配る。*

このページは評価している人のためのものである。ochakai が断るものは
[README](../README.md#what-it-refuses) と
[ROADMAP](../ROADMAP.md#considered-and-deliberately-not-doing) にあり、
表面が小さいまま保たれる理由は [docs/surface.md](surface.md) にある。
以下で使う語彙はその**八つの条件**である。

## 一行で

semantic layer は revenue = `SUM(price)` だと教えてくれる。100 という
数字が良いのか悪いのか、この系列では前年比がプラスでも何の情報も無い
こと、同じ名前の `traffic_source` 列が二つあって別物であること、そして
この問いに対して誰かが sanctioned としたクエリが*その*一本であること
は、教えてくれない。ochakai はその後半を持ち、一つ一つに人の裁定を残し、
それを消費するエージェントが次の一つを書き戻せるようにする。

## 外からの実測

上の前半 — 人が書いたコンテキストが精度を運ぶ — は、もうこのページの
主張ではなく公開された実測である。四つ挙げる。**どれも ochakai を測った
ものではない**(いずれも 2026-08-21 に原典を開いて確かめ、採取時点の数字
のまま置いてある)。

- **ベンダー自身の数字。** Snowflake は Cortex Analyst について「agentic
  AI system に semantic model を組み合わせて実世界のユースケースで 90%+
  の SQL 精度」と書き、同じ内部評価セット(150 問)で GPT-4o の単発生成は
  「51% に急落した」と書いている([2024-08-29](https://www.snowflake.com/en/blog/engineering/cortex-analyst-text-to-sql-accuracy-bi/))。
- **人が書いた文書が検索に効く量。** Pinterest は「テーブル文書を埋め込み
  に入れない場合の検索ヒット率は 40%、文書に置く重みに比例して線形に上がり
  90% に達した」と報告している([2024-04-03](https://medium.com/pinterest-engineering/how-we-built-text-to-sql-at-pinterest-30bad30dabff))。
  A/B ではなく重みへの比例である、という形のまま引く価値がある。
- **切り分けた実験。** LinkedIn の SQL Bot は ablation で、ナレッジグラフ
  の構成要素が「4+(correct or close to correct)の割合を 9%(スキーマ
  のみ)から 48%(全部入り)へ上げる」と測り、寄与の大半は **example query
  とテーブル・カラムの記述**だったと書いている([arXiv:2507.14372](https://arxiv.org/abs/2507.14372)、2025-07)。
  同じ表は逆向きの結果も載せている — domain knowledge のレコードを足すと
  4+ の割合はむしろ下がり、論文は無関係なレコードが引き下げたのだろうと
  書く。**キュレーションは足せば効くのではない**という、この節で最も引く
  価値のある一行である。
- **ベンチマークと実環境の差。** Prosus(Toqan)は ProLLM リーダーボードで
  「上位モデルは Stack Overflow データに対する SQL の acceptance が
  85–90%」としたうえで、テーブルが 2 つを超える用途での out-of-the-box の
  text-to-SQL は「25% 未満から始まった」と書く([2024-09-10](https://medium.com/prosus-ai-tech-blog/just-ask-data-insights-for-everyone-a21057d24263))。

四つが揃って言っているのは一つのことである — 精度を運んでいるのはモデル
ではなく、人が書いたコンテキストのほうである。**そしてどれも、それをどこ
に置くべきかは言っていない。** LookML でも semantic model でも wiki でも
同じ数字は出るはずで、上の四つはどれもその形で持っている。このページが
扱うのはその次の問い — 誰が確かめたか、どのクライアントに配れるか、
間違いだと分かったときにどこへ戻すか — であり、外からの実測は ochakai の
証拠ではなく、**この枠が空いていることの証拠**である。

## 隣人たち

| 比較対象 | あちらが持つもの | あちらが持たないもの | 判定 |
|---|---|---|---|
| **[Google Cloud Knowledge Catalog](https://cloud.google.com/products/knowledge-catalog)(旧 Dataplex)** | 同じ Google Cloud の IAM、立てるものが無いこと、自動収集、lineage、品質、用語集、Gemini が生成する説明と example query、Context API と remote MCP | 人の裁定が中核であること、却下の理由が履歴に残ること、OKF での丸ごとの出口、収集ではなくキュレーションであること、**一つのテーブルに二つ目の読み方を並べられること** | 同じ雲で正面から重なる — 下記参照 |
| ウェアハウス native の semantic layer(dbt MCP、Cube、Lightdash、Snowflake Semantic Views、Databricks Metric Views)と、その標準([Apache Ossie](https://github.com/apache/ossie) — 旧 OSI) | メトリクス定義、ディメンション、コンパイルされた SQL | 解釈、用語集、書き戻し、ウェアハウスをまたぐもの、そして誰が確認したか | 両立 — 下記参照 |
| 「コンテキスト層」になったカタログ(OpenMetadata、DataHub、Atlan) | 技術メタデータ、リネージ、オーナーシップ、大規模な収集 | キュレーションされた側が OSS であること — 解釈とレビューループは商用ティアに置かれがち | 両立、あるいは既に運用しているなら ochakai の代わりになる |
| エージェントのメモリ層(mem0、Zep / Graphiti、Letta、[MemPalace](https://github.com/mempalace/mempalace)、[remnic](https://github.com/joshuaswarren/remnic)) | ユーザーごと・エージェントごとの記憶、自動抽出・自動注入、チームで共有する形。**MemPalace は書き込みに LLM を使わず、Letta はメモリが git 管理の markdown + frontmatter、remnic は人の承認の門と provenance と訂正ループを持つ** | 認証された呼び出し元を観測する台帳としての裁定、secret を置かないこと(remnic は bearer token) | 両立 — 下記参照 |
| 自分の文書に対する RAG | 他の理由で書かれた文書からの断片 | 単位としてのレビュー済みの主張、provenance、著者の向き | 両立 |
| AI アナリスト製品に内蔵された検証済みクエリストア(Cortex Analyst の VQR、Genie) | 問い + 検証済み SQL、一つのベンダーのチャットの中で | 他のクライアント、出口 | 重なる、そしてロックインする |
| FDE 型オントロジー([Palantir Foundry Ontology](https://www.palantir.com/docs/foundry/ontology/overview)) | 組織のデジタルツイン — オブジェクトとリンク、Action と write-back、ライブデータへの接続、プラットフォーム内のガバナンス | 出口(オントロジーはプラットフォームのもの)、FDE 無しの立ち上げ、テナントごとの安価なセルフホスト | 約束は同じ、買い方を拒む — 下記参照 |
| OSS の「Palantir 代替」([semantica](https://github.com/semantica-agi/semantica)) | 数十ソースの取り込みと自動抽出、ポリグロットなグラフ・ベクトルストア、OWL/SHACL、PROV-O、エージェントの決定の因果記録、セルフホスト | 人の裁定が中核であること(decision の記録は verify ではない)、secret-zero、単一形式の往復、キュレーションが買う小ささ | 約束の語彙は重なる、軸が違う — 下記参照 |
| グラフ DB ネイティブのオントロジー基盤([NebulaGraph](https://nebula-graph.io/posts/ontology-and-graph-databases-enterprise-ai-from-theory-to-production-reality)、Neo4j + GraphRAG) | 型システムと書き込み時のスキーマ強制、型間のリレーション制約、数十億ノードへのスケール、多段トラバーサルとグラフアルゴリズム | 検証のループが中核であること(あちらでは成熟モデルの最終段)、markdown での出口、キュレーションされた規模が買う単純さ | 規模が前提から違う — 下記参照 |
| **OKF ネイティブのローカルツール群と、MCP サーバー付きの markdown vault**([okf-skills](https://github.com/scaccogatto/okf-skills)、[okfcli](https://github.com/okfcli/okf)、[serradura/okf](https://github.com/serradura/okf)、[kaut](https://github.com/yurgeno/kaut)、LLM Wiki 系の [llm-wiki-compiler](https://github.com/atomicstrata/llm-wiki-compiler)、Obsidian、ノートの git リポジトリ) | 同じ OKF v0.2 の markdown + frontmatter、リンク、ローカルな所有、エージェントが読み書きすること、一部は draft キューと書き込みの門、**一部は rationale 付きの approve / reject の記録**([KL4A](https://github.com/CogniSwitch/KL4A)) | 認証された呼び出し元を**観測**する台帳としての検証(あちらの `verified` は誰かがタイプした一行である)、outcome と miss の計測 | 両立 — 下記参照 |

### Google Cloud Knowledge Catalog(旧 Dataplex)

*同じ雲の上で正面から重なる。多くのチームには、これで足りる。*

このページで最も重要な隣人である。理由は機能ではなく足場で、他のどの
隣人とも違って**同じ Google Cloud の IAM の上に立つ**。ochakai の
secret-zero(C2)は Cloud Run IAM と Cloud SQL IAM で無設定に買われて
いるが、あちらは立てるものが何も無い — プロジェクトで API を有効にすれば
済み、Cloud Run も Postgres も月 $10 も要らない。Claude Code から使える
こと(C5)も、`https://dataplex.googleapis.com/mcp` のリモート MCP
サーバー(OAuth 2.0 + IAM、read-only と read-write のスコープ)が同じ
ように満たす。**失格要件で落とせない隣人はここだけである。**

持っているものは多い。2026-04-10 に Dataplex Universal Catalog から改名し
([overview](https://docs.cloud.google.com/dataplex/docs/introduction))、
BigQuery ほかからの自動収集・lineage・データ品質・ビジネス用語集を GA で
持つ。その上で Gemini が説明と関係と **example query** を生成し、semantic
search と Context API と MCP でエージェントへ配る([AI agents 向けの
解説](https://docs.cloud.google.com/dataplex/docs/ai-overview))。そして
発表([2026-04-23](https://cloud.google.com/blog/products/data-analytics/introducing-the-google-cloud-knowledge-catalog))は
**verified queries and semantic guardrails**(Preview)を挙げている —
幻覚した join を検証済みのクエリで置き換えるという、ochakai の
`Attested Computation` と同じ問題である。ここは「両立」で逃げられない
(この節の事実は 2026-08-25 と 2026-09-03 に原典を開いて確かめた)。

**そして 2026-08-27 から、あちらは OKF を読む。** リポジトリの `kcmd push`
が OKF v0.2 のバンドルを concept ごとの Entry(`okf-bundle`)と二つの
aspect — 本文と、`verified` / `stale_after` / `generated` / `attester` /
`sources` / `status` を含む 13 欄の `okf` — に写し、`LookupContext` が
エージェントに YAML で配る
([発表](https://cloud.google.com/blog/products/data-analytics/scale-okf-bundles-across-an-organization-with-knowledge-catalog))。
`ochakai export` のバンドルはその入口の形をしている(下の「export は
第三者の検証器を通る」)。**あちらが写すのは主張である**: `verified` は
文書の frontmatter から写され、カタログ自身は検証を行わず、行為者の
identity で記録もしない — ochakai が import で `received` に置くもの
(0009 §3.2、0046 §2.2)と同じ扱いで、逆向きも同じである。Knowledge
Catalog から来た `verified` を ochakai は裁定として読まない。OKF への
書き戻し、レビューキュー、却下、outcome の報告は記事に無い。concept が
一つずつ Entry になるので、下で言う「二つ目の読み方」は OKF 経由なら並ぶ。

違いは軸である: **あちらは生成して昇格させ、こちらは人が裁定する。**
起点は機械で、data steward の仕事はあちらの use case の言葉で「AI が
生成したメタデータをレビューし、キュレーションし、昇格させる」ことで
ある。ochakai の起点は人か、人の裁定を待つ draft を書くエージェントで
あり、**却下は理由ごと履歴に残る**(C7、[ループ](loop.md)) — 次の
書き手が読めるところに残るのであって、読ませる強制力は無い
([0135](design/0135-a-rejection-is-a-deletion.md) §3)。「誰がいつ確かめたか」は status ではなく別立ての
台帳であって([0065](design/0065-identity-and-provenance.md))、監査ログは
その代わりにならない。出口も向きが違う: メタデータは Cloud Storage へ
export して BigQuery から読めるが、形式はカタログのものであって、丸ごと
出て丸ごと戻る OKF v0.2 バンドルではない(C1・C3、
[0075](design/0075-the-bundle-is-the-address-space.md))。収集で
はなくキュレーションであることの意味は、下の「コンテキスト層としての
カタログ」と「OSS の『Palantir 代替』」の二節が言うとおりである。

**そしてもう一つ、形そのものが決めている違いがある: あちらの単位は
テーブルのスロットで、こちらの単位は文書である。** BigQuery のテーブルには
system entry が一つ自動で建ち、自動で入る required aspect は編集できない
— 足せるのは optional aspect だけで、しかも
[**エントリレベルでは aspect type ごとに最大一つ**](https://docs.cloud.google.com/dataplex/docs/enrich-entries-metadata)
であり(列ごとには複数持てる)、付け直しはその aspect の中身を置き換える。
つまり同じテーブルについて**二つ目の読み方を並べて置けない**。部門ごとに
違う解釈も、まだ裁定されていない draft も、置くには aspect type をもう
一つ作るか自分の entry group に並行の custom entry を建てるかで、どちらも
書くことではなくモデリングである。ochakai の単位は id を持つ文書で、
`resource` は鍵ではなく欄だから、同じテーブルを指す concept は何本でも
並ぶ — それぞれが自分の著者・検証状態・履歴を持ち、draft は verified の
隣に住み、却下されたものは理由ごと履歴に残る。**キュレーションが単位を
文書にする理由がここにある**: 一つの資産についての知識は、一つの値ではない。

**名前は紛らわしかったが、2026-08-14 に規格は自分のリポジトリを持った。**
OKF の SPEC と参照バンドルは
[GoogleCloudPlatform/open-knowledge-format](https://github.com/GoogleCloudPlatform/open-knowledge-format)
にあり、ochakai の型語彙はその参照バンドルに合わせてある
([0036](design/0036-okf-schema-first.md))。それまで置かれていた
`knowledge-catalog/okf` は、その README 自身が「凍結スナップショットで
保守しない」と書く写しである。製品としての Knowledge Catalog はこの節の
隣人であり、規格はもう競合と名前を共有していない。

**見直す条件を書いておく**: verified queries が Preview を出て、かつ
「この一本を誰がいつ確かめたか」を単位ごとに持ち、丸ごと持ち出せる形が
付いたとき。2026-08-27 で前半は主張としては満たされた。残るのは、
カタログ自身が裁定を行為者の identity で記録すること、却下が理由ごと
残ること、丸ごと OKF に戻ることの三つで、そうなればナレッジの置き場が
Google Cloud の中で足りるので、BigQuery 一本のチームに対して ochakai が
言えることはほとんど残らない。

### ウェアハウス native の semantic layer

メトリクス定義については最も近く、2026 年にはウェアハウスがそれを
スキーマオブジェクトとしてネイティブに持つようになった。単一ウェア
ハウスの世界では、その半分はあちらのものであり、ochakai の `Metric`
concept は冗長である。残るのは semantic model の YAML に収まらない
すべて — ベースライン、注意書き、しきい値、誰かが sanctioned とした
クエリ、そしてウェアハウスをまたぐナレッジである。ochakai が SQL の
生成をやめたのは意図した決定であり
([0070](design/0070-what-was-retired-and-why.md) §3)、エージェントが
必要とするのは検証済みのクエリとその周りの注意書きだからである。

**その層は 2026 年に標準を持った — [Apache Ossie](https://github.com/apache/ossie)
(incubating、旧 Open Semantic Interchange / OSI)である。** Snowflake・
dbt Labs・Salesforce ほかが始め、Apache Software Foundation に寄贈されて
Incubator に入った(Apache-2.0)。標準化するのは**定義の層**で、
2026-08 に読んだ `core-spec/spec.yaml`(`0.2.0.dev0`、draft)の
トップレベルは `semantic_model` / `datasets` / `fields` / `metrics` /
`relationships` / `dialects` / `datatypes` である。

**ochakai はこれと競合しない。同じ境界の反対側に立っている。** 上の
「一行で」がその境界そのものである — Ossie が一つの綴りに揃えるのは
revenue = `SUM(price)` の側で、ochakai が持つのは 100 が良いのか悪いのか
の側である。決定的なのは、その `spec.yaml` に **provenance・検証・trust・
鮮度・誰が書いたか を運ぶ構造が無い**ことで、あるのはベンダーごとの
`custom_extensions` だけである。つまり OKF が中核に置いているもの —
誰が書き、誰が確認し、いつ古びるか([0065](design/0065-identity-and-provenance.md)・
[0069](design/0069-the-loop-and-what-measures-it.md))— は Ossie の枠の
外にあり、**二つは同じものを二通りに言っているのではない**。重なって
いないから、両方置ける。

**いま実装はしない。** これは立場の表明であって計画ではない。Ossie を
読み書きすることは OKF の横に第二の形式を持つことであり、C3 がそれを
断っている。ウェアハウス側の semantic model を ochakai の `Metric`
concept に投影したいなら、その形は既にある — 自分のサービスアカウントで
動く普通のクライアント([例](../examples/bigquery-catalog))であって、
ochakai が持つコネクタではない([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1)。
**見直す条件は書いておく**: Ossie が incubating を出て、かつ「この定義を
人が確認した」を core が運ぶようになったとき。そのときは重なりが本物に
なるので、OKF との対応を書く価値がある。

### コンテキスト層としてのカタログ

Google 純正のそれは上の節が単独で扱う。ここはそれ以外である。

意図の上では最も近く、正直なリスクでもある: MCP サーバー付きのカタログ
を既に運用している組織は、それで十分だと考えるのが妥当である。ochakai
の構造上の違いは、解釈のナレッジとレビューループが商用ティアではなく
**オープンソースの中核**であること、そして収集ではなくキュレーション
であること — コネクタも取り込みも無く、量より信頼の密度である。カタログ
を持たないチームには単独で立ち、持つチームにはエージェント向けの検証済み
層としてその隣に立つ。カタログの投影は自分のサービスアカウントで動く
普通のクライアントであって([例](../examples/bigquery-catalog))、
ochakai が持つ資格情報では決してない。

### エージェントのメモリ層

*メモリ層は起きたことを覚え、ochakai は正しいことをキュレーションする。*
一行目は今も立つ。**その下にこの節が長く書いていた三つの前提は、崩れた**
(2026-09-05 に原典を開いて確認)。

- **「あちらは LLM で抽出する」はカテゴリ全体には言えない。** MemPalace は
  書き込み時の LLM 呼び出しがゼロで — 分類も分割も正規表現と語彙スコア、
  本文は逐語で保存する — その姿勢のまま LongMemEval R@5 96.6% を公表して
  いる。批判論文はその数字を再現できるとしたうえで、効いているのは逐語
  保存と既定の埋め込みであって階層の比喩ではないと書く
  ([arXiv:2604.21284](https://arxiv.org/abs/2604.21284))。**LLM を持たない
  ことは、もうこの節の差別化ではない。** ochakai がそれを持たない理由 —
  人の検証への信頼の根拠
  ([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md))— は
  変わらないが、理由と差別化は同じものではない。
- **「記憶は個体に属する」も言えない。** MemPalace は共有ハブを、Letta は
  クラウドの共有メモリと git のリモートを、mem0 は `app_id` / `org_id` の
  スコープを持つ。
- **「誰もレビューしない」も一律には言えない。** Letta の dreaming は提案を
  適用前に通す門を任意で持ち、Zep は ABAC と保持期限と全リクエストの
  監査証跡を売っている。**ただし門の中身は分かれる**: Letta の設定名は
  "Agent reviews before applying" で、見直すのはエージェント自身であって
  人の承認は要らない。**人が承認する門を持つのは、この並びでは
  [remnic](https://github.com/joshuaswarren/remnic) だけである。**

**残る差は一つに絞れる**: 裁定が**インスタンス自身の観測**として台帳に残り、
その行が**認証された呼び出し元**を名指すこと
([0075](design/0075-the-bundle-is-the-address-space.md) §3.1)。人が確かめた
のか機械かは、その台帳から導かれる tier が言い分ける — 中核に置いているのは
`human-reviewed` の側である
([0138](design/0138-a-verification-stands-until-the-content-moves.md))。mem0 の
history は old / new と `user_id` を持つが行為者の名前を持たず、feedback
(POSITIVE / NEGATIVE / VERY_NEGATIVE と理由)は蓄えて自社のチューニングに
使うもので、人が読む面が無い。**mem0 自身の 2026 年の報告が、誰が記憶を
検査できて・いつ消えるかは application-layer の未解決事項だと書いている。**
ochakai は人の裁定の後ろにあるチーム共有のナレッジであり、却下は理由ごと
リビジョンと `log.md` に残る — ただし**それが次の提案を止めるわけではない**。
却下を削除にしたとき、塞ぐことはしないと決めている
([0135](design/0135-a-rejection-is-a-deletion.md) §3)。

**形も近づいた。** Letta は 2026-02 にメモリを git 管理の markdown +
YAML frontmatter に作り直した(MemFS)— `system/` の下だけが毎ターン
system prompt に載り、残りはファイル木だけ見せて必要なときに読ませる。
OKF バンドルと同じ形であり、この節と下の vault 節の境目は消えつつある。

あちらから学ぶ価値があるのは記憶の品質ではなく人間工学のほうである:
本当の強みは、エージェントが覚えようと*決める*必要が無いことである。
ochakai は Claude Code の[フック](../examples/claude-code)でそれに
答える — 想起も書き戻しも習慣ではなく仕組みとして、しかも LLM 無しで。
**期限切れの扱いは、そこで分かれる。** mem0 は期限の切れた記憶を既定で
検索から隠すが(`show_expired` で戻せる)、ochakai の期限切れは「間違い」
ではなく「再確認が要る」なので
([0069](design/0069-the-loop-and-what-measures-it.md) §7)、隠さず、順位も
動かさず、想起のポインタ行に印として出す。

### 自分の文書に対する RAG

RAG は誰かが他の理由で書いた文書から断片を取ってくる。返ってくるものの
正しさは、wiki に何が書いてあったか以上にはならない。ochakai が返すのは
人が `verified` と印を付けた concept であり、誰が書き、誰がいつ確かめた
かを言う provenance と、長く確かめられていないナレッジや間違いとして
戻ってきたナレッジを浮かせるフィードが付いている。**単位はチャンクでは
なく、レビュー済みの主張である。**

もう一つの違いは向きである。RAG の索引は文書*から*作られる。ここでの
ナレッジベースは、それを使うエージェント*によって*書かれ、人が昇格
させる — 書き戻しのループが製品そのものであり、却下された提案はその
理由を履歴に残す。

### AI アナリスト製品に内蔵された検証済みクエリストア

AI アナリスト製品はどれもこれを持っており、そのどれもが自分のチャット
だけに供給する。ochakai の同じナレッジは Claude Code にも、ホストされた
MCP エージェントにも、CI ジョブにも等しく供給され、ベース全体が OKF
v0.2 バンドルとして往復する
([0075](design/0075-the-bundle-is-the-address-space.md) §1) — git に
置ける素の markdown と YAML である。MIT でテナントごとのセルフホスト:
あなたのナレッジが人質になることはない。

### FDE 型オントロジー

*約束は共有し、買い方を拒む。* オントロジーが売っているもの — 組織の
意味を一箇所に置き、オブジェクトとリンクで結び、意思決定をその上で
回す — は正しく、実はこのページのどの隣人よりも ochakai の埋める枠に
近い。[Palantir Foundry の Ontology](https://www.palantir.com/docs/foundry/ontology/overview)
はそれを二つの支払いで買う: プラットフォームへの接続(semantic な
オブジェクト・リンクに加えて、Action と write-back という kinetic の
半分)と、意味を書き起こす常駐エンジニア(FDE)である。

ochakai の賭けは、2026 年にはこの二つの支払いがどちらも要らなくなった、
というものである。kinetic の半分のうち**実行** — Action を発火させ、
ライブデータに接続し、write-back する — は、ツールを持ったエージェント
自身の仕事になった。実のところ Foundry でも Ontology は実行主体では
ない: action type はスキーマとして Ontology に定義され、実行するのは
その上のアプリケーションである。線は定義と実行の間に引かれ、**定義の
側はここに住む** — sanctioned な計算とその契約(`Attested Computation`
の `runtime` / `executor` / `attester`)も手順(`Skill`)も通常の
concept として検証と provenance の下に置かれ、他の知識と同じ検索で
実行主体に届き、実行した側は結果を `report_outcome` で書き戻す
([architecture](architecture.md))。残る建設対象は semantic の中核と
kinetic の定義、それだけである。書き起こす労働は消えない — 担い手が
FDE からエージェントに変わり、人には判断の要る中核の検証だけが残る
(README の C4、[ループ](loop.md))。

この意味で ochakai は**最小のオントロジー**と呼べる: concept とリンクが
オブジェクトとリンクに、`verified` の台帳と provenance がガバナンスに
対応し、全体が OKF バンドルとして持ち出せるので
([0075](design/0075-the-bundle-is-the-address-space.md))、オントロジーが
プラットフォームのものになることはない。持たないものは意図して持たない。
Action の実行基盤が要る、オペレーショナルな write-back が要る、
kinetic の三つ目の要素 — 誰が実行してよいかの Role — や concept 単位の
権限が要る([0065](design/0065-identity-and-provenance.md) §1)、
Google Cloud の外で動かす([0003](design/0003-gcp-only.md)) — どれか
一つでも真なら、FDE 型を買う理由はまだそこにある。買い方だけを外した
OSS が同じ約束を名乗る場合は、この節の答えが効かないので次の節が扱う。

### OSS の「Palantir 代替」

*約束の語彙は重なる、軸が違う。* このカテゴリ(代表は
[semantica](https://github.com/semantica-agi/semantica)、自称 "The Open
Source Palantir for AI Agents")は上の節の二つの支払いを両方とも外して
くる — OSS でセルフホスト、FDE の代わりに取り込みと自動抽出のパイプ
ラインが意味を書き起こす。だから「買い方を拒む」はここでは答えに
ならず、残る違いは軸そのものである: **あちらは収集し、こちらは検証する。**

あちらは数十ソースを取り込み、プロセス内の ML(NER・関係抽出)で
グラフを機械が建て、衝突検出や SHACL で後から統治する。ochakai は
コネクタも取り込みも持たない
([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1)。
どちらも「中核に LLM 不要」を名乗るが意味が逆で、あちらは抽出まで機械が
やるための ML を中に持ち、こちらは LLM(エージェント)が外にいる前提で
抽出の実装を一行も持たない — 読む・割る・書き起こすのはエージェントの
仕事で、ochakai は draft を受けて人の裁定を待つ。あちらの Decision
Intelligence が記録するのはエージェントの決定の因果であり、C7 の中核で
ある人の verify / reject と trust tier に相当する段は、グラフ DB 節と
同じく成熟の先にある。姿勢も同じ向きに割れる: ポリグロットなストレージ
と環境変数に並ぶ資格情報に対して、Google Cloud 一択の secret-zero
(C2)と OKF 一枚の往復(C3)である。

なお表面を数字で比べるなら、MCP だけは本数で語らないこと — ツール数は
あちらが多いのに、実測の常駐バイトはツールが書き方を教えるこちらの方が
重い([0076](design/0076-two-tools-leave-mcp.md) が「代金は本数ではなく
バイト」と言った当のトレードオフである)。数字を置いておく: ochakai は
6 ツールで 10,485 バイト(2026-09-04、`tools/list` の応答を丸ごと数え、
instructions の 1,037 を含む — [surface.md](surface.md) の `MCP-BYTES` が
天井を置いている当の数)、semantica は 17 ツールで 7,771 バイト
(2026-08-23)。**こちらの数字は 12,290 から落ちている**: 同じ 6 ツールで、
二度言っていた散文を削ったぶんである。トークン数は測っていない — 数えているのはバイトで、天井も
バイトである。断片化した生データから
統治されたグラフを機械で建てたいならあちらを使う。人が確かめた少数の
ナレッジをエージェントに小さな契約で配りたいなら、それがこの枠である。

### グラフ DB ネイティブのオントロジー基盤

*主張は頷けるが、規模が前提から違う。* このカテゴリの売り文句は
「オントロジーを運用するには専用のグラフエンジンが要る」である — 型ごとの
必須プロパティを書き込み時に強制し、型間に許されるリレーションを制約し
(Customer は Order を place できるが Supplier はできない)、数十億ノードを
多段にたどる([NebulaGraph の二部作](https://nebula-graph.io/posts/ontology-and-graph-databases-enterprise-ai-from-theory-to-production-reality)が典型)。
断片化した数十の業務システムを一つの意味の層でつなぐなら、正しい道具である。

ochakai の前提はその逆側にある: 人を通った数千の concept(下の「負ける
ところ」の規模の項)であり、その規模に専用エンジンは要らない。多段の
辿りは、リンクは本文の導出でどの concept も `linked_from` を連れて
返るから([0106](design/0106-a-read-carries-what-points-at-it.md))、
エージェントの search → get で足りる。型付き
リレーションは持たない — `rel` は機械可読な型として導入され、機械が
一度も読まなかったので、関係の種類は周囲の散文が運ぶ
([0136 §2](design/0136-a-concept-is-addressed-not-labelled.md))。
書き込み時のスキーマ強制も同じ向きで断っている: 型は開いた語彙
([0038](design/0038-type-vocabulary-realignment.md))で、品質は制約では
なく人の裁定で買う(C7)。

対比が最も鋭いのは成熟モデルの終点である。あちらの四段階の最終段 —
AI が生んだ洞察を人が検証してオントロジーへ還流する閉ループ — は、
12〜18 ヶ月の投資の先にある最前線として売られる。ここではそれが初日の
中核であり(draft → 裁定 → verified、`report_outcome`、
[ループ](loop.md))、その段が言う「LLM は推測せず、決定論的な意味の層に
問う」は ochakai が中に LLM を持たない理由と同じ線である
([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md))。
実体が数十億で機械的な多段推論が仕事ならあちらを買う。足りないのが
誰かが確かめたナレッジとそのループなら、グラフ DB を運用していても
この枠は別に残る。

<a id="a-markdown-vault-with-an-mcp-server"></a>

### OKF ネイティブのローカルツール群と、MCP サーバー付きの markdown vault

最も安価な代替であり、2026 年には最も高い確率で既に持っているもので
ある: YAML frontmatter を持つ markdown ノートの vault で、本文にリンク
があり、MCP サーバー経由で Claude Code から 10 分ほどで届く — インフラは
一切要らない。**そしてその vault は、2026 年の夏に OKF を話すようになった。**
Google が OKF v0.2 を公開した 2026-07-25 から三か月で、GitHub の topic
`open-knowledge-format` には 136 のリポジトリが付いた(2026-09-05)。
**この節の見出しが言う「ローカルツール群」は、もうカテゴリの全体では
ない。** 大きいものの多くは今もローカルで、ファイルで、一台である —
Claude Code プラグインと conformance checker と read-only MCP を持つ
okf-skills(359)、validate / lint / search の一本の Go バイナリ
okfcli、MCP 14 ツールの serradura/okf(153)、Obsidian 互換の
pi-llm-wiki(555)。だが同じ topic には**サーバーもクラウドサービスも
入った**: remnic(193)は HTTP + MCP のサーバーを配り、
aws-samples/sample-okf-llm-wiki は S3 / Athena / Bedrock の上で OKF
バンドルを MCP に出す。**OKF はもう Google Cloud の話ではない。**

**そして「人の裁定」を持つものが増えた。** kaut は書き込みの門と draft
キューと freshness の判定を、okf-hub は「提案だけ受け、管理者役が統合
する」レビューを、どちらも git を正本にして持つ。ここに二つ加わる —
**KL4A** は「nothing is marked `verified` until a human approves it」と
書き、approve / reject / edit を**それぞれイベントとして、reviewer と
rationale と before/after の diff ごとバンドルの中に**記録し、却下された
ものを `include_rejected` で引けるように残す。**remnic** は
「Agents propose; a human approves」を掲げ、エージェントの失敗から古い
信念を割り出して置き換えを人の承認に掛け、古い側を superseded として
残す。**この節が長く「無い」と書いてきたもの — *no* の記憶と、裁定の
記録 — は、もう ochakai だけのものではない。**

**そして規格の側に、二つの圧力が付いた。** 一つは上の「食い違い」が
示した版の運用で、もう一つは**上位互換を名乗るフォーク**である —
[aix-format](https://github.com/DavidROliverBA/aix-format) は「Every AIX
bundle is also a valid OKF bundle」と書きつつ、**パスではない恒久的な
`id`**(move と rename を跨いで生き残る)、**型付きのリレーション**
(`depends-on`・`supersedes`・`contradicts` と逆向き)、`confidence`、
namespace による federation、`manifest.aix.yaml` を足す。この四つのうち
前二つは、ochakai が**明示的に断った**ものである — 住所はパスであり
([0075](design/0075-the-bundle-is-the-address-space.md) §2)、`rel` は
機械が一度も読まなかったので落ちた
([0136](design/0136-a-concept-is-addressed-not-labelled.md) §2)。
断り自体は今も筋が通っているが、**同じ穴を別の答えで埋める形式が出て
きたこと**は、次にこの二つを問い直す人が読むべき事実である。

**そこで通した。** 2026-09-05、aix-format の `examples/` を
`ochakai import` に入れ、`ochakai export` で引き戻した。6 文書のうち
`index.md` と `log.md` は予約名なので取り込まれず(SPEC §3.1)、4 concept
が 4/4 で入り、10 ファイルで出た。**4 文書とも、provenance 以外は
frontmatter も本文もバイト一致で戻った。** 動いたのは 0009 §3.2 の
とおり — 文書が申告した `generated` / `verified` が `received` の下へ分かれ、
このインスタンス自身の `generated` と `created_by` が付いた。それだけである。

**AIX が足した四つは、四つとも原文のまま出てきた。** 恒久的な
`id: payment-service-v2`、4 本の `links[].rel`(`supersedes`・
`depends-on` ×2・`authored-by`、`note` ごと、4 → 4)、
`provenance.confidence`、federation で修飾した
`to: data-eng/orders-events`。**YAML のコメント行(`# AIX additions`)
まで残っている。** つまり **ochakai がモデル化を断ったものを、失っては
いない** — OKF §4.1 の producer キーがその穴を既に埋めており、それは
0043 §3.6 が約束していることである。

**関係は本文から届く。** `linked from` は 3 件とも本文の markdown リンク
から導出され、「supersedes」は散文が伝えていた — SPEC と 0136 §2 が
そう言っているとおりである。`fm.id` は 400 で断られ、その文面が立場の
説明になっている(「a producer's own key is stored and handed back
exactly as written, not asked for」)。**恒久 id を足すことは、
[0139](design/0139-ochakai-does-not-choose-a-spelling.md) がちょうど
畳んだ「規格が綴りを定めているものの二つ目の綴り」を足し直すこと**でも
ある — 住所を定めているのは SPEC §2 である。

**食い違いは三つ、どれも小さい。** `sources[0]` は `resource` ではなく
`uri` を書くので(SPEC §5.1)、**索引からは落ちる** — `?source=` で
引けない。**文書には残っている**ので C1 は無事で、落ちたのは向こうの
非適合のほうである。`manifest.aix.yaml` は markdown でないので
`OCHAKAI_GCS_BUCKET` の無いデプロイでは保存できない(0075 §1)。そして
**AIX 自身の例が `stale_after: 2026-11-20` と裸の日付を書いている** —
2026-08-21 以降の SPEC §5 に反するが、0139 §3.1 が入りで日付を取り続ける
と決めた理由がまさにこれで、そのまま読めてそのまま戻った。

**だから断りは、測ったうえで有効である。** 型で分岐するコードが一つ出た
日に `rel` のモデル化が値段に見合う、という 0136 §2 の反証条件も動かない。

重なりは表面的ではない — **物理的な形が同じ**なのである。`ochakai
export` のバンドルは YAML frontmatter を持つ markdown ファイルの
ディレクトリで、その関係は普通の本文リンクである
([0136](design/0136-a-concept-is-addressed-not-labelled.md) §2)。
つまり**それはどのツールでも開く**。良いエディタとグラフビューで
ナレッジベースを眺めたいのなら、それがその道具であり、ここにあるものは
何も競合しない。

**export は第三者の検証器を通る。** 2026-09-03、`ochakai export` した
53 concept のバンドルと `examples/demo` の 18 concept を、okfcli v0.5.0 の
`validate` と okf-skills の §11 conformance checker に通した。どちらも
エラー 0 で、警告はバンドルの外のファイルを指すリンクと期限を過ぎた
`stale_after` だけである。C3 は自分のテストではなく他人の検証器で守れる
ようになった。

**`kcmd push` に入れた。** 2026-09-05、`ochakai export` した
demo.ochak.ai のバンドル(18 concept + `export` が生む 22 の
`index.md` / `log.md` = 40 文書)を、`toolbox/mdcode/demo/okf` の
`setup.ts` / `push.ts` / `pull.ts` で ochakai-example の us-central1 へ
出し、引き戻し、entry group と型を消した。

**入口は机上のとおりだった。** エントリ名はファイルパスそのもので
(`.../entryGroups/ochakai_probe/entries/metrics/revenue`)、それは
ochakai の id である
([0075](design/0075-the-bundle-is-the-address-space.md) §2)。`title` /
`description` / `tags` は Documents Layout の上段へ、OKF の信号 11 キーは
`okf` aspect へ、`type` は `okf_type`、`resource` はエントリの resource へ。
**modeled でないキーは `extra` に `[path, value]` の対として退避され、
`pull.ts` がそこから元の形を組み直す。** 往復は 40/40 で値も本文も動かず、
frontmatter を持たない 22 文書は**バイト一致で戻った** — 却下の理由は、
prose としてなら本当に向こうへ渡る。残る 18 は YAML の引用符・キー順・
行の折り方だけが違う。

**食い違いは二つあった。**

**一つ目 — `YYYY-MM-DD` の日付で push が落ちる。** aspect の
`stale_after`・`sources[].last_modified`・`usage_window` は `datetime` 型
で、`Text '2026-07-24' could not be parsed at index 10` で 400 になる。
40 文書のうち 3 文書の 5 値がそれに当たり、RFC 3339 に直せば 40/40 が
通った。**そして push はトランザクションではない** — 24 エントリが
出来たところで止まった。バンドルを丸ごと渡す相手が SPEC に厳密なら、
`export` の出口は今のところ通らないことがある。

**この節は「こちらが広げた側である」と書いていた。それは誤りだった
(2026-09-05 に訂正)。** 広げたのは規格の側である。**v0.2 は公開後に
normative な規則を変え、版番号を動かさなかった。**

- **2026-07-24 に公開された v0.2** の §5.5 は `stale_after` を
  「An absolute date (`YYYY-MM-DD`)」と定義し、比較を
  `today >= stale_after` と書いていた。`sources[].last_modified` も
  `usage_window` も日付だった。ochakai が日付だけを受けていたのは、
  **そのときの SPEC がそう書いていたから**である。
- **2026-08-21**、`knowledge-catalog` の #323 と
  `open-knowledge-format` の #6 が同じ日に同じ変更を入れ、三つの鍵は
  「offset 付きの ISO 8601 datetime」になった。両リポジトリの SPEC は
  現在バイト等価で、**見出しは両方とも今も「Version 0.2」である。**
- SPEC §12 は minor を「後方互換な追加」、major を「破壊的変更」と
  定めている。**この変更はどちらでもない** — 妥当だった値が妥当で
  なくなった。**それでも版は動いていない。**

つまり [0133](design/0133-an-okf-moment-is-an-instant.md) §1 の
「SPEC はそう言っていない」は、**書かれた時点の SPEC については正しく、
ochakai の過去のコードについては誤っている**。あの規則は誤読ではなく、
七日前まで規格そのものだった。**0133 の決定(二つの綴りを取る)は、
理由が変わってむしろ強くなる** — 二つの状態の「v0.2」で書かれた
バンドルを両方読める唯一の規則が、それだからである。

**代金は出口の側に移った。** 0133 §3.1 の「来た綴りで戻す」は、UTC
真夜中を日付に畳んで書く。**OKF 自身の参照エージェントは、その綴りを
黙って落とす** — `is_stale()` は `"T"` を含まない値に対して常に
`False` を返す。2026-09-05 に main の参照エージェントで実測した:

| `stale_after` | 参照エージェントの `is_stale` |
|---|---|
| `2026-01-01`(既に過去) | **False** |
| `2026-01-01T00:00:00Z`(同じ瞬間) | True |

**これは仮定ではなく、そのとき出荷していたバンドルの話だった。**
`examples/demo/queries/sales/revenue-by-traffic-source.md` の
`stale_after` は `2026-08-01` で、ochakai の期限切れフィードは正しく
上げるのに、**OKF 適合の消費者は同じファイルを「期限切れでない」と
読んでいた。** 0133 §2 が「この製品が名指しで断ってきた形」と呼んだ
**黙って値を落とす**が、読む面から出口の面へ移っただけで残っていた。

**これは [0139](design/0139-ochakai-does-not-choose-a-spelling.md) で
畳んだ。** 入り(二つの綴りを取る)は動かさず、出口で
**ochakai が綴りを選ばない**ことにした — 手元にテキストがあればそれを
返し、瞬間で持っている `stale_after` の列だけを RFC 3339 で綴る。
自分のバンドルの日付も規格の綴りにした(値は動かない)。**ただし
他人の文書は正規化しない**(0139 §3.4): バイト列が持ち主のもので
あることが C1 であり、規格は既に一度、版を動かさずに normative を
変えている。**だから push が落ちる条件は残る** — 日付で書かれた文書を
取り込んだベースを export すれば、その三つの鍵は同じ 400 を受ける。
直す口は書き手の側(`POST /api/v1/frontmatter`)にある。

**二つ目 — 向こうが索引する `verified` 欄は、40 件すべてで空だった。**
demo の 18 concept はどれもトップレベルの `verified:` を持たない。検証の
申告は `received` の中にあり
([0009](design/0009-provenance-portability.md) §3.2)、`received` は
modeled でないので `extra` の JSON 文字列に落ちる。

**人の裁定を持つベースなら、向きは逆である。** export の `verified` は
常にこのインスタンスの台帳であり、文書が持ち込んだ申告は
`received.verified` に分かれている(0009 §3.2)。誰かが verify した
ベースを push すれば、その行は向こうの modeled な `verified` 欄に入る。
**測ったのは裁定が一つも無いベースである** — demo は
[サンドボックス](design/0087-a-sandbox-says-it-is-one.md)で、
[0066](design/0066-four-postures-one-word.md) §3 の下では誰も名前のある
人間になれない。

**そして削れないのはこちらである。** 向こうは `verified` に入っている
ものを写すだけで、**観測された裁定と、誰かがタイプした一行を区別できない**。
ochakai の中核は人の裁定が台帳に載ることだが、その区別は向こうの索引の
上では消える。バンドルは渡るが、**バンドルを渡す先が裁定を裁定として
扱うかは、渡した側が決められない**。

**だから足すものは無い。** 出口が OKF v0.2 のバンドルであることが、
そのまま Knowledge Catalog への入口になっている(C3 が買っていたものの
一つがこれである)。上の二つは足すものの話ではなく、**バンドルを渡した
先で何が読めるかの話**である。

**渡らないものを三つ書いておく。** `.md` でないファイルは残る(向こうの
README がそう書いており、[0131](design/0131-a-deployment-says-what-it-cannot-do.md)
のファイルはローカルのままになる)。本文のリンクは markdown のまま運ばれ、
カタログのネイティブなエッジにはならないので、`linked_from`
([0106](design/0106-a-read-carries-what-points-at-it.md))に当たるものは
向こうに無い。そして **`synonyms` は `extra` の JSON に入る** — ochakai では
索引が読む鍵([0105](design/0105-a-concept-answers-to-its-other-names.md))
だが、向こうでは検索できる欄ではなくなる。C8 の機構は、バンドルと一緒には
旅をしない。

**LLM Wiki にも入れた。** Karpathy が 2026-04 に gist で書いた LLM Wiki
パターン — 「Obsidian が IDE、LLM がプログラマ、wiki がコードベース」、
生の資料を LLM が wiki に*コンパイル*し、lint が矛盾と古い主張と孤立
ページを拾う — は、この節が言う vault の中で最も速く増えた形であり、
上の pi-llm-wiki と aws-samples/sample-okf-llm-wiki はその実装である。
**前提が ochakai と正反対である**: 向こうは LLM が wiki を維持することが
本体で、こちらは中に LLM を持たない
([0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md))。
その代表実装 [llm-wiki-compiler](https://github.com/atomicstrata/llm-wiki-compiler)
(2.0k、MIT、OKF の producer と consumer、レビューキュー、MCP サーバー)
の v1.1.0 に、2026-09-05、`examples/demo` の 18 concept と `kb/bundle` の
9 文書を `import --okf` で入れ、`lint` を通し、`export --target okf` で
戻した。

- **入口は LLM を呼ばず、既定で止まる。** import はファイル操作で、
  18/18 と 9/9 が skipped 0 で入る。既定では live にならず
  `.llmwiki/candidates/` に `imported-okf` の理由で積まれ、`review show`
  は本文を「UNTRUSTED(prompt injection の可能性)、provenance は
  unverified」と表示し、`review approve` が LLM を呼ばずに昇格する。
  「import は裁定しない」(v0.22.0)と同じ判断を、向こうは**止める**こと
  で実装している — こちらは書き込んで trust を unverified に置く。
  **却下の理由は残らない**: `review reject` は候補の JSON を `archive/`
  に移すだけで、reviewer も理由も裁定の時刻も書かず、中身は積まれた
  ときのままである。
- **`verified` は向こうでも一行になる。** OKF の `verified` /
  `generated` / `sources` / `status` / `synonyms` は live ページの
  `x-okf.originalFrontmatter` に畳まれ、ページ自身は
  `provenanceState: imported` になる。Knowledge Catalog と同じ結果で、
  台帳は文字列として渡る。向こうの信頼の単位は**生の資料の行範囲への
  引用**であり、`lint` は 18 文書中 13 に「引用の無い推論段落」の警告を
  出した — OKF §5.1 の `sources` と `[^id]` 脚注は引用として読まれない。
  エラーは 0 で、`lint` も LLM を要らない(要るのは `compile` である)。
- **戻りは壊れる。** 再 export の 18 文書のうち 13 で本文リンクが
  `[text](/path)](/path)` に二重化し(35 か所、元は 104 リンク)、
  `references/` が向こうの予約名なので `references/thelook-dataset.md`
  は `concepts/` へ**動かされ**、それを指す 8 リンクは元の住所を指した
  ままになった。0075 §2 の「住所はパスである」と
  [0129](design/0129-a-move-runs-when-its-rewrite-fits.md) の「リンクが
  収まらない move は動かない」が、向こうには無い。`timestamp:` は import
  した時刻(2026-09-05)になり、文書は持たなかった時刻を得る。`index.md`
  は `okf_version: "0.1"` を名乗る。**それでも ochakai の側は戻りを
  18/18 で読む**: 二重化したリンクは本文のまま入り、`verified` と
  `generated` と `timestamp` は 0009 §3.2 のとおり `received` に分かれ、
  `x-llmwiki` は producer のキーとして残る。壊れたのは本文であって、
  形式ではない。

**この節にとって新しいのは軸である。** 裁定を持つものはもう増えた(上の
KL4A と remnic)。LLM Wiki が持ち込むのは**コンパイラという役割**で —
資料から wiki を LLM が作り、lint が矛盾と古さを機械的に拾う — その後半
は ochakai の期限切れフィードと同型であり、前半は ochakai がエージェント
の側に置いた仕事である。ochakai に LLM Wiki から足すものは無く、LLM
Wiki のコンパイルの出口を OKF で ochakai に入れる道は上の測定どおり今日
も開いている — 本文リンクを除いて。

ファイルの側が持たないのは、ファイル形式でないすべてである:

- **単位。** ノートは誰かが自分の理由で書いた文書である。ochakai の
  concept は、自分のライフサイクル・`verified` の台帳・provenance を
  [OKF](design/0075-the-bundle-is-the-address-space.md) 自身のキーで
  運ぶ主張である。それらのキーは普通の YAML なので vault にも置ける —
  そしてまさにそこに違いが出る。vault では、人の名前が書かれた
  `verified` の concept は誰かがタイプした一行である。ここではそれは、
  認証された呼び出し元についてインスタンスが観測したことであり、文書
  から読み戻されることは決してない
  ([0009](design/0009-provenance-portability.md)、
  [0075](design/0075-the-bundle-is-the-address-space.md) §3.1) —
  ファイルには観測者がいない。vault の中の何一つ、その台帳に追記も
  しなければ、そこから trust tier を導きも、それに反する書き込みを
  拒みも、最後の確認が古びた concept を浮かせもしない。
- **ループ(C7)。** **この項は 2026-09 に狭くなった。** 大半には今も
  対応するものが無いが、*no* の記憶は KL4A が rationale ごとバンドルに
  持ち、失敗から訂正へ戻す経路は remnic が人の承認付きで持つ — どちらも
  「これが最も鋭い違い」ではもう無い。残るのは**測る側**である: 利用回数、
  検証の古さのフィード、concept に基づいて動いて間違いだったと分かった
  エージェントからの結果報告、そして**答えの無かった問い**
  ([ループ](loop.md)、
  [0069](design/0069-the-loop-and-what-measures-it.md) §1)。裁定を持つ
  ものは増えたが、**裁定の効き目を数で持つものは、まだ見ていない。**
- **向き。** vault は人が人のために書き、エージェントは読み手である —
  最近は書き手でもあるが、監査もされず測られもしない。ochakai の賭けは
  逆で、エージェントが幅を下書きし、人が判断の要る中核を検証する。
- **複数の書き手。** vault はフォルダであり、その並行性の話はファイル
  同期である。共有された vault は衝突を生む。ochakai は ETag /
  `If-Match` の楽観ロック([0030](design/0030-optimistic-locking.md))を
  持つサーバーであり、人が裁定したものをエージェントが上書きできない
  という面の規則を持つ
  ([FAQ](faq.md#can-an-agent-overwrite-or-delete-knowledge-a-human-verified))。
- **identity、そして secret が無いこと(C2)。** vault の MCP サーバーは
  たいてい、クライアントの設定に API キーを書いたローカル REST API
  プラグインの後ろで動く — それこそ ochakai の設計が拒んでいる当の
  トークンである([0065](design/0065-identity-and-provenance.md) §1、
  [0003](design/0003-gcp-only.md))。そして呼び出し元には記録すべき
  identity が無い: 「これを書いたのは誰か」は「誰のノート PC だったか」
  である。
- **到達(C5、C6)。** vault は一台のマシンの上にあり、その MCP サーバー
  も同じである。ホストされたエージェント、CI ジョブ、そして自分の Web
  サービスに埋め込める REST API は、そこには無い。
- **検索。** 埋め込みを既定でオンにした hybrid search
  ([0080](design/0080-search-and-how-a-deployment-embeds.md) §1)、住所で
  絞る検索、画像や PDF の中身に対するファイル検索 — 人のブラウズでは
  なくエージェントの問いの形に合わせた読み取りである。
- **型。** vault は型に無関心であり、それはノートには正しく、ここでは
  代償になる: このファイルが検証済みの golden query で、あちらが議事録
  だと、エージェントに言うものが何も無い。

だから両立は本物で、しかも異例なほど文字どおりである。vault は人が
考えて書く場所であり、ochakai はチームの検証済みデータナレッジが住み、
エージェントに配られる場所である — そして保存形が同じ markdown である
以上、export → Git → レビュー → import のループは、途中で vault を
通ることができる([Git をレビュー経路にする](guides/git-review.md))。

## ochakai が負けるところ

はっきり書く。利点しか見つからない評価は、評価ではないからである。

- **書く体験。** Obsidian とそのエコシステムは、書く場所としても眺める
  場所としてもはるかに優れている。同梱の Web UI は方針としてキュレー
  ションの面であって、執筆環境でも BI ツールでもない
  ([0067 §1](design/0067-four-faces-and-what-they-decline.md))。
- **Google Cloud なら純正がある。** 上の Knowledge Catalog は同じ IAM の
  上で立てるものが無く、収集は自動で、MCP も持っている。ochakai の
  プロジェクトと Postgres と月 $10 とセットアップは、人の裁定・却下の
  理由が残ること・OKF の出口に対して払う分であって、その三つが要らない
  なら払う理由も無い。
- **インフラ。** vault を立てるのはただである。ochakai は Google Cloud
  プロジェクトと Postgres を要る — 月 $10 ほどだが、ゼロではないし、
  5 分でもない。
- **Google Cloud 推奨、外は認証だけ。** C2 の secret-zero という性質は
  Cloud Run IAM と Cloud SQL IAM で無設定に買われている
  ([0003](design/0003-gcp-only.md))。外で動かす道は認証には開いた —
  OIDC 発行者を名指せば ochakai が公開鍵で自分で検証し、secret は
  増えない([0086](design/0086-a-second-way-to-say-who-is-calling.md))
  — が、埋め込みは Vertex AI か無いかで検索は lexical のみになり、
  データベースの資格情報は運用者の仕事に戻る。失格要件が一つ消えた
  だけで、Google Cloud の外が快適になったわけではない。
  **ただし「lexical のみ」がどれだけ落ちるかは、推測ではなく測ってある。**
  CI の golden set は同じ 88 問を lexical のみでも走らせており、無関係な
  二つのドメインにまたがる 29 concept のベースで recall@10 = 1.00、
  MRR = 0.91(74 問が 1 位、85 問が 3 位以内)である
  ([CONTRIBUTING](../CONTRIBUTING.md) の *The search eval harness*)。
  日本語を二文字窓で引くのは lexical 側の機構である
  ([0080](design/0080-search-and-how-a-deployment-embeds.md) §1)から、
  そうなるのは設計上の筋でもある。**二つ添えて読むこと。** ベースは
  29 concept で、下の「規模」が言う数千ではない — 数が増えれば希釈は
  効くので、これは上限に近い floor であって予測ではない。そして埋め込みが
  何を足すかはこの数字では測れない(fused 側の encoder は lexical と語彙を
  共有する代役である)。言えるのは「外でも日本語の検索は立つ」であって、
  「埋め込みは要らない」ではない。
- **認可は既定で無く、あってもディレクトリ単位である。** 何も書かなければ
  デプロイに到達できる者がすべてを読み書きする
  ([0065](design/0065-identity-and-provenance.md) §1)。ディレクトリごとの
  閲覧者・編集者は書ける([0109](design/0109-a-directory-has-readers-and-writers.md))
  が、concept ごとの権限も役割(role)も無く、読める concept の本文が
  隠した id を名指していればその id は読める(0109 §4.1)。**境界が要る
  ときの最初の答えは認可ではなくデプロイを分けることで**、それで足りない
  のはチームどうしが語彙を共有しているときである([FAQ](faq.md#who-can-read-and-write))。
- **規模。** **キュレーションされた**ベースが届く大きさのために作られて
  いる — 数百万ではなく数千の concept である。一つ残らず人を通っている
  からである。
- **エコシステム。** 維持者は一人、バイナリは一つ、プラグインも
  マーケットプレイスも無い。

## 選ぶ

- Google Cloud で BigQuery を使っていて、足りないのが技術メタデータ・
  lineage・自動生成の説明と example query なら → Knowledge Catalog。
  純正で、立てるものが無い。
- MCP サーバー付きのカタログを既に運用していて、エージェントが取り
  こぼしている問いがリネージとオーナーシップなら → カタログで足りる。
- ウェアハウスが一つで、足りないのがメトリクス定義なら → そのウェア
  ハウス自身の semantic layer。
- 足りないのがエージェントが*あなた*を覚えていること — 好み、進行中の
  文脈 — なら → メモリ層。
- 一人、一台、自分で書くノートなら → OKF ネイティブのローカルツールか
  vault と、その上の MCP サーバー。
- オントロジーの約束には頷くが、プラットフォームと FDE という買い方を
  断りたい — そして kinetic の実行(Action、write-back)をエージェントに
  任せられるなら → semantic の中核と kinetic の定義を最小に持つ、それが
  この枠である。
- **エージェントと人が複数いて、足りないのが誰かが確かめたナレッジ** —
  どのクエリが sanctioned か、その数字が何を意味するか、何が既に試され
  却下されたか — であり、Google Cloud で動かせるなら → それがこれの
  埋める枠である。

## いつ戻ってくるか

上の一覧が答えるのは**いま**どれを選ぶかである。評価している人の多くに
とって正しい答えは「まだ要らない」であり、そのときに役に立つのは、何が
起きたらこの枠に入るのかという条件のほうである。2026-08 に 41 組織の
データエージェント利用を調べたときに繰り返し現れたものを四つ挙げる
([調査記録](https://github.com/na0fu3y/ochakai/issues/703))。

- **単一ベンダーのチャットに閉じないと決めたとき。** 二つ目の
  クライアント — 別のアシスタント、CI、自分のサービス — が同じ検証済み
  クエリを要求した瞬間に、ベンダーの中の検証済みクエリストアは原本の
  置き場ではなくなる。上の「重なる、そしてロックインする」が実務で
  効き始めるのはここである。
- **ナレッジの書き手が二人を超えたとき。** vault 節が数えたもの —
  検証の台帳、observer としての identity、*no* の記憶、並行する
  書き込み — は、一人のうちは一つも要らず、二人目から毎日要る。
- **「この数字の根拠クエリを誰がいつ確かめたか」を監査が訊いたとき。**
  ベンダーの中の verified フラグは、その問いに答える台帳ではない。
- **キュレーションした資産の置き場が不安になったとき。** 定義層の
  標準化([Ossie](https://github.com/apache/ossie))は provenance を
  運ばないので、丸ごと出て丸ごと戻ること(C1・C3)がそのまま出口になる。

どれも起きていないなら、上で選んだものを使い続けるのが正しい。

## この調査がどこにあるか

このページは生きた答えであり、代替が変われば変わることを前提にして
いる。その裏にある調査の日付入りスナップショットは
[0001](design/0001-architecture.md) §2 で取られた — カタログが
コンテキスト層になっていることとウェアハウス native の semantic layer に
ついての 2026-07-16 のノート、そしてメモリ層についての 2026-07-17 の
ノートである。0001 は [0081](design/0081-what-ochakai-is-and-what-it-refuses-to-hold.md)
が置き換えたので、あの二つのノートは墓標が指す全文のコミットにある —
**日付の付いた調査は書き換えないので、動かすのではなく置いてきた。**
landscape は決定ではないので、代わりにここで追う。
