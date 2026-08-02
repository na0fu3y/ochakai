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

番号付きドキュメントを要するのは、**利用者が観測できるものを変える決定**
だけである(ワイヤの形、保存形と往復、identity と provenance の意味、
Google Cloud への依存、意図してやらないこと)。外形の変わらない内部変更は
PR の説明で足り、まだリリースに乗っていない決定の改訂は新しい番号ではなく
元のドキュメントの差し替えになる — 基準は
[0048](0048-decision-records-for-wire-contracts.md)。

## 現行ドキュメント早見表

**改訂の履歴を追わずに現在の姿にたどり着くための表**(0048 §2.4)。
各領域について、いま読むべきドキュメントだけを挙げている。ここに無い
番号は Superseded か、本体の `Status:` ヘッダが行き先を書いている。

| 領域 | いま読むドキュメント |
|---|---|
| 全体アーキテクチャ | [0001](0001-architecture.md) |
| Google Cloud 前提・secret-zero | [0003](0003-gcp-only.md) |
| 認証と identity | [0002](0002-authn-authz.md)、[0027](0027-delegated-provenance.md)(委譲)、[0052](0052-producer-beside-the-actor.md)(producer)、[0032](0032-webui-iap-identity.md)(IAP)、[0040](0040-read-only-mode.md)(read-only)、[0042](0042-public-read-only.md)(公開読み取り専用)、[0060](0060-one-word-for-the-posture.md)(姿勢の綴り) |
| OKF 互換・バンドル・保存形 | [0046](0046-bundle-address-space.md) が現行。[0061](0061-a-dry-run-is-the-write-withheld.md)(書き込み面の dry run)、[0047](0047-fm-carries-okf-keys.md)(フィルタの語彙)、[0037](0037-stale-and-source-lookup.md)(期限と引用元)、[0024](0024-links-from-body.md)(リンク)、[0022](0022-filename-as-name.md)(名前)、[0019](0019-release-review-adjustments.md)(ID 文字種) |
| 住所とパス | [0046](0046-bundle-address-space.md) §§3.1・3.5 と [0064](0064-rest-stops-at-api-v1.md) §5 が現行(バンドルのパスが住所、面は `/api/v1/bundle/{path}` 一本、`.md` は必須でバンドルパスの一部)。[0017](0017-path-addressing.md)(「パスが住所、タイプは属性」の決定そのもの)、[0041](0041-path-scoped-search.md)(prefix)、[0021](0021-move-and-webui-refinements.md)(move) |
| 型の語彙 | [0063](0063-two-unused-recommended-types-leave.md)。改訂の履歴は [0038](0038-type-vocabulary-realignment.md) |
| 知識の単位の呼び名 | [0057](0057-concept-is-the-word-a-reader-meets.md)(ツール名・読む語)、[0064](0064-rest-stops-at-api-v1.md) §7 が現行(JSON フィールド名 `entries` → `concepts`) |
| 添付ファイル | [0046](0046-bundle-address-space.md)(バンドルのオブジェクト)、[0020](0020-attachment-search.md)(検索) |
| 検索と埋め込み | [0053](0053-embeddings-by-default.md)(既定・次元変更)、[0058](0058-filters-nobody-arrived-through.md)(スコアの床は持たない)、[0020](0020-attachment-search.md)(添付)、[0041](0041-path-scoped-search.md)(prefix)、[0033](0033-context-hits-are-a-ranking.md)(context) |
| サーフェスの配分 | [0015](0015-surface-consistency.md)、[0056](0056-one-question-one-command.md)(CLI の第二の住所を畳む規則)、[0062](0062-a-listing-is-not-a-search.md)(一つのコマンドが二役なら割る規則)、[0058](0058-filters-nobody-arrived-through.md)(通行量の無い入口を降ろす規則)、[0004](0004-cli.md)(CLI)、[0007](0007-api-only-cli.md)、[0033](0033-context-hits-are-a-ranking.md)(context)、[0039](0039-mcp-stdio-bridge.md)(MCP stdio)、[0050](0050-listings-page-rankings-do-not.md)(一覧と順位の分け方) |
| Web UI | [0006](0006-web-ui-serving.md)(配信)、[0044](0044-web-ui-edits-documents.md)(編集)、[0032](0032-webui-iap-identity.md)(identity) |
| 検証ループと利用測定 | [0056](0056-one-question-one-command.md)(裁定の面・綴り)、[0025](0025-closing-the-loop.md)、[0029](0029-usage-recording-off-the-read-path.md)、[0037](0037-stale-and-source-lookup.md)、[0049](0049-queue-counts.md)(キューの長さ)、[0059](0059-a-queue-is-named-by-its-listing.md)(キューの名前)、[0051](0051-instance-metrics-and-search-misses.md)(インスタンスの指標と検索ミス)、[0050](0050-listings-page-rankings-do-not.md)(一覧のページング)、[0062](0062-a-listing-is-not-a-search.md)(一覧の CLI 面) |
| 同時実行と削除 | [0030](0030-optimistic-locking.md)、[0031](0031-purge.md) |
| 実装の品質ゲート | [0035](0035-verifiability.md) |
| 決定の書き方 | [0048](0048-decision-records-for-wire-contracts.md) |
| バンドル往復と provenance の所有権 | [0046](0046-bundle-address-space.md) §2.2 が現行(主張と観測の分離)。[0009](0009-provenance-portability.md)(**Proposed** — 唯一の未採択) |
| やらないと決めたこと | [0028](0028-retire-compile-sql.md)(compile_sql)、[0018](0018-semantic-model-as-knowledge.md)(専用機構)、[0012](0012-retire-mcp-oauth-connector.md)(OAuth コネクタ) |
| REST の安定性契約 | [0064](0064-rest-stops-at-api-v1.md)、[docs/compatibility.md](../compatibility.md) |

英語話者向けに、各ドキュメントの決定と利用者への影響を要約した
[README.en.md](README.en.md) を置いている。**正典は各ドキュメント本体**で、
要約が食い違えば本体が正しい。新しいドキュメントを足したら要約も足す —
足し忘れは `TestEnglishDesignIndexCoversEveryRecord` が落ちる。

この index と各ドキュメントの `Status:` ヘッダの食い違いは
`cmd/ochakai/designdocs_test.go` が落とす(0035)。1 つの決定が触る
4 箇所(0048 §1)のうち、**この index にエントリがあること**、**両方の
index の現行 / Superseded の表示が本体のヘッダと一致すること**、
**Superseded にした側とされた側・改訂した側とされた側の両方にその記録が
あること**が対象で、覚えていることが要るのは残りの 1 つ — 早見表の行が
指す先が本当に「いま読むべきドキュメント」か — だけになる。

だから**下の各節は、そのドキュメントが決めたことだけを書く**。何が後から
改訂・Superseded されたかは本体の `Status:` ヘッダが必ず持っており、ここに
写せば二つ目の写しが増えて食い違うだけになる。読む先が変わるものは上の
早見表が答える。

## アーキテクチャと基盤

- [0001 全体アーキテクチャ](0001-architecture.md) — **Accepted**。
  LLM を内蔵せず SQL を実行しないナレッジストアという中核。双方向ループ
  (エージェントが下書きし、人が検証する)、Postgres + pgvector 一本、
  利用テレメトリ。
- [0003 Google Cloud 前提化](0003-gcp-only.md) — **Accepted**。
  Cloud Run + Cloud SQL IAM を前提にし、トークン・パスワードを持たない
  (secret-zero)。
- [0053 埋め込みは既定であり、ベクトル空間は捨ててよい](0053-embeddings-by-default.md)
  — **Accepted**。Google Cloud の上ではプロジェクトをメタデータサーバから
  発見して意味的検索を既定で有効にし、使えるかどうかは設定ではなく IAM が
  決める(起動時に 1 回だけ訊く)。発見された既定は Vertex AI や pgvector が
  無ければ字句検索に落ち、名指しされた設定は従来どおり起動を拒否する。
  off は `OCHAKAI_EMBEDDINGS=off` ただ一つ。次元を変えたときの手作業の
  `DROP TABLE` を廃し、**ベクトルは導出物である**という理由で製品が作り直す
  (詰め直しは `ochakai reembed` のまま — 支払いは頼まれてから)。添付の
  昇格は 0046 で完了しており何も足さない。

## 実装の品質ゲート

- [0035 不変条件を機械に検査させる](0035-verifiability.md) — **Accepted**。
  型に載らない不変条件(網羅性・wire 契約・往復の正確さ)を、
  golangci-lint / openapi 契約テスト / fuzz の三層で外から検査する。
  linter の選定基準は「clean tree で 0 件」。

## 決定の書き方

- [0048 決定記録はワイヤ契約の決定に絞る](0048-decision-records-for-wire-contracts.md)
  — **Accepted**。番号付きドキュメントを要するのは利用者が観測できるものを
  変える決定だけ(ワイヤ・保存形・identity・GCP 依存・refusal)とし、
  外形の変わらない内部変更は PR に置く。**まだリリースに乗っていない決定の
  改訂は、新しい番号ではなく元のドキュメントの差し替え**(0034 の先例を
  規則にする)。既存 47 本は書き換えず、代わりに index に現行ドキュメントの
  早見表を置き、英語要約の維持義務を現行ドキュメントだけに縮める。

## 認証・認可と provenance

- [0002 認証・認可](0002-authn-authz.md) — **Accepted**。認可機構を
  持たない —「到達できた者は読み書きできる」。ヘッダから読むのは
  provenance だけで、信頼の判断は参照側が provenance を見て行う。
- [0040 デプロイ単位の read-only](0040-read-only-mode.md) —
  **Accepted**。デプロイ全体を読み取り専用にする。
  呼び出し元を区別しないので 0002 の「認可を持たない」は維持される。
  強制点はサービス層 1 箇所、各面は自分の語彙で告げる(REST は 403 と
  ヘッダ、MCP は書き込みツールを出さない)。
- [0042 公開読み取り専用という姿勢](0042-public-read-only.md) —
  **Accepted**。誰でも到達できるデプロイのために、identity を一切読まない
  (トークンも委譲ヘッダも見ない、全員 anonymous、401 を返さない)。
  read-only を含意するので「公開かつ書き込み可能」は設定として存在しない。
- [0060 姿勢は一語で言う](0060-one-word-for-the-posture.md) —
  **Accepted**、**BREAKING**。`OCHAKAI_READ_ONLY` /
  `OCHAKAI_PUBLIC_READ_ONLY` / `OCHAKAI_INSECURE_DEV` の 3 つのブールを
  `OCHAKAI_MODE`(未設定 / `read-only` / `public` / `dev`)一本に畳む。
  0040 と 0042 が**決めたことは何も変えない** — 3 つのブールが書き下せた
  8 通りのうち意味を持つのは 4 つだけで、差は規則 3 つ(黙って補正する
  含意 2 つと、起動を拒否する組み合わせ 1 つ)で埋められていた。実際に
  あるのは「呼び出し元が特定されるか」「誰かが書けるか」の 2 軸 4 象限で
  あり、`INSECURE_DEV` が姿勢に見えなかったのは名前が機構を言っていた
  からである(§1.1)。**規則は消えるのではなく、書き下せなくなる。**
  読めない綴りは既定に落とさず起動エラーにする(0053 §2.4 と同じ理由)。
  ENV は 16 → 14 で、姿勢の 3 綴りは VOCAB に数えない — 姿勢はデプロイ
  する人の語であり、それは ENV が既に数えている(§4)。
- [0027 呼び出し元によるエンドユーザー identity の委譲](0027-delegated-provenance.md)
  — **Accepted**(ヘッダの綴りは 0064 が `X-` を落として改訂)。信頼済みの
  呼び出し元が `Ochakai-On-Behalf-Of` でエンドユーザーを名乗り、
  provenance がサービスアカウント 1 つに潰れる問題を解く。
- [0052 名乗りは actor の隣に置く](0052-producer-beside-the-actor.md)
  — **Accepted**(ヘッダの綴りは 0064 が `X-` を落として改訂)。0043 §3.8
  (0046 §2.4 が継承)の「SPEC §7 の `<producer>/<version>` は使わない」を
  改訂し、`Ochakai-Producer` / MCP の `clientInfo` / `OCHAKAI_PRODUCER`
  から来る自称を `Actor.producer` として**認証済みの actor の隣に**記録
  する。actor の綴りは `human:` / `process:` のまま、trust tier も不変。
- [0009 OKF/Git 往復と provenance の所有権](0009-provenance-portability.md)
  — **Proposed**。export → Git → import の往復で provenance が誰のものに
  なるかの整理(バンドルは知識のみを運び、provenance はインスタンス固有)。
  文書の trust family は台帳にも trust tier にも入らないまま、
  `received:` の下に主張として保存される(0046 §2.2)。

## ナレッジモデル(構造・ID・型・名前・リンク)

- [0047 問いの語彙は OKF が定義するキーである](0047-fm-carries-okf-keys.md)
  — **Accepted**。0046 §3.11 を 2 点改訂する。「named フィルタは式への
  糖衣」は採らず typed column を残し、`fm.type` / `fm.status` /
  `fm.tags` / `fm.sources` / `fm.stale_after` は 400 で拒否する
  — 同じキーが綴りによって別の問いになる状態(沈黙した `status` は
  `status=stable` に当たり `fm.status=stable` に当たらない)を、解決では
  なく除去するため。あわせて `fm.` を **OKF が定義するキー**に閉じる
  — producer が発明したキーは保存され往復するが引けない。開いた語彙は
  0 件が答えなのか綴り間違いなのかを利用者に判定させず、引けるキーの
  一覧をどの面にも書けなくするからである。引けるキーは
  `domain.EnvelopeKeys` から導出するので、OKF が足したキーは列も
  マイグレーションも無しで引ける(0046 §3.11 の目的)。拒否はサービス層
  1 か所で、REST / MCP / CLI が共有する。0015 §3 に「`fm.` は Web UI に
  載せない」という省略を 1 件足す(§4)。**未リリースにつき、同番号の
  初版を 0048 §2.3 に従って差し替えた**(その規則の最初の適用)。
- [0061 dry run は書き込みを止めたもの](0061-a-dry-run-is-the-write-withheld.md)
  — **Accepted**。`PUT /api/v1/bundle/{path}` に `dry_run` と
  `Ochakai-Plan` を足し、**書き込み経路そのものに「書かないで答えろ」と
  言う**。`ochakai import --dry-run --strict` は CI の関門として文書化
  されていたのに、`--strict` が落ちる理由の半分 —— trust family が
  `received` の claim として残るか(0046 §2.2 の判定は**保存済みとの
  比較**である)、サーバがその文書を拒むか、書き込みが何も変えないか
  —— がバンドルからは見えず、**関門が通って本番 import が落ちていた**
  (§1)。クライアント側で作り直せば表面は増えないが、増えないのは表面
  だけで、増えるのは書き込み判断の第二の実装である(0007、§2.1)。
  判定は `settleUpdate` を `Update` と `Plan` が共有して一つの経路から
  出す(§3.3)。dry run は 200 のみ・ETag 無し・DELETE では 400、
  ヘッダではなくクエリパラメータ(**落とされたヘッダは書き込みになる**)。
  副産物として dry run が created / updated / unchanged を出し分ける。
  PARAM 18 → 19、HEADER 9 → 10 —— **天井を二つ上げる決定**であり、
  増えるのは口であって能力ではない(§4)。
- [0046 バンドルがアドレス空間](0046-bundle-address-space.md) —
  **Accepted**。**OKF 互換領域の現行ドキュメント**。バンドルを
  「パス → オブジェクト」の写像そのものとし、概念でないファイルも
  往復させる(取り込みで消えるものが無くなる)。保存は**受け取った
  バイト列**で正準形は導出値、`index.md` / `log.md` は SPEC §8 / §9 の
  導出面(browse と revisions を吸収)、本文リンクは SPEC §6 の形だけ、
  trust は SPEC §5.3 の 3 段、索引は frontmatter の jsonb。REST は
  `/api/v1/bundle/{path}` を中心とした 8 面に畳まれ、添付という概念が
  対象消滅する。
- [0043 文書を真とする](0043-document-first.md) —
  **Superseded by 0046**。保存もワイヤも正準化した OKF 文書
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
  — **Accepted**。0036 が封筒に載せた
  `stale_after` / `sources` を引けるようにする: `sort=stale_after` の
  期限切れフィード(verify ではなく編集で空になる — 0025 の 2 フィードとの
  違いは §2.2)と、引用元からの逆引き `source` フィルタ(+ GIN index)。
- [0036 OKF のスキーマを真とする](0036-okf-schema-first.md) —
  **Superseded by 0043**(document-first への全面置き換え)。
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
  **Superseded by 0036**。OKF バンドルとの双方向互換の出発点 — 任意ネストの
  パス、自由文字列タイプ、バンドルインポート、サーバー所有キー。
- [0016 knowledge-catalog リファレンスバンドルへの準拠](0016-knowledge-catalog-alignment.md)
  — **Superseded by 0036**。リファレンスバンドルへの準拠
  (`resource` の封筒フィールド化ほか)。
- [0017 パスが住所、タイプは属性](0017-path-addressing.md) —
  **Accepted**。ID がアドレスであり、パスはもはや型を主張しない。
- [0019 v0.10.0 リリース前の整合調整](0019-release-review-adjustments.md)
  — **Accepted**。ID 文字種の OKF 同等化、バンドルインポートは拒否文書を
  スキップしてレポート。
- [0022 ファイル名が名前](0022-filename-as-name.md) — **Accepted**。
  title の任意化、ID の検索対象化、NFC 正規化。
- [0023 型の語彙を OKF に一本化する](0023-okf-type-vocabulary.md) —
  **Superseded by 0038**。内部スラグと OKF 表示名の二重語彙を
  廃止し、型の値は OKF の綴りそのものに。
- [0038 推奨型の語彙を OKF の証拠に合わせ直す](0038-type-vocabulary-realignment.md)
  — **Accepted**。`Semantic Model` と `Golden Query` を外し、`Skill` /
  `Playbook` / `Policy` / `API Endpoint` を足して 11 型に(退役した綴りは
  自由型として存続、マイグレーションなし)。語彙を述べる箇所を
  `domain.TypesHint()` と外側のテストで固定する。
- [0063 使われていない2つの推奨型を外す](0063-two-unused-recommended-types-leave.md)
  — **Accepted**。**型の語彙領域の現行ドキュメント**。0038 が足した
  `Playbook` と `API Endpoint` を推奨語彙から外し、11 型を 9 型に。
  ochakai 自身の `examples/` にも OKF 公式のリファレンスバンドルにも
  一度も書かれていなかった 2 語で、SPEC の例示だけを根拠にしていた
  (issue #353)。型集合は閉じないまま、退役は自由型として存続、
  マイグレーションなし。
- [0057 concept は読む人が出会う語でもある](0057-concept-is-the-word-a-reader-meets.md)
  — **Accepted**、**BREAKING(文言のみ)**。**知識の単位の呼び名領域の
  現行ドキュメントの一つ**(0054 の決定を §0 に統合する)。0054 は MCP
  のツール名 5 本の `knowledge` を OKF SPEC §2 の語 `concept` に改めた
  (`search_concepts` 等、ツールは 8 本のまま)。0057 はその数え方が
  取りこぼしていたのが**利用者が読む英語**だったと気づく — 0.16.0 の直後で
  英語ドキュメントは `entry` 287 回に対し `concept` 6 回、`get_concept`
  自身の説明が「Fetch one entry」、Web UI のボタンが「New entry」で
  ある。翻訳表は消えずにツール名からドキュメントへ移っていた(§1)。
  読む語をすべて `concept` にする — README・docs・examples・deploy の
  英語、CLI のヘルプと出力、Web UI のラベル、MCP のツール説明と
  jsonschema、OpenAPI の description、エラー文(§3.1)。**境界は
  「読む語」と「識別子」**で、Go の型名(0054 §3.4)や DB の列名、
  そして英語として別の意味の "entry"(catalog entry、`sources` の要素、
  changelog の項目、CSS の `.tree-entry`)は動かさない(§3.2)。0057 自身は
  ワイヤを 1 バイトも動かさない。

- [0054 知識の単位は concept と呼ぶ](0054-concept-is-the-okf-word.md)
  — **Superseded by 0057**。MCP のツール名 5 本の `knowledge` を OKF
  SPEC §2 の語 `concept` に改めた最初の決定。0057 §0 に吸収された。

- [0024 リンクは本文から導出する](0024-links-from-body.md) —
  **Accepted**。構造化 `links` フィールドを廃止し、リンクは本文 markdown
  から導出する。

## 添付ファイル(0046 でバンドルのオブジェクトになった)

- [0008 画像添付](0008-image-attachments.md) — **Superseded by 0046**。
  画像添付の導入。「原本ではなく根拠」の位置づけと `<id>/<name>` は、
  0046 では帰属の**導出規則**として残る。
- [0011 添付画像バイト列の GCS 移行](0011-gcs-attachment-storage.md) —
  **Superseded by 0046**。バイト列を GCS に置くという判断だけが残る。
- [0013 添付ファイルの一般化と GCS 一本化](0013-attachment-files-gcs-only.md)
  — **Superseded by 0046**。任意ファイル形式への一般化と GCS 一本化。
  sniff によるメディアタイプ判定と markdown-only 運用は 0046 も維持し、
  SVG / HTML の書き込み拒否だけが配信側の防御に置き換わる。
- [0020 添付ファイルの検索](0020-attachment-search.md) — **Accepted**。
  ファイル名のレキシカル一致と全ファイルのベクトル検索。

`attachments` / `Attachment` という wire 上の綴りは 0046 §2.1 が概念と
しては既に対象消滅させていたが、名前だけ残っていた。**その綴りの retire
自体は [0064](0064-rest-stops-at-api-v1.md) §6 の決定**であり、この節の
どのドキュメントも改訂しない — 保存モデルは 0046 のまま、動いたのは
JSON キーと MCP ツール名だけである。`change` の語彙に残っていた
`attach` / `detach` という最後の綴りは 0064 §8 が仕上げる(issue #470)。

## ブラウズと Web UI

- [0006 Web UI の二つの配信経路](0006-web-ui-serving.md) — **Accepted**。
  自己完結 1 ファイルの UI を、認証モデルの異なる二経路
  (`ochakai ui` / `ochakai serve-ui`)で配信する。
- [0014 フォルダブラウズ](0014-folder-browse.md) — **Superseded by 0046**。
  prefix によるツリー閲覧。
- [0021 ナレッジの move と Web UI の微調整](0021-move-and-webui-refinements.md)
  — **Accepted**。move(パス変更)の導入と表示の整理。
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

- [0004 リモート CLI](0004-cli.md) — **Accepted**。REST API の薄い
  クライアントとしての CLI。
- [0007 DB 直結コマンドの廃止](0007-api-only-cli.md) — **Accepted**。
  データの出し入れを API 経由に一本化。DB 直結で残るのは `serve` のみ。
- [0015 サーフェス一貫性の方針](0015-surface-consistency.md) —
  **Accepted**。4 サーフェスの役割分担と、意図して実装しないもの。
- [0058 誰も通らなかった二つのフィルタ](0058-filters-nobody-arrived-through.md)
  — **Accepted**、**BREAKING**。**第二の住所ではなく、最初から通行量の
  無かった入口を降ろす**二件。`min_score`(REST・CLI)は**廃止** —
  スコアはモード依存で較正されておらず(0051 §3.1 が同じ理由で閾値を
  採らなかった)、同梱の唯一の利用者である recall フックが既定 0 = off
  で渡していた。応答を絞る手段は較正の要らない `budget` が既に持って
  いる。`fm.` は **MCP からだけ降ろす**(REST と CLI には残るので
  0047 の語彙も導出も不変)— 同じ 944 文字の説明が 2 ツールに載って
  スキーマ説明文の 14%(1,888 / 13,099 文字)を占め、接続するエージェント
  が毎回払っていたのに、Web UI にも例にもフックにも通行の跡が無かった。
  **ツール数は 8 のまま、エージェントが払う額だけが減る**(§4)。
- [0064 REST は /api/v1 で止まる](0064-rest-stops-at-api-v1.md) —
  **Accepted**。**REST の安定性契約領域の現行ドキュメント**。REST を凍結
  する前の最後の一括破壊的変更(issue #379)。不明なクエリパラメータは
  全操作で 400(`fm.` を除く)、リクエストヘッダの `X-` 接頭辞を撤去、
  ファイルの住所が `Accept: application/json` でメタデータを返すように
  なり sha256 をバイト列無しで読める、`attachments` / `Attachment` を
  wire から `files` / `File` に retire、`stats`・バンドル一覧・`context`
  の JSON キー `entries` を `concepts` に改名(§7、0057 §3.2 の除外を
  撤回、issue #411)、`change` の語彙に残っていた `attach` / `detach` を
  `add_file` / `remove_file` に改名(§8、マイグレーション付き、issue
  #470)、`context` の冗長な `truncated` を撤去・バンドル一覧のファイル
  行を `updated_at` から `created_at` に・`reembed` の `embedded` を
  `concepts` に(§9、issue #470)、If-None-Match の衝突は 412、DELETE が
  If-Match に対応、ファイル PUT が作成時 201、withdrawn の空振りは 409、
  `limit` / `days` の範囲外は全面 400。バージョンや capability の合図は
  無し、CORS も意図して無し。凍結を破ってよい理由はセキュリティ上の欠陥
  だけ(§11)、DELETE の再実行は 404 のままで安全(§11)。
  [0046](0046-bundle-address-space.md) §3.5 の代表表現の表を改訂する
  (概念の既定は JSON の View)。[docs/compatibility.md](../compatibility.md)
  を同 PR で書き換え、REST だけが「不安定」の対象から外れる。
- [0033 context の hits は順位に徹する](0033-context-hits-are-a-ranking.md)
  — **Accepted**。バイト予算の決定を記録し、`hits` から知識の複製を外す
  (全サーフェスで `id`/`type`/`title`/`status`/`score` のみ)。REST の
  応答形が変わる。

## 検証ループと利用測定

- [0050 一覧はカーソルでページングし、順位はしない](0050-listings-page-rankings-do-not.md)
  — **Accepted**。フィード(0025 §6、0037 §2.1)と source 逆引き
  (0037 §2.3)に不透明な keyset `cursor` を足し、`limit` の上限で
  キューが静かに切れる状態を無くす。**検索は `limit` が契約のままで、
  `cursor` を 400 で拒否する** — 順位は融合の窓であって再開できる順序では
  ないという refusal(§2.2)。総件数は返さず、`cursor` の不在が終わりを
  意味する(§2.3)。REST / MCP / CLI / Web UI の 4 面に載り、CLI が
  ページを自分で歩かないことだけが意図的な省略(§4)。
- [0062 一覧は検索ではない](0062-a-listing-is-not-a-search.md)
  — **Accepted**、**BREAKING**。`ochakai search --sort` を廃し、一覧を
  `ochakai list [feed]` に分ける。0056 の系の逆向き — **一つのコマンドが
  二つの能力を持つなら、コマンドも二つ**。`search` は順位(クエリ必須、
  `--limit` 10/50、cursor 無し)、`list` は全順序(フィードか逆引き、
  `--limit` 100/1000、`--cursor` で進む)で、0050 が別物と決めた二つが
  CLI でも別のコマンドになる。**ワイヤは動かない**(`sort=` も
  `search_concepts` もそのまま)。語彙も動かない(`sort.*` の 4 語が
  フラグ値からコマンド引数へ移るだけ)。CLI 25 → 26、FLAG 29 → 28。
  `stats` が印字するコマンド文字列は `ochakai list …` になる
  (0049 §3.4・0059 の印字形式を改訂)。
- [0056 一つの問いに一つのコマンド、一つの裁定に一つの語](0056-one-question-one-command.md)
  — **Accepted**、**BREAKING**。**裁定の面・綴り領域の現行ドキュメント**
  (0055 の決定を §0 に統合する — REST の三つの裁定面を
  `POST /api/v1/review/{id}` 一本にした決定)。ワイヤで畳んだ面が CLI にコマンドとして
  残っていた二件を畳む: `ochakai backlinks` は
  `ochakai search --links-to`(0046 §3.5 が REST 側を畳んだ先)へ、
  `ochakai queues` は `ochakai stats` へ。**失われた能力は無い** —
  各行が次に打つコマンドを持つことも `--exit-code` も `stats` に移り、
  クエリ無しの `--links-to` はアドレス順の一覧のままである(§3.1、§3.2)。
  第二の面が高くついていた証拠として、`backlinks --limit` のヘルプは
  既に嘘になっており(消えた面の既定値を説明していた)、`queues` と
  `stats` は同じ三つの数を違うキーで印字していた(§1)。あわせて却下の
  取り消しの綴りを `withdraw` 一語にする — ワイヤの `withdrawn`
  (0055 §3.2 が選んだ綴り)へ CLI・Web UI・リビジョンの `change` を
  揃え、既存行はマイグレーション 0034 が書き換える(§3.3)。
  **`verify` と `reject` は 2 本のまま** — 畳むのは能力が同じ二本で
  あって、行為が違う二本ではない(§2)。0015 §2 の「CLI は完全性の面」
  に、完全性とは能力の完全性であるという一行を足す。
- [0055 裁定は一つの面から下す](0055-one-ruling-one-face.md) —
  **Superseded by 0056**。verify / reject / 却下の解除という構造の
  同じ三つの REST 面を `POST /api/v1/review/{id}` 一本にした最初の
  決定。0056 §0 に吸収された。
- [0049 キューの長さを数える](0049-queue-counts.md) — **Accepted**。
  0025 と 0037 の 3 本のフィードを一覧せずに**数える**ことと、空でない
  間 2 で終了する `--exit-code`。空にできないカナリアのフィードは数えない
  (§3.2)。配送・スケジューラ・閾値は持たず、押すのは運用者の cron / CI で
  ある(§4)という refusal を含む。
- [0059 キューは、それを一覧する sort の名で呼ぶ](0059-a-queue-is-named-by-its-listing.md)
  — **Accepted**、**BREAKING**。0049 が数えた 3 つのキューのうち 2 つは、
  それを一覧する `sort` とは別の綴りを持っていた — `stats` の各行が
  「`reported_wrong` が 1 件、見るには `--sort failed`」と印字しており、
  **行そのものが二つの名前を指す証拠になっていた**。`reported_wrong` →
  `failed`、`past_expiry` → `stale_after` に改名する。方向がこちらなのは
  sort 側の綴りが既にある語の使い回しだからで(`failed` は report する
  outcome の値、`stale_after` は OKF SPEC §5 の frontmatter キー)、
  キュー側の 2 語は**そのためだけに存在する発明語**だった。`drafts` は
  単一の sort ではない(`sort=usage` + `status=draft`)ので据え置く。
  あわせて surface.md の VOCAB の数え方に、既存規則の裏面を足す —
  「一つの綴りが二つの族で別の意味を持つなら 2 語」に対して、
  **「一つのものが二つの名前を着ているなら 1 語」**。VOCAB は 38 → 36。
- [0025 書き戻しループを締める](0025-closing-the-loop.md) — **Accepted**。
  検証の時効と、failed 報告・利用実績による再検証の優先順位づけ。内容の
  変わらない PUT では再検証を表現できないので、記録は独立した操作にする —
  0015 §4 の verify 糖衣の判断はここが覆した。
- [0029 利用測定を読み取りパスから外す](0029-usage-recording-off-the-read-path.md)
  — **Accepted**。利用イベントをメモリにバッファして定期フラッシュし、
  利用統計は best-effort と明示する(上限超過は破棄、シャットダウンは
  ドレイン後に最終フラッシュ)。
- [0051 答えられなかった問いを記録し、ループをインスタンスで測る](0051-instance-metrics-and-search-misses.md)
  — **Accepted**。0049 がキューの**長さ**(残量)を数えた上で、まだ
  答えの無かった**通過量と姿**を足す。測定が per-entry しか無く、しかも
  ヒット 0 の検索は捨てられていた(紐づけるエントリが無かった)状態を、
  2 つの器で埋める: **ミスという行**(0029 と同じバッファ・同じ刈り取り、
  クエリ文字列は 500 バイトまで)と、**インスタンスを 1 回で答える
  `GET /api/v1/stats`**(status / trust 別の内訳、窓の中の検証・報告・
  ミス、答えの無かった問い上位 10 件)。キューの深さは 0049 §3.1 が
  空けた場所に `queues` として同じ問い合わせのまま載る — 同じキューを
  2 度数えない。集計面はこの 1 つで、`prefix` はエントリに紐づく数
  (`entries` / `queues` / `review` / `outcomes`)をすべて絞る。
  **`misses` だけは絞られない** — ヒット 0 の検索には結びつく id が
  存在しないからで、これは §2 の「答えられたかはインスタンスの属性」の
  帰結である(§3.7)。ロールアップを持たずオンデマンドで計算し、保持期間を
  超える窓は黙って短く答えず 400 で拒否する。ミスは「0 件」と定義し、
  スコア閾値は採らない(スコアは検索モード間で未較正)。
  CLI は `ochakai stats --prefix`(nudge は 0049 の `queues` のまま)、Web UI は
  Review 画面のタイル、**MCP には載せない**。クエリ文字列の保存は
  初めてなので `OCHAKAI_RECORD_MISSES` で止められ、公開デプロイ(0042)
  では記録しない。

## 同時実行と削除

- [0030 If-Match による楽観ロック](0030-optimistic-locking.md) —
  **Accepted**。ETag / If-Match で条件付き更新を opt-in で提供する。MCP は
  通り道を持たず、キュレーション保護が内部で使う。
- [0031 purge](0031-purge.md) — **Accepted**。ソフト削除済みエントリを
  完全に破棄して id を解放する二段階目の削除。REST / CLI のみ、監査行を
  残し、GCS の blob は回収しない。

## セマンティックモデルと compile

- [0018 import-ossie の廃止](0018-semantic-model-as-knowledge.md) —
  **Accepted**。セマンティックモデルは専用機構を持たず、通常のナレッジ
  エントリ。
- [0028 compile_sql とセマンティックモデル面の撤去](0028-retire-compile-sql.md)
  — **Accepted**。決定的 SQL コンパイルとその周辺(API・MCP・CLI・UI)を
  すべて撤去。

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
