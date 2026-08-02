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
| 認証と identity | [0065](0065-identity-and-provenance.md) |
| デプロイの姿勢(read-only / public / dev) | [0066](0066-four-postures-one-word.md) |
| OKF 互換・バンドル・保存形 | [0046](0046-bundle-address-space.md) が現行。[0061](0061-a-dry-run-is-the-write-withheld.md)(書き込み面の dry run)、[0047](0047-fm-carries-okf-keys.md)(フィルタの語彙)、[0069](0069-the-loop-and-what-measures-it.md) §2(期限と引用元)、[0024](0024-links-from-body.md)(リンク)、[0022](0022-filename-as-name.md)(名前)、[0019](0019-release-review-adjustments.md)(ID 文字種) |
| 住所とパス | [0046](0046-bundle-address-space.md) §§3.1・3.5 と [0064](0064-rest-stops-at-api-v1.md) §5 が現行(バンドルのパスが住所、面は `/api/v1/bundle/{path}` 一本、`.md` は必須でバンドルパスの一部)。[0017](0017-path-addressing.md)(「パスが住所、タイプは属性」の決定そのもの)、[0041](0041-path-scoped-search.md)(prefix)、[0021](0021-move-and-webui-refinements.md)(move) |
| 型の語彙 | [0063](0063-two-unused-recommended-types-leave.md)。改訂の履歴は [0038](0038-type-vocabulary-realignment.md) |
| 知識の単位の呼び名 | [0057](0057-concept-is-the-word-a-reader-meets.md)(ツール名・読む語)、[0064](0064-rest-stops-at-api-v1.md) §7 が現行(JSON フィールド名 `entries` → `concepts`) |
| 添付ファイル | [0046](0046-bundle-address-space.md)(バンドルのオブジェクト)、[0020](0020-attachment-search.md)(検索) |
| 検索と埋め込み | [0053](0053-embeddings-by-default.md)(既定・次元変更)、[0020](0020-attachment-search.md)(ファイル)、[0041](0041-path-scoped-search.md)(prefix)。スコアの床を持たないことは [0068](0068-how-a-face-is-added-and-removed.md) §3、`context` の `hits` は [0067](0067-four-faces-and-what-they-decline.md) §4 |
| サーフェスの配分 | [0067](0067-four-faces-and-what-they-decline.md)(各面の役割と、載せないもの)、[0068](0068-how-a-face-is-added-and-removed.md)(足す規則と降ろす規則) |
| Web UI | [0006](0006-web-ui-serving.md)(配信)、[0044](0044-web-ui-edits-documents.md)(編集)、[0065](0065-identity-and-provenance.md) §5(プロキシと identity) |
| 検証ループと利用測定 | [0069](0069-the-loop-and-what-measures-it.md) が現行。裁定の面と一覧のページングは [0068](0068-how-a-face-is-added-and-removed.md) |
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

- [0065 認可は持たず、誰として記録するかだけを決める](0065-identity-and-provenance.md)
  — **Accepted**。**identity と provenance の現行ドキュメント**
  (0002 / 0027 / 0032 / 0052 の四冊を一冊にまとめたもので、決定は一つも
  動いていない)。認可機構を持たず「到達できた者は読み書きできる」、
  ヘッダから読むのは provenance だけ。actor は認証から来て綴りは
  `human:` / `process:` の 2 つ、委譲は置き換えではなく合成(`via` が
  必ず残る)、producer の自称は actor の**隣**にだけ置ける、そして
  identity を差し替えるプロキシは identity の自称を運ばない。
- [0066 四つの姿勢を、一語で言う](0066-four-postures-one-word.md) —
  **Accepted**。**デプロイの姿勢の現行ドキュメント**(0040 / 0042 / 0060 の
  三冊を一冊にまとめたもので、決定は一つも動いていない)。「呼び出し元が特定
  されるか」「誰かが書けるか」の 2 軸 4 象限を `OCHAKAI_MODE` 一語で言う
  (未設定 / `read-only` / `public` / `dev`)。read-only の強制点はサービス層
  1 箇所で利用測定だけが凍結の外、public は identity を一切読まず 401 も
  返さない。読めない綴りは既定に落とさず起動エラーにする。
- [0009 OKF/Git 往復と provenance の所有権](0009-provenance-portability.md)
  — **Proposed**。export → Git → import の往復で provenance が誰のものに
  なるかの整理(バンドルは知識のみを運び、provenance はインスタンス固有)。
  文書の trust family は台帳にも trust tier にも入らないまま、
  `received:` の下に主張として保存される(0046 §2.2)。
- [0002 認証・認可](0002-authn-authz.md) — **Superseded by 0065**。
- [0027 呼び出し元によるエンドユーザー identity の委譲](0027-delegated-provenance.md)
  — **Superseded by 0065**。
- [0032 Web UI の書き込みを IAP の identity で記録する](0032-webui-iap-identity.md)
  — **Superseded by 0065**。
- [0052 名乗りは actor の隣に置く](0052-producer-beside-the-actor.md)
  — **Superseded by 0065**。
- [0040 デプロイ単位の read-only](0040-read-only-mode.md) —
  **Superseded by 0066**。
- [0042 公開読み取り専用という姿勢](0042-public-read-only.md) —
  **Superseded by 0066**。
- [0060 姿勢は一語で言う](0060-one-word-for-the-posture.md) —
  **Superseded by 0066**。

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
- [0037 宣言した期限と引用元から引けるようにする](0037-stale-and-source-lookup.md) — **Superseded by 0069**。
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

Web UI の書き込みが誰として記録されるかは、この節ではなく
[0065](0065-identity-and-provenance.md) §5 が持つ — 両プロキシがブラウザ
由来の委譲ヘッダを常に削り、serve-ui が IAP の検証済み JWT からのみ作り直す
という規則は、identity の領域の決定だからである(0032 を統合した)。

## サーフェス(REST / MCP / CLI / Web UI)

- [0067 四つの面と、それぞれが引き受けないもの](0067-four-faces-and-what-they-decline.md)
  — **Accepted**。**面の配分の現行ドキュメント**(0004 / 0007 / 0015 /
  0033 / 0039 の五冊を一冊にまとめたもので、決定は一つも動いていない)。
  REST は唯一の契約、MCP のツール数は予算、CLI は完全性の面(能力の
  完全性)、Web UI は BI ツールではない。CLI が REST の薄いクライアントで
  あること、`mcp-stdio` が面ではなく経路であること、`context` の `hits` が
  全面で順位に徹すること、そして**面ごとに意図して載せないもの**の一覧を
  現行の語彙で持つ。
- [0068 面はどう足され、どう降ろされるか](0068-how-a-face-is-added-and-removed.md)
  — **Accepted**。**面を足す規則と降ろす規則の現行ドキュメント**(0050 /
  0056 / 0058 / 0062 の四冊を一冊にまとめたもので、決定は一つも動いて
  いない)。能力とコマンドは一対一(一つなら畳み、二つなら割る)、一覧は
  検索ではない(cursor は一覧だけ、順位は `limit` が契約)、通行量の無い
  入口は降ろす(`min_score` の廃止、`fm.` を MCP から)、裁定は
  `POST /api/v1/review/{id}` 一本から下し `withdraw` 一語で言う。
- [0004 リモート CLI](0004-cli.md) — **Superseded by 0067**。
- [0007 DB 直結コマンドの廃止](0007-api-only-cli.md) — **Superseded by 0067**。
- [0015 サーフェス一貫性の方針](0015-surface-consistency.md) — **Superseded by 0067**。
- [0058 誰も通らなかった二つのフィルタ](0058-filters-nobody-arrived-through.md) — **Superseded by 0068**。
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
- [0033 context の hits は順位に徹する](0033-context-hits-are-a-ranking.md) — **Superseded by 0067**。

## 検証ループと利用測定

- [0069 検証ループと、それを測るもの](0069-the-loop-and-what-measures-it.md)
  — **Accepted**。**検証ループと利用測定の現行ドキュメント**(0025 / 0029 /
  0037 / 0049 / 0051 / 0059 の六冊を一冊にまとめたもので、決定は一つも
  動いていない)。空にできる三つのキューと、キューではない `verified_at`。
  失敗報告は証拠にもとづく陳腐化、`stale_after` は書き手の宣言なので編集で
  しか空かない。利用測定は読み取りパスに触れず best-effort、ヒット 0 の検索は
  行として残り、`GET /api/v1/stats` がインスタンスを一回で答える
  (`prefix` はミス以外のすべてを絞る)。押すのは数であって通知ではない。
- [0050 一覧はカーソルでページングし、順位はしない](0050-listings-page-rankings-do-not.md) — **Superseded by 0068**。
- [0062 一覧は検索ではない](0062-a-listing-is-not-a-search.md) — **Superseded by 0068**。
- [0056 一つの問いに一つのコマンド](0056-one-question-one-command.md) — **Superseded by 0068**。
- [0055 裁定は一つの面から下す](0055-one-ruling-one-face.md) —
  **Superseded by 0056**。verify / reject / 却下の解除という構造の
  同じ三つの REST 面を `POST /api/v1/review/{id}` 一本にした最初の
  決定。0056 §0 に吸収された。
- [0049 キューの長さを数える](0049-queue-counts.md) — **Superseded by 0069**。
- [0059 キューは、それを一覧する sort の名で呼ぶ](0059-a-queue-is-named-by-its-listing.md) — **Superseded by 0069**。
- [0025 書き戻しループを締める](0025-closing-the-loop.md) — **Superseded by 0069**。
- [0029 利用測定を読み取りパスから外す](0029-usage-recording-off-the-read-path.md) — **Superseded by 0069**。
- [0051 答えられなかった問いを記録する](0051-instance-metrics-and-search-misses.md) — **Superseded by 0069**。

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
- [0039 stdio クライアントへの橋](0039-mcp-stdio-bridge.md) — **Superseded by 0067**。
