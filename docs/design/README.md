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
[0048](0048-decision-records-for-wire-contracts.md)。番号は**領域の決定**に
与え、その内側の規則(誰が最初の行を書けるか、数が何を含むか)は PR と
CHANGELOG に置く。リリース済みの記録を改訂するときは差分ではなく領域の
現在の姿を一本に書き、下の早見表の一行が挙げる記録の数には天井がある
— [0128](0128-a-number-is-for-an-area-not-a-rule-inside-it.md)。

## 現行ドキュメント早見表

**改訂の履歴を追わずに現在の姿にたどり着くための表**(0048 §2.4)。
各領域について、いま読むべきドキュメントだけを挙げている。ここに無い
番号は Superseded か、本体の `Status:` ヘッダが行き先を書いている。

| 領域 | いま読むドキュメント |
|---|---|
| 全体アーキテクチャ | [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) |
| Google Cloud 前提・secret-zero | [0003](0003-gcp-only.md)。**認証の第二の経路は [0086](0086-a-second-way-to-say-who-is-calling.md)**(OIDC 発行者を名指したデプロイは自分で検証する。secret は増えない)。**残りを撤回してよい条件は [0115](0115-the-second-footing-waits-for-search.md)**(埋め込みが Google Cloud の外でも既定になること — それまで足場は一つ)。**複数の組織の運用を一人が引き受ける形は [0119](0119-an-operated-fleet-is-deployments-or-directories.md)** — 単位はデプロイか 0109 のディレクトリで、テナント列は持たない |
| 認証と identity | [0065](0065-identity-and-provenance.md)。**認可(ディレクトリごとの閲覧者・編集者)は [0109](0109-a-directory-has-readers-and-writers.md)** — 0065 §1 が自分で置いた改訂条件が満たされた。付与が一つも無いデプロイは 0065 のままである。**ポリシーの置き換えが前提条件を取ることは [0120](0120-the-policy-is-replaced-only-as-it-was-read.md)**(0109 §2 を改訂 — `If-Match` の意味は concept と同じ)。**最初の一行を置けるのも管理者だけであることは [0122](0122-the-first-rule-is-an-administrators-to-write.md)**(0109 §3 を改訂 — 外の呼び出し元は書く前に断る)。**ディレクトリごとの管理者は [0124](0124-a-directory-can-have-its-own-administrator.md)**(0109 §3 を改訂 — `may_admin` は prefix に縛られ、根には置けない)。**subtree のアーカイブを読める者に開いたのは [0127](0127-an-archive-says-which-part-it-is.md)**(0109 §3 を改訂 — アーカイブが自分の範囲を名乗る)。**`stats` が範囲を持つ呼び出し元にも答えることは [0123](0123-the-numbers-say-what-they-counted.md)**(0109 §3 を改訂 — 答えが自分の範囲を宣言する)。**`move` が書き換えの収まる範囲で動くことは [0129](0129-a-move-runs-when-its-rewrite-fits.md)**(0109 §3 を改訂 — はみ出すなら丸ごと断る)。**OIDC 経路で email を持たないトークンが人を process にすることを、そう言うのは [0117](0117-a-person-recorded-as-a-process-says-so.md)**(0086 §4 を改訂 — 記録の仕方は同じで、黙って行わなくなった)。**どの経路がどのヘッダを読むかは [0121](0121-each-path-reads-its-own-header.md)** — 自分で検証するデプロイは `Authorization` だけを読む |
| デプロイの姿勢(read-only / public / dev / sandbox) | [0066](0066-four-postures-one-word.md)。**五つ目の `sandbox` は [0087](0087-a-sandbox-says-it-is-one.md)**(匿名で、書けて、消える — そしてそう言う) |
| 環境変数の名前そのもの | **[0112](0112-a-start-refuses-a-variable-it-does-not-read.md)** — `OCHAKAI_` で始まり ochakai が読まない変数が一つでもあれば `serve` / `serve-ui` は名指しで起動を止める(0064 §2 の「宣言していないキーは 400」を、運用者が手で綴るもう一つの面に当てたもの)。値の側の拒否は 0066 §4・[0080](0080-search-and-how-a-deployment-embeds.md) §2 のまま。素通りするのは harness の `OCHAKAI_TEST_*` と、ochakai が配るフック・job が読む名前の一覧だけ(§4) |
| OKF 互換・バンドル・保存形 | [0075](0075-the-bundle-is-the-address-space.md)(バンドル・住所・保存形)、[0074](0074-the-document-and-the-vocabulary-that-asks-it.md)(文書の形と問いの語彙)。**取り込みが文書を拒む条件と CLI が送るバイト列は [0079](0079-taking-the-document.md) が現行**。期限と引用元は [0069](0069-the-loop-and-what-measures-it.md) §2 |
| 住所とパス | [0075](0075-the-bundle-is-the-address-space.md) が現行(パスが住所、型は属性、move、prefix)。`.md` が必須でバンドルパスの一部であることは [0064](0064-rest-stops-at-api-v1.md) §5。**`.md` が concept の住所であり、そこに座れるものは concept だけであることは [0100](0100-md-is-how-a-concept-is-spelled.md)**(0075 §3.3 を改訂) |
| 型の語彙 | [0071](0071-the-recommended-type-vocabulary.md)。型に `/` を許すのは [0064](0064-rest-stops-at-api-v1.md) §18(0071 §1 の「`/` 不可」を撤回) |
| 知識の単位の呼び名 | [0057](0057-concept-is-the-word-a-reader-meets.md)(ツール名・読む語)、[0064](0064-rest-stops-at-api-v1.md) §7 が現行(JSON フィールド名 `entries` → `concepts`) |
| ファイル | [0075](0075-the-bundle-is-the-address-space.md)(バンドルのオブジェクトと帰属)、[0080](0080-search-and-how-a-deployment-embeds.md)(検索)。ベクトルの鍵がパスであることは [0091](0091-a-file-vector-is-keyed-by-its-path.md)。**バケットの無いデプロイがそう言い、どの面もファイルを差し出さなくなることは [0131](0131-a-deployment-says-what-it-cannot-do.md)** — `stats` が `files` を答え、直し方(変数の名前)はバンドル全体を持つ呼び出し元にだけ載る |
| 検索と埋め込み | [0080](0080-search-and-how-a-deployment-embeds.md) が現行 — 何を融合するかと、`OCHAKAI_EMBEDDINGS` 一語でどう埋め込むかを一冊で持つ。**埋め込みはデプロイのリージョンで行う**(§1.2、データ所在地)。住所で絞る `prefix` は [0075](0075-the-bundle-is-the-address-space.md) §6、スコアの床を持たないことは [0068](0068-how-a-face-is-added-and-removed.md) §3、`context` の `hits` は [0067](0067-four-faces-and-what-they-decline.md) §4([0093](0093-the-budget-governs-the-whole-response.md) が予算の側を改訂)。ヒットが運ぶ一致箇所は [0084](0084-a-hit-says-why-it-matched.md)。**入力窓に収まらなかった concept を数えることとチャンク化を断ることは [0089](0089-a-half-embedded-concept-says-so.md)**(0080 §3・§7 を改訂)。**ファイルのベクトルをパスで引き、帰属を検索時に読むことは [0091](0091-a-file-vector-is-keyed-by-its-path.md)**(0080 §5 を改訂)。**書き手が与えた別名(`synonyms`)を索引が読むことは [0105](0105-a-concept-answers-to-its-other-names.md)** |
| サーフェスの配分 | [0067](0067-four-faces-and-what-they-decline.md)(各面の役割と、載せないもの)、[0068](0068-how-a-face-is-added-and-removed.md)(足す規則と降ろす規則)。**MCP の転送が stateless になり、プロトコルが 2026-07-28 になることは [0118](0118-a-call-carries-everything-it-needs.md)**(0067 §3 を改訂 — 面もツールも動かない)。一覧がいつまで持つかを自分で答えること(構築時に決まる三つの一覧だけ。concept の読みは 0 のまま)は同記録 §7。MCP のツール 7 本とその境界は [0076](0076-two-tools-leave-mcp.md) と、**7 本目を割った [0096](0096-a-listing-is-not-a-search-here-either.md)**。**ツールの答えが一通で返ることと、予算がスキーマを両側とも数えることは [0103](0103-the-tool-result-travels-once.md)**。CLI がファイルを名指す綴りは [0077](0077-one-address-for-a-file.md)。**`context` の予算が応答全体を縛ることは [0093](0093-the-budget-governs-the-whole-response.md)**(0067 §4 を改訂)。**CLI の行の第一列が「読み手が並べてくれと言った鍵」だけになることは [0110](0110-the-first-column-is-the-key-you-asked-for.md)**(0068 §3 の CLI への適用。`search` と逆引きからスコア列が落ちる)。**その行に太字と dim が付くのは [0111](0111-weight-for-the-eye-and-only-for-an-eye.md)**(端末のときだけ。パイプが受け取るバイト列は変わらない) |
| Web UI | [0130](0130-the-web-ui-and-the-fields-of-a-document.md) が現行(配信・ページの形・編集を一冊で。0072 / 0092 / 0126 を畳んだ)。プロキシと identity は [0065](0065-identity-and-provenance.md) §5。**CSP の下で配信され、他人のフレームに入らないことは [0094](0094-the-page-runs-under-a-policy.md)**。**このデプロイができないことをページが出さなくなり、直せる呼び出し元にだけ案内を出すことは [0131](0131-a-deployment-says-what-it-cannot-do.md)** |
| 検証ループと利用測定 | [0069](0069-the-loop-and-what-measures-it.md) が現行。裁定の面と一覧のページングは [0068](0068-how-a-face-is-added-and-removed.md)。**二つのフィードが直近 90 日で並ぶことは [0090](0090-a-queue-ranks-on-what-happened-lately.md)**。**人間側のフローに時系列が加わることは [0095](0095-the-loop-has-a-shape.md)**。**却下の理由がどこまで運ばれるかは [0104](0104-a-ruling-travels-with-its-reason.md)**(CLI の出力と export へ) |
| 同時実行と削除 | [0030](0030-optimistic-locking.md)、[0031](0031-purge.md)。**purge とファイル削除が参照されなくなったバイト列を回収することは [0099](0099-a-purge-reaches-the-bytes.md)**(0031 §3.2 を改訂) |
| 実装の品質ゲート | [0035](0035-verifiability.md) |
| 決定の書き方 | [0048](0048-decision-records-for-wire-contracts.md)。**番号は領域の決定に与え、その内側の規則には与えないことと、この表の一行が挙げてよい記録の数の天井は [0128](0128-a-number-is-for-an-area-not-a-rule-inside-it.md)**(0048 §2.1 / §2.2 を改訂) |
| バンドル往復と provenance の所有権 | [0075](0075-the-bundle-is-the-address-space.md) §3.1 が現行(主張と観測の分離)。往復で何が動かないか・Git をレビュー経路にする決定・二つの拒否は [0009](0009-provenance-portability.md)。**export が却下の理由まで運ぶことは [0104](0104-a-ruling-travels-with-its-reason.md)**(読み戻さないのは同じ) |
| 空のベースを埋める | [0085](0085-the-empty-base-and-what-fills-it.md) — `ochakai seed` が運用者自身の撃った `INFORMATION_SCHEMA` の答えを `BigQuery Table` の draft バンドルにし、書き込みは既存の `import` が行う。**ウェアハウスには接続しない**ので [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1 のコネクタ取り込みの拒否は不変 |
| やらないと決めたこと | [0070](0070-what-was-retired-and-why.md)。**MCP OAuth コネクタの再実装の出発点は [0116](0116-the-connector-price-changed-not-its-condition.md) が差し替えた** — 戻す条件は 0070 §5 のままで、その日に開くのが 0010 の認可サーバ(863 行)ではなく、測定済みの二つの答え(180 行)になった |
| REST の安定性契約 | **凍結の範囲は [0107](0107-the-freeze-holds-the-okf-core.md) が現行** — 凍るのは OKF コア(bundle の往復と search)だけで、残りの `/api/v1` は 0.x の不安定な面。凍結の機構と最後の一括変更は [0064](0064-rest-stops-at-api-v1.md)、[docs/compatibility.md](../compatibility.md)。**凍結が止めているものの中身は [0082](0082-what-the-freeze-holds-still.md) が現行**(応答専用スキーマへの追加は対象外。**任意のクエリパラメータの追加も対象外で、それは [0101](0101-a-level-can-be-walked.md) §5**)。凍結を破ってよい理由は三つあり、二つ目(OKF 非適合な出力)は [0100](0100-md-is-how-a-concept-is-spelled.md) §4、三つ目(規格が定める綴りの重複を畳む)は [0102](0102-one-history-in-one-spelling.md) §3。エラー応答が運ぶ `code` は [0083](0083-an-error-carries-a-code.md)。**本文の鍵の照合が完全一致で、同じ鍵の重複が 400 になることは [0125](0125-a-body-names-each-field-once.md)**(0064 §2 が決めた規則を、書かれたとおりに効かせたもの) |
| MCP・CLI の安定性契約 | [0088](0088-a-retired-name-answers-for-one-release.md)(改名された名前は一リリースだけ答える — 呼べるが、載らない) |

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

- [0081 ochakai が何であり、何を持たないか](0081-what-ochakai-is-and-what-it-refuses-to-hold.md)
  — **Accepted**。**全体アーキテクチャの現行ドキュメント**(0001 のうち
  他のどの記録も持っていない決定だけを引き継いだもの)。LLM を内蔵せず
  SQL を実行しない Context Provider、Go 単一バイナリと PostgreSQL 一本
  (Redis もベクトル DB も検索クラスタも持たない)、配布とサプライ
  チェーン、そして却下の記憶 —— No を覚えていることが verified を覚えて
  いることと同じだけ要る。§5 は 0001 の各節がいまどの記録に移ったかの
  行き先表である。
- [0001 全体アーキテクチャ](0001-architecture.md) — **Superseded by 0081**。
  最初の記録。402 行のうち生きていたのは 4 分の 1 ほどで、残りは後続が
  持つか、もう事実でなかった。
- [0087 サンドボックスは、自分がそれだと言う](0087-a-sandbox-says-it-is-one.md) —
  **Accepted**。`OCHAKAI_MODE=sandbox` は姿勢の四つ目のセル(匿名かつ
  書ける)を**意図して配る**綴りである。この製品の中心はループなのに、
  公開デモは `public`(read-only を含意)なので**看板の機能だけが体験
  できなかった** — 書き戻しを一度見るのに Postgres が要った。`dev` の
  再利用ではないのは、`dev` が自分で「デプロイに使うな」と言っており、
  設定を読む人が事故と意図を区別できず、Terraform が `allUsers` を許すのは
  公開姿勢だけだからである。要点は §3: **言わないサンドボックスは、
  書いたものを盗む** — だから `GET /api/v1/stats` が `sandbox: true` を
  返し、Web UI が全ページにバナーを出す。ヘッダにできないのは凍結の
  ためで([0082](0082-what-the-freeze-holds-still.md) が外に置いたのは
  応答スキーマへの追加であってヘッダ名ではない)、`stats` はもともと
  ループについて答える面である。miss は記録しない(0051 §3.4)。
- [0086 誰が呼んでいるかを言う、二つ目の方法](0086-a-second-way-to-say-who-is-calling.md) —
  **Accepted**。`OCHAKAI_OIDC_ISSUER` と `OCHAKAI_OIDC_AUDIENCE` を設定した
  デプロイは、Bearer トークンの署名・発行者・audience・有効期限を自分で
  検証する。Google Cloud の外にあった姿勢は `dev`(誰も認証しない)と
  `read-only`(誰も認証せず書き込みも断る)の二つだけで、その間は
  Cloud Run の IAM チェックがプロセスの前で済んでいることに乗っていた。
  **secret は増えない** — 検証に使うのは発行者が HTTPS で公開する公開鍵で、
  共有鍵系のアルゴリズムは許可リストに入れていない。認可は相変わらず無く、
  埋め込みは Vertex AI か無いか(GCP 外は lexical のみ。移行 0036 以降、
  日本語の二文字語は索引で引ける)。片方だけの設定と、`dev` / `public` との
  組み合わせは起動時に断る。ENV 11 → 13。契約は遡及宣言を一つ得る —
  両経路が元から送っていた 401 を全操作に宣言し、info の散文を二経路の
  現在形に書き直す。
- [0121 経路ごとに、読むヘッダが違う](0121-each-path-reads-its-own-header.md)
  — **Accepted**。0086 に**自分で検証するデプロイが資格情報をどのヘッダで
  受け取るか**を足し、0065 §2 の「`X-Serverless-Authorization` を優先して
  読む」が Cloud Run 経路だけの規則であることを言う。**壊れていたのは、
  OIDC のデプロイを Cloud Run IAM の後ろに置いた構成**で、Google が転送した
  署名なしトークンを自分の発行者の鍵で検証しようとして**全リクエストが
  401** になっていた — 呼び出し元の本物のトークンは同じリクエストの
  `Authorization` に入ったまま読まれない。しかもそれは Google の
  二ヘッダ分割が**意図どおり**働いた形である(プラットフォームが検査する
  ヘッダとアプリケーションが検査するヘッダは別で、ochakai 自身の
  `serve-ui` がその形で書かれている)。決定は、検証器を持つデプロイは
  `Authorization` だけを読むこと — Cloud Run 経路の優先には理由が現に
  効いている(二本のうち Google が検証したのは片方だけなので、もう一方を
  読めば認証された呼び出し元が他人として記録される)が、**OIDC 経路には
  その理由が無い**: どちらも前段で検証されておらず、検証するのは
  ochakai 自身なので、防ぐべき詐称が存在しない。優先が実際にしていたのは
  資格情報を覆い隠すことだけだった。`X-Serverless-Authorization` しか
  無いリクエストは、暗号の失敗ではなくヘッダの位置として断る(0117 の形)。
  **検証の中身も記録される名前も委譲も secret-zero も動かず、面は一つも
  増えない。** 直った構成 — OIDC × Cloud Run IAM の二重の関門 — は
  それ自体で使え、**公開 invoke の姿勢はここでは綴っていない**
  (それは [0119](0119-an-operated-fleet-is-deployments-or-directories.md)
  §5 の一番目で、deploy ガイド・SECURITY.md・0066 §7 のレート制限を動かす
  別の決定である)。
- [0115 二つ目の足場は、検索を待つ](0115-the-second-footing-waits-for-search.md)
  — **Accepted**。0003 の「それ以外の実行環境をサポートしない」を一行も
  緩めないまま、**それを撤回してよい条件**を与える。0086 の後に Google
  Cloud に縛られているのは三つ — データベースの資格情報、埋め込み、
  ファイルのバイト列 — で、**同じ重さではない**。資格情報とファイルは
  何が失われるかを言えば運用者の選択になるが、埋め込みは返せない:
  C8 が乗っているのが検索であり、外では 0114 §2 の言う「退化していない」
  デプロイ、つまり**別の製品**になる。**支える価値の無い足場を「支える」
  と宣言するのは、嘘を一つ増やすことである**(§3)。条件は測れる三つ —
  設定を足さずに有効、secret を増やさない、データ所在地を運用者が選べる —
  で、機構は選ばない。この記録は改訂そのものではなく、条件が満たされた
  日に別の番号が 0003 を改訂する(§4)。却下: 外部 API キーの埋め込み、
  条件より先に二つ目のストレージエンジンを足すこと、「マルチクラウド
  対応」という言葉、そして条件を書かずに黙っていること。
- [0119 任された運用の単位は、デプロイかディレクトリである](0119-an-operated-fleet-is-deployments-or-directories.md)
  — **Accepted**。決定を一つも動かさず、**複数の組織の運用を一人が
  引き受ける形**を先に書く: 隔離が要る組織は一つの Cloud Run サービスと
  一つのデータベース、一つの failure domain を受け入れる組織は同じ
  デプロイの中のディレクトリ(0109 の付与)で、**二つは同じ道の上にある**
  — ディレクトリの下を export したものは普通の OKF バンドルなので、
  変換なしにデプロイへ卒業できる。運用者は IAM の principal の集合で
  あって鍵の持ち主ではない。形を開けているのは既存の四つ — 境界は住所で
  引く、secret-zero、バンドルが丸ごと出て戻る、誰にも問い合わせない —
  で、**この四つを壊す変更がこの形を閉じる変更である**(§2)。断るのは
  テナント列(住所の横の二つ目のスコープ — 0109 §7・0075 §7 が既に
  断っており、隔離はディレクトリと同じ一箇所の強制で変わらず、テナント
  ごとの設定はデプロイが無料で持つ)、運用者向けの telemetry と管理
  プレーン、艦隊の道具と登録の面をこのリポジトリに置くこと、「マネージド
  版」を約束すること(§3)。今日の代金は運用者が払う — identity は
  Google のもの(Cloud Run 上で OIDC を公開 invoke にする姿勢は綴られて
  いない)、互換性の窓、共有インスタンスの経済、そしてディレクトリの形で
  0109 §3 が管理者に寄せたもの(§4)。製品が動いてよいのは順序付きの
  宿題 — 公開 invoke で自分を検証する姿勢、0109 §3 が自分で予告した分割
  (スコープ内の `stats`・export・miss、`PUT /api/v1/access` の
  `If-Match`、自分の prefix の下にだけ規則を置ける principal)、
  レート制限 — で、どれも別の番号になる(§5)。faq の「ホスティング版は
  無い」は今日も真。面は動かない。
- [0003 Google Cloud 前提化](0003-gcp-only.md) — **Accepted**。
  Cloud Run + Cloud SQL IAM を前提にし、トークン・パスワードを持たない
  (secret-zero)。
- [0053 埋め込みは既定であり、ベクトル空間は捨ててよい](0053-embeddings-by-default.md) — **Superseded by 0080**(0073 経由)。

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
- [0128 番号は領域の決定に与え、その内側の規則には与えない](0128-a-number-is-for-an-area-not-a-rule-inside-it.md)
  — **Accepted**。0048 §2.1 / §2.2 を改訂。観測できるかは領域を選ぶ基準で
  あって粒を選ぶ基準ではなく、0109 が一週間に五つの改訂を受けて早見表の
  一行に九本が並んだ。**領域の内側の規則は番号を取らず PR と CHANGELOG に
  置き、リリース済みの親を改訂するときは差分ではなく領域の現在の姿を
  一本に書いて親と改訂を吸収する**(skill の手順 7 を規則にする)。
  早見表の一行が挙げる記録の数に `INDEX-ROW-RECORDS` の天井を置き、向きは
  下だけ — 今日の最大 10 から始め、広い行から畳んで 3 で止まる。

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
- [0112 起動は、自分が読まない変数を断る](0112-a-start-refuses-a-variable-it-does-not-read.md)
  — **Accepted**、**BREAKING**。`OCHAKAI_` で始まり ochakai が読まない
  変数が環境にあれば、`serve` と `serve-ui` は**その名前を挙げて起動を
  止める**。値の綴りは何度も疑ってきた(0066 §4、0080 §2、0109 §3)のに
  **名前には検査が一つも無く**、`OCHAKAI_RECORD_MISSES` を一文字落とせば
  切ったつもりの記録が付いたまま走り、0078 が畳んだ `OCHAKAI_VERTEX_*`
  を残したデプロイは誰も読まない値を持ち続けた。0064 §2 が REST で
  未知のキーを 400 にしたのと同じ判断を、**運用者が手で綴るもう一つの
  面**に当てる。見つかった名前は一度に全部挙げ、読む変数の一覧
  (`config.Known`)はソースから読み戻して検査する(0035)。名前空間の
  うち `OCHAKAI_TEST_` は harness のもので読み飛ばし、出荷コードが
  そこを読んでいないことを別のテストが持つ。**環境変数に 0088 の
  一リリースの窓は無い** — 窓は「古い綴りでも動く」を意味し、それは
  この面では黙って効くことそのものだから、代わりに起動時の名指しが
  その役を果たす。CLI のクライアント側は検査しない(間違いがすぐ見える)。
  警告ログにする案・退役した綴りだけの表を持つ案(打ち間違いを一つも
  捕まえない)は採らない。**面の数は一つも動かない**(ENV は 15 のまま)。
- [0009 OKF/Git 往復と provenance の所有権](0009-provenance-portability.md)
  — **Accepted**。export → Git → import の往復で provenance が誰のものに
  なるか: バンドルは知識のみを運び、provenance はインスタンス固有である
  (世界観の現行の記述は 0075 §3.1)。同じインスタンスへの往復では
  provenance は一つも動かず、Git はレビュー経路として規範化され、
  **merge は verify ではない**。二つの拒否 —— `--preserve-provenance` と
  署名付き provenance —— は認可機構を持たないことから出る。手順は
  [git-review ガイド](../guides/git-review.md)。
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
- [0117 人が process として記録されるとき、そう言う](0117-a-person-recorded-as-a-process-says-so.md)
  — **Accepted**。[0086](0086-a-second-way-to-say-who-is-calling.md) §4 の
  「email が無ければ `sub` を process として記録する」を改訂する —
  **記録の仕方は一字も変えず、黙って行わなくなる**。三つ目の枝は「機械の
  トークンはこの形をしている」を理由に置かれたが、**人のトークンもこの形で
  届く**: 発行者が email を ID トークンにだけ載せる(それが普通の構成である)
  と、ブラウザで認可を通った人の書き込みが `process:<sub>` 名義になり、
  401 も 403 も出ない。0065 §3 が委譲について「静かな降格は、明示的な失敗より
  常に高くつく」と書いた当の形が、二つ目の扉で起きている。**403 にはできない**
  — email を持たないトークンは機械のものかもしれず、見分ける材料が無いので、
  拒めば正当な機械の構成を拒む。だから告知で、**プロセスに一度だけ**言う
  (見分けられない以上、正しい構成でも必ず一度は鳴る。毎回鳴る警報は何も
  知らせない)。発行者と `sub` を出し、鍵の素材もトークンも出さない。面は
  一つも動かない — 増えるのは運用者自身のログの一行である。

## ナレッジモデル(構造・ID・型・名前・リンク)

- [0079 文書を受け取る](0079-taking-the-document.md) —
  **Accepted**。**取り込みが文書を拒む条件と、CLI が送るバイト列の現行
  ドキュメント**(0075 §3 / §4.2 と 0074 §1 を改訂。**REST は変えない**)。
  SPEC §11 で consumer の仕事は文書を受け取ることであり、ochakai が文書を
  拒んでいたところはどれも利用者のバイト列が消えうる場所だった。
  **`executor.receipt` の要求と、型の書かれていないキーの文字列強制は、
  ochakai 自身の規則を SPEC のものと取り違えていた** — どちらの note も
  `SPEC §10.2` を引いていたが、§10.2 はその要求をしていない。手書きの
  `index.md` / `log.md` が消えたことも note に出る。`ochakai put` /
  `import` は正準形を描き直さず、渡された文書のバイト列をそのまま送る。
  書き込み経路が拒む文書は**保存しないまま報告する** — 残すには 0064 で
  凍るワイヤに規則を足すしかなく、それはやらないと決めた(§1)。拒否が
  その文書の指すファイルまで道連れにしていたほうは直した。
- [0075 バンドルがアドレス空間である](0075-the-bundle-is-the-address-space.md)
  — **Accepted**。**バンドル・住所・保存形の現行ドキュメント**(0017 / 0021 /
  0041 / 0046 の四冊を一冊にまとめたもので、決定は一つも動いていない)。
  ochakai が持つのは一つのバンドル(パス → オブジェクトの写像)で、
  **バンドルに入ったものは出てくる**。パスが住所で型は属性、保存は受け取った
  バイト列で正準形は導出値、他者の trust family は `received:` の主張として
  残る。帰属は本文から導出し、move は名前空間ごと動かし、`prefix` は住所で
  絞る(認可ではない)。
- [0074 文書の形と、それに問う語彙](0074-the-document-and-the-vocabulary-that-asks-it.md)
  — **Accepted**。**concept 文書の形と問いの語彙の現行ドキュメント**
  (0019 / 0022 / 0024 / 0047 / 0061 の五冊を一冊にまとめたもので、決定は
  一つも動いていない)。ファイル名が名前(title は任意、鍵として比較される
  文字列だけ NFC)、リンクは本文から導出、id の文字種はパス安全性だけ、
  `fm.` が引けるのは OKF が定義するキー、そして dry run は書き込みを止めた
  ものである。
- [0071 推奨する型の語彙](0071-the-recommended-type-vocabulary.md) —
  **Accepted**。**型の語彙の現行ドキュメント**(0038 / 0063 を一冊に
  まとめたもの)。推奨は 9 型で、型は閉じた集合ではない。語彙を決める根拠は
  「綴りだけで意味が立つか」「OKF が綴りを与えた証拠」「その綴りで誰かが
  実際に書いたか」の三つで、三つ目が `Playbook` と `API Endpoint` を外した。
- [0047 問いの語彙は OKF が定義するキーである](0047-fm-carries-okf-keys.md) — **Superseded by 0074**。
- [0061 dry run は書き込みを止めたもの](0061-a-dry-run-is-the-write-withheld.md) — **Superseded by 0074**。
- [0046 バンドルがアドレス空間](0046-bundle-address-space.md) — **Superseded by 0075**。
- [0043 文書を真とする](0043-document-first.md) —
  **Superseded by 0046**。保存もワイヤも正準化した OKF 文書
  そのものにし、DB は索引とインスタンス台帳(検証・却下・利用・履歴)に
  徹する。status は OKF の 3 値、検証は台帳への追記(複数可)、却下は
  rejection 台帳、ETag は内容ハッシュ、actor は `process:`。写像が恒等に
  なり、attrs と封筒の二重底・rejected の非可逆往復・外来 `stable` の
  draft 降格・`updated_at` の三役が対象消滅する。
- [0041 住所で検索の範囲を絞る](0041-path-scoped-search.md) — **Superseded by 0075**。
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
- [0017 パスが住所、タイプは属性](0017-path-addressing.md) — **Superseded by 0075**。
- [0019 v0.10.0 リリース前の整合調整](0019-release-review-adjustments.md) — **Superseded by 0074**。
- [0022 ファイル名が名前](0022-filename-as-name.md) — **Superseded by 0074**。
- [0023 型の語彙を OKF に一本化する](0023-okf-type-vocabulary.md) —
  **Superseded by 0038**。内部スラグと OKF 表示名の二重語彙を
  廃止し、型の値は OKF の綴りそのものに。
- [0038 推奨型の語彙を OKF の証拠に合わせ直す](0038-type-vocabulary-realignment.md) — **Superseded by 0071**。
- [0063 使われていない2つの推奨型を外す](0063-two-unused-recommended-types-leave.md) — **Superseded by 0071**。
- [0057 concept は読む人が出会う語でもある](0057-concept-is-the-word-a-reader-meets.md)
  — **Accepted**、**BREAKING(文言のみ)**。**知識の単位の呼び名領域の
  現行ドキュメントの一つ**(0054 の決定を §0 に統合する)。0054 は MCP
  のツール名 5 本の `knowledge` を OKF SPEC §2 の語 `concept` に改めた
  (`search_concepts` 等、当時はツール 8 本のまま。うち 2 本は
  [0076](0076-two-tools-leave-mcp.md) が降ろして 6 本になった)。0057 はその数え方が
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

- [0106 読みは、自分を指すものを連れて返る](0106-a-read-carries-what-points-at-it.md)
  — **Accepted**。concept の単読(REST の JSON・`get_concept`・
  `ochakai get`)が `linked_from` を運ぶ — その concept を本文から指す
  concept の行(住所順、上限 20、却下済みは出ない)。リンクは本文からの
  導出なので順方向は文書に見えているが、**逆方向だけが他人の文書に住む**:
  metric を読む者に、それを explains する insight の存在は見えなかった。
  `links_to` は畳まない — 尋ね方を知っている者の完全でページングできる
  逆引きはあちら、尋ねなかった者に届く先頭 20 行がこちら。行はポインタで
  あって配達ではないので fetched は記録せず、応答専用の追加なので凍結の
  外である(0082)。MCP のスキーマは 1 バイトも動かない(0103)。
- [0024 リンクは本文から導出する](0024-links-from-body.md) — **Superseded by 0074**。

## 添付ファイル(0046 でバンドルのオブジェクトになった)

- [0131 できないことは面が言い、直し方は直せる人にだけ言う](0131-a-deployment-says-what-it-cannot-do.md)
  — **Accepted**。`OCHAKAI_GCS_BUCKET` の無いデプロイはファイルを 501 で
  断るのに、Web UI は「ファイルを追加」を出し続けていた — **押すまで
  分からず、押して出るのは環境変数の名前**で、それはナレッジを書きに来た
  人が設定できるものではない。`GET /api/v1/stats` が `files` を答える:
  `enabled` は全員が読み(嘘をつかれるのは全員だから)、`variable` は
  バンドル全体を持つ呼び出し元にだけ載る(宛先の無い指示は雑音であって、
  秘密ではない)。判定は `RequireAdmin` と同じで、付与が一つも無いデプロイ
  では全員 — 手元で `ochakai ui` を動かしている人こそ、変数を設定できる
  唯一の人である。**オブジェクトの不在は「無効」ではなく「訊かれたことが
  ない古いサーバ」**で、そこが姿勢の二つの真偽値と違う。面はページ(面が
  消え、管理者にはバナー)と CLI(`ochakai stats` の一行)。MCP は
  そもそもファイルを差し出さないので、止める嘘が無い。一般の「無効な機能」
  リストは採らなかった — 覚える語が一つ増えるだけで、欄で足りる。
- [0085 空のベースと、それを埋めるもの](0085-the-empty-base-and-what-fills-it.md) —
  **Accepted**。`ochakai seed` が、運用者自身が撃った
  `INFORMATION_SCHEMA` の答えを読んで `BigQuery Table` の **draft** バンドル
  を吐き、書き込みは既存の `import` が行う。**ウェアハウスには接続しない** —
  クライアントはバイナリにリンクされず、資格情報も読まないので、
  [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1 の
  「コネクタを持たない」は一行も緩んでいない。出るのが draft で
  `description` が空なのは要点であり、誰も書いていない一文は全員が信じて
  しまうからである。カタログのコネクタが作るのは誰も請け合っていない
  数千行、こちらが作るのは数千個の問いである。CLI 24 → 25、FLAG 28 → 29。
- [0084 ヒットは、なぜ一致したかを言う](0084-a-hit-says-why-it-matched.md) —
  **Accepted**。検索のヒットが `snippet`(クエリの語が本文のどこに落ちたか、
  読める幅で切り出したもの)を運ぶ。行にあった title と description は
  「この concept は全体として何か」であって「この問いに何が答えたか」では
  なく、10 件から 1 件を選ぶ呼び出し元は本文を取りに行くしかなかった —
  0067 §4 が結果に文書を載せないと決めたときの計算が、一段ずれた場所で
  そのまま起きていた。名前や description で一致したときと、ベクトルで
  見つかったときは出さない: どちらも指させる一節が無い(後者に本文の冒頭を
  出すのは、理由でないもので理由に答えることである)。`ts_headline` では
  なく Go で切るのは、上位 N の本文が既に手元にあり、一致の単位が移行
  0036 の二文字窓というこちらの規則だからである。MCP の応答には出るが、
  ツールの説明文には足さない。
- [0080 検索が何を融合し、このデプロイがどう埋め込むか](0080-search-and-how-a-deployment-embeds.md)
  — **Accepted**。**検索と埋め込みの現行ドキュメント**(0020 / 0053 /
  0073 / 0078 を一冊にまとめたもの)。Google Cloud の上では埋め込みが既定
  で、使えるかを決めるのは設定ではなく IAM。**埋め込みはデプロイが動いて
  いるリージョンで行う** — プロジェクトと同じくメタデータサーバーから読み、
  読めなければ誰も選んでいないリージョンで埋め込むより字句検索に落ちる
  (§1.2。どの固定値も、それを選ばなかったデプロイには間違いである)。
  検索は字句・concept のベクトル・
  ファイルのベクトルの三本を融合し、ファイルは独立して返らず、ochakai は
  ファイルを解釈しない。設定は `OCHAKAI_EMBEDDINGS` 一語 — 未設定 / `on` /
  `off` / Vertex AI のモデル resource name で、どれでもない綴りは起動エラー
  (0066 と同じ形)。幅はデプロイの設定ではなくモデルごとの定数で、知らない
  モデルは拒否する(**ベクトルは導出物である**)。§3 は
  [0089](0089-a-half-embedded-concept-says-so.md) が改訂 — 列の幅の話に、
  入力窓の話が加わる。
- [0089 半分しか埋め込まれていない concept は、そう言う](0089-a-half-embedded-concept-says-so.md)
  — **Accepted**。0080 §3 を改訂する。モデルの入力窓に収まらなかった
  concept は前半だけがベクトル検索に載り、**それが成功と区別できなかった** —
  ベクトルはあり、順位に乗り、後半で引けないだけである。切り捨てはベクトル
  の行が持つ(同じ concept が別のモデルでは丸ごと入るので、concept の性質
  ではない)。数は `stats.embedding` の `vectors` / `truncated` の対で出て、
  対で読む — 4 分の 3 ならモデルを移す話、400 分の 3 なら concept を分ける話
  である。**チャンク化はしない**: 誰が詰まったかまだ誰も言えず、機構は
  一 concept 一ベクトルという前提を四箇所で崩し、そもそも分けるべき concept
  である可能性のほうが高い。再開の条件は §4 にある。
- [0114 退化した順位は、そう名乗る](0114-a-degraded-ranking-says-so.md)
  — **Accepted**。0080 §1 の融合が**片肺で走ったとき**を決める。クエリを
  埋め込めなかった検索はレキシカル一本に退化する(それは前からそう)が、
  **言っていたのはログだけで、薄い結果を手にしている呼び出し側には
  成功と区別が付かなかった** — 0089 §1 と同じ形の静かな失敗である。
  応答に `degraded: true` が付く。困るのが誰かは C8 が決めていて、
  **trigram は二文字の日本語語を引けない**ので、日本語のデプロイで
  埋め込みが落ちることは検索が「ほぼ何も返さない」に近づくことであり、
  そのとき利用者は自分の問いの立て方を疑う。**埋め込みを持たない
  デプロイは退化していない**(そこでは語の一致が答えの全部で、毎回立つ旗は
  何も言わない)。ワイヤに乗るのは値一つで、文は面ごとに書く — CLI は
  stderr(パイプのバイト列は変えない)、Web UI は日本語、MCP は
  `search_concepts` の説明文(この面には出力スキーマが無いので、
  `jsonschema` タグはエージェントに届かない)。MCP の出力型を検索用と
  一覧用に割り、検索から `cursor`(検索はページしない)、一覧から
  `degraded`(埋め込むものが無い)が落ちた。常駐 11,629 → 11,788 バイトで
  `MCP-BYTES` は動かず、真偽値なので `VOCAB` も 49 のままである。
  **0082 §3 も改訂する** — 応答スキーマの置き場所は二つではなく三つで、
  三つ目(operation に直接書かれたもの)に凍結の応答専用規則が届いて
  いなかった。
- [0091 ファイルのベクトルは、パスで引く](0091-a-file-vector-is-keyed-by-its-path.md)
  — **Accepted**。0080 §5 を改訂する。ファイルのベクトルの鍵は
  `(concept id, ファイル名)` だった — ファイルが concept の持ち物で
  `<id>/<name>` にしか置けなかった頃の形で、0046 / 0075 でファイルが
  bundle のオブジェクトになったあとは、**本文が `charts/q1.png` と
  `tables/q1.png` を指す concept が両方を同じ行に書き、片方が消えていた**。
  失敗は静かである — ファイルは保存も配信も export も名前での字句一致も
  通り、落ちるのはベクトル面だけ、つまり「別の言い方で訊く」経路だけで
  ある。鍵はパスになり、**どの concept のヒットになるかは検索時に bundle が
  答える**: 今日リンクされたファイルは今日から引き、リンクを外れた瞬間に
  引かなくなり、二つの concept が見せる一枚は一本のベクトルで両方を引く。
  古い行は**移し替えず落とす** — 上書きで消えた側はもう無く、残った行が
  どちらのものかは行からは分からない。アップグレード後に
  `ochakai reembed` を一度走らせるのが運用者の手順で、それまでファイルは
  名前で引ける。未埋め込みの列はファイルの列になり、**このデプロイの
  モデルが受け取れる媒体型だけ**が並ぶ(zip を並べると列は永久に減らず、
  `reembed` が毎回「まだ残っている」と言って非ゼロで終わっていた)。面は
  一つも増えない。
- [0105 concept は書き手が与えた別名にも答える](0105-a-concept-answers-to-its-other-names.md)
  — **Accepted**。lexical 索引が `synonyms` を読むようになる。型語彙は
  `Metric` を「定義と**同義語**」と説明し、`examples/demo` はそのとおり
  `synonyms: [net sales, top line, 売上]` と書き、`ochakai search 売上` は
  その metric を返さなかった — haystack は id・title・description・tags・
  body と名前空間内のファイル名だけで、producer のキーは `attrs` に入り、
  誰も読んでいなかった。**日本語でいちばん痛い**: 倉庫の列が `revenue` で
  会議の語が `売上` のとき、その二つを結ぶ最も自然な場所がこれである。
  足すのはトリガがファイル名を足しているのと同じ場所(移行 0040)で、
  配列でも単一の文字列でも読む。**`synonyms` だけ**である — 他の producer
  キーは concept の別名ではなく concept についての値で、索引に入れると
  「参照しているだけの concept」が一致として返り始める(それは
  `--links-to` と `--source` の問いである)。キーは producer のものの
  まま: 検証もワイヤのフィールドも専用フィルタも足さず、0043 §3.6 の
  「書かれたまま保つ」は動かない — **読むことと所有することは別**である。
  面は一つも増えず、検索結果は変わる(それが目的で、変わり方は検索評価
  ハーネスが数で持つ)。
- [0073 検索が何を融合し、埋め込みがいつ効くか](0073-search-and-when-embeddings-apply.md)
  — **Superseded by 0080**。0020 / 0053 を一冊にした前身。
- [0078 このデプロイがどう埋め込むかを、一つの変数で言う](0078-one-variable-says-how-it-embeds.md)
  — **Superseded by 0080**。五つの設定を `OCHAKAI_EMBEDDINGS` 一語に畳んだ
  決定。0073 の設定面だけを改訂する記録だったため、0080 が両方を受けた。
- [0008 画像添付](0008-image-attachments.md) — **Superseded by 0046**。
  画像添付の導入。「原本ではなく根拠」の位置づけと `<id>/<name>` は、
  0046 では帰属の**導出規則**として残る。
- [0011 添付画像バイト列の GCS 移行](0011-gcs-attachment-storage.md) —
  **Superseded by 0046**。バイト列を GCS に置くという判断だけが残る。
- [0013 添付ファイルの一般化と GCS 一本化](0013-attachment-files-gcs-only.md)
  — **Superseded by 0046**。任意ファイル形式への一般化と GCS 一本化。
  sniff によるメディアタイプ判定と markdown-only 運用は 0046 も維持し、
  SVG / HTML の書き込み拒否だけが配信側の防御に置き換わる。
- [0020 添付ファイルの検索](0020-attachment-search.md) — **Superseded by 0080**(0073 経由)。

`attachments` / `Attachment` という wire 上の綴りは 0046 §2.1 が概念と
しては既に対象消滅させていたが、名前だけ残っていた。**その綴りの retire
自体は [0064](0064-rest-stops-at-api-v1.md) §6 の決定**であり、この節の
どのドキュメントも改訂しない — 保存モデルは 0046 のまま、動いたのは
JSON キーと MCP ツール名だけである。`change` の語彙に残っていた
`attach` / `detach` という最後の綴りは 0064 §8 が仕上げる(issue #470)。

## ブラウズと Web UI

- [0130 Web UI — 二つの配信経路と、文書とその欄](0130-the-web-ui-and-the-fields-of-a-document.md)
  — **Accepted**。**Web UI の現行ドキュメント**(0072 / 0092 / 0126 を一冊に
  畳んだもの)。ページは認証を知らず、同じ一式を `ochakai ui` と
  `ochakai serve-ui` の二経路で配る — 危険な組み合わせはコマンドを分けること
  で書けなくなり、クロスサイトの書き込みは両方の経路が断る(`serve` には
  置かない)。配信するのはビルドなし・フレームワークなし・CDN なし・npm
  依存なしの ES モジュール一式で、純粋なモジュールは `node --test` が値で
  確かめる。**保存されるのは文書のままで、その frontmatter に欄が戻る** —
  0072 §3.1 が「需要が戻ったら、読み取りの既定形を二重にするのではなく
  サーバ側に『文書 → 構造』の面を足す」と書いた条項を実行し、
  `POST /api/v1/frontmatter` 一本にした。ブラウザは YAML を読みも書きも
  せず、書き戻しは**名指したキーのブロックの差し替え**なので、キーの順・
  コメント・クォート・欄の無い producer のキーは一バイトも動かない。
  `generated` / `verified` は欄にならない(押しても何も起きないボタンを
  置かないため)。
- [0094 ページはポリシーの下で動く](0094-the-page-runs-under-a-policy.md)
  — **Accepted**。0092 §5 が「可能にするだけで決めていない」と書いた CSP と
  a11y の基礎を決める。インライン `<script>` が一つも無くなったので
  `script-src 'self'` が**書けるようになった** — CSP の価値の大半はそこで、
  一行でも残っていると書けない。実測でも違反はインラインの `style` だけで、
  スクリプトの違反は一件も出ない。**`frame-ancestors 'none'` は利用者が
  観測できる拒否である**: Web UI はキュレーションの面で、人の裁定が起きる
  唯一の場所だから、囲んだ側がボタンの位置を知って上に何でも重ねられる
  状態にはしない — 埋め込みたい人には REST がある。`img-src` に `https:`
  を許すのは、ヘッダを足したついでに外部画像を黙って消すのが「安全に
  見えるように文書を壊すこと」だから。**`style-src` の `'unsafe-inline'` は
  支払いであって値引きではない** — literal な style 属性が約 40 あり、
  全部をクラスに移すと三十個のユーティリティと引き換えになる一方、
  style 属性に攻撃者由来の値は一つも入らない(唯一の動的な表の
  `text-align` は四つの定数のどれか)。`Referrer-Policy` は足さない —
  数えるヘッダが 14 本目になるのに、買えるのはブラウザ既定の少し先だけで、
  ルートは fragment に居るのでパスは `/` である。a11y は網羅ではなく
  「無いと使えない」四つ: toast が live region、移動後にフォーカスが主領域へ、
  現在のタブに `aria-current`、エラーバナーが `role="alert"`。
- [0072 Web UI — 二つの配信経路と、文書としての編集](0072-the-web-ui-serves-and-edits-documents.md) — **Superseded by 0130**。
- [0092 ページはモジュールになる — テストできるように](0092-the-page-is-modules-so-it-can-be-tested.md) — **Superseded by 0130**。
- [0126 ブラウザは、どこから来たかを言う](0126-a-browser-says-where-it-came-from.md) — **Superseded by 0130**。
- [0006 Web UI の二つの配信経路](0006-web-ui-serving.md) — **Superseded by 0072**。
- [0014 フォルダブラウズ](0014-folder-browse.md) — **Superseded by 0046**。
  prefix によるツリー閲覧。
- [0021 ナレッジの move と Web UI の微調整](0021-move-and-webui-refinements.md) — **Superseded by 0075**。
- [0044 Web UI は文書を編集する](0044-web-ui-edits-documents.md) — **Superseded by 0072**。

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
- [0118 一回の呼び出しが、必要なものを全部運ぶ](0118-a-call-carries-everything-it-needs.md)
  — **Accepted**。0067 §3 と [0065](0065-identity-and-provenance.md) §4 を
  改訂する。**`/mcp` を stateless にし、MCP プロトコル 2026-07-28 で
  答える。** SDK は stateless なハンドラからしか新プロトコルを出さず、
  session を持つハンドラは 2025-11-25 に上限を掛けるので、ochakai は
  最新の SDK を積んだまま最新の仕様を拒んでいた — しかも黙って(交渉は
  成功し、ツールは動き、降ろされたことはどこにも出ない)。**手放すのは
  一度も使っていないモードである**(roots も sampling も logging も
  使わず、どれも 2026-07-28 で非推奨。ochakai の状態は接続ではなく
  ナレッジベース)。得るのは新プロトコルと、idle session が溜まらない
  ことと、**インスタンスを増やせること**(session はメモリにあったので
  `max_instance_count = 1` は選択ではなく帰結だった)。`Mcp-Session-Id`
  は発行も参照もせず、GET と DELETE は 405。**代償は明示する**: 
  2025-11-25 では名乗りが `initialize` にしかなく、それは使い捨ての
  session で消えるので、古いクライアントの `clientInfo` は producer に
  届かない(実測 — `claude-desktop/1.2` が空になる)。落ちるのは 0065 §4
  が「認可でも identity でもない、記録である」と呼んだ**自称**の側で、
  `created_by` は動かず、`Ochakai-Producer` が代替で、クライアントが
  移った日に自動的に戻る。数える表面は一つも動かない。
- [0068 面はどう足され、どう降ろされるか](0068-how-a-face-is-added-and-removed.md)
  — **Accepted**。**面を足す規則と降ろす規則の現行ドキュメント**(0050 /
  0056 / 0058 / 0062 の四冊を一冊にまとめたもので、決定は一つも動いて
  いない)。能力とコマンドは一対一(一つなら畳み、二つなら割る)、一覧は
  検索ではない(cursor は一覧だけ、順位は `limit` が契約)、通行量の無い
  入口は降ろす(`min_score` の廃止、`fm.` を MCP から)、裁定は
  `POST /api/v1/review/{id}` 一本から下し `withdraw` 一語で言う。
- [0110 第一列は、並べてくれと言った鍵である](0110-the-first-column-is-the-key-you-asked-for.md)
  — **Accepted**、**BREAKING(CLI)**。`ochakai search` と
  `ochakai list --source` / `--links-to` の人間向け出力から第一列を
  落とす。0068 §3 が `min_score` をワイヤから降ろした理由
  (「スコアはモード依存で較正されていない」)が、**同じ数を印字して
  いた面には及んでいなかった** — レキシカルのみなら 0〜2、融合なら
  `rrfK = 60` から上限 0.049 で、**同じクエリが埋め込むデプロイでは
  0.033、埋めないデプロイでは 1.730 を出すのに、行はどちらかを
  言わない**。逆引きの列は常に `0.000` だったので同時に落ちる。
  フィードの第一列は**残る**(読み手が並べてくれと言った鍵で、上下と
  比較できる)。`--json` と REST の `score` は不変。Web UI は生の
  スコアを一度も描画しておらず同じ 0.05 で lexical / 融合を判定して
  おり、MCP は説明文で触れていない — **生の数を見せていたのは CLI
  だけだった**。順位を第一列にする案・正規化する案・モードを添える案は
  採らない。面の数は一つも動かない。
- [0111 目のための重みを、目にだけ出す](0111-weight-for-the-eye-and-only-for-an-eye.md)
  — **Accepted**。0110 が第一列を落とした行に、**太字と dim だけ**を
  足す。住所(URI・`browse` のセグメントとファイル名)は太字、行の末尾に
  続く散文(`— description` と `· snippet`)は dim、その間の値
  (`status`・`type`・フィードの第一列)は素のまま — 比べる列を薄く
  しないためである。**出るのは標準出力が端末のときだけ**で、パイプ・
  リダイレクト・`--json` が受け取るバイト列は 1 バイトも動かないので、
  0067 §5.3 が断る TUI にも当たらず、互換性の手続きも要らない
  (0110 が BREAKING だったのと対照的)。**エスケープを剥がせば今日と
  同じ行**であることがこの決定の全体で、テストがそれを検査する。色相を
  使わないのは、端末の配色が利用者のものであり、0094 の「色だけが担い
  手になってはいけない」を満たすためである。`status` の色分け・却下行の
  区別(0104 が同じ綴りだと決めている)・クエリ語のハイライト(ベクトルで
  当たった行に嘘の下線を引く)・`get` / `stats` の装飾は採らない。
  `--color` フラグも足さない — 止める側の綴りは既存の慣習
  `NO_COLOR` を読むだけで、**ENV が 14 → 15** になる唯一の代金である。
- [0076 二つのツールが MCP から降りる](0076-two-tools-leave-mcp.md) —
  **Accepted**、**BREAKING(MCP)**。`delete_concept` と
  `get_concept_usage` を降ろし、MCP のツールを 8 本から 6 本にする
  (0068 §3「通行量の無い入口は降ろす」の、パラメータではなくツールへの
  適用)。削除は**裁定**であり、この面は取り消せる裁定
  (`POST /api/v1/review/{id}` の verify / reject)すら載せていないのに、
  取り消せないほうだけを配っていた — 却下を任せられないエージェントに
  削除を任せる理由は無い。利用回数はループの**人間側**で、エージェントが
  頼ってよいかを判断するための trust tier と `verified_at` は検索ヒットと
  `get_concept` に既に載っている。**能力が落ちるのは MCP からだけ**で、
  REST・CLI(`ochakai delete` / `ochakai usage`)・Web UI には一つも
  欠けない。0067 §5.1・§6・§7 を改訂する。
- [0109 ディレクトリに閲覧者と編集者を持つ](0109-a-directory-has-readers-and-writers.md)
  — **Accepted**。0065 §1 が自分で置いた改訂条件
  (「『読めるが書けない人』が要るようになったら」)が満たされ、
  **prefix ごとの閲覧・編集の付与**を持つ。同じ一つのバンドルの中に
  「見せてはいけない知識」と「変えられては困る知識」が同時にある利用者が
  現れ、デプロイを分ける既存の答えは共有語彙を `unverified` のコピーに
  落とす(0075 §3.1)ので使えなかった。表は**付与だけ**で deny を持たず、
  セグメント境界の照合は 0075 §6 の規則をそのまま使う。**付与が一つも
  無いデプロイは 0065 のままである** — 最初の一行が全員に対して同時に
  境界を入れる。管理者は `OCHAKAI_ADMINS`(ポリシーを編集できる者を
  ポリシーの中で決めることはできない)で、バンドル全体を取る操作
  (stats・export・move・reembed・ポリシー自身)は管理者に寄せる。
  スコープ外の読みは **404**。**塞げないものが一つあり、そう書いてある**:
  可視な concept の本文が隠した id を名指していれば読める(0075 §3 が
  サーバによる文書の書き換えを禁じている)。
- [0129 move は、書き換えが収まるとき動く](0129-a-move-runs-when-its-rewrite-fits.md)
  — **Accepted**。0109 §3 の「バンドル全体を取る操作は管理者のもの」から
  `move` を外す。**断っていた理由は正しく、対象が広すぎた**: §3 の文は
  「書き換えが他人のスコープに及ぶと」であって、及ぶかどうかは concept
  ごとに違い、**及ばないほうが普通**である — ディレクトリとは互いを
  参照し合うものを一つの場所に置いたもののことだからで、
  [0123](0123-the-numbers-say-what-they-counted.md) §4 も同じ文を引いて
  両者を分けていなかった。決定は、書き換えが触れる三つ — 動かす concept・
  移動先・**その concept を指している live な concept すべて** — が
  呼び出し元の書ける範囲にあるとき動く、である。移動先だけは**名指して
  403**(まだ何も無い場所なので、中身について何も答えない)。**はみ出すなら
  丸ごと断り、絞らない** — 絞れば飛ばしたリンクが消えた id を指し、
  部分的な move は断られた move より悪い。判定は書き換えと同じ
  トランザクションの中(`rewriteReferences` が `FOR UPDATE` で読む集合その
  もの、[0124](0124-a-directory-can-have-its-own-administrator.md) と同じ
  形)で、問い合わせは増えない。**漏れる一ビットを引き受ける**: 範囲外から
  参照されているという事実で、アドレスも件数も中身も無い — 0123 §3 が
  miss を差し止めたのと逆の答えになるのはそこである。面はどれも動かない。
  却下: 絞って通す、範囲外への書き込みを別の actor 名で行う(台帳に
  「システム」は無い)、件数を 403 に載せる、読める範囲を基準にする、
  `reembed` も同時に割る(詰まった人がいない)。
- [0127 アーカイブは、自分がどの部分かを言う](0127-an-archive-says-which-part-it-is.md)
  — **Accepted**。0109 §3 の「export は管理者のもの」を改訂する — **全体は
  管理者のまま、subtree は読める者のもの**。**測ったら危険の形が違った**:
  `ochakai import` は加算的で削除しないので「戻したときに残りが消える」は
  起きず、本当の危険は**部分と全体が同じ形で届くこと**だった — 実測すると
  subtree アーカイブも根の `index.md` を持ち、名前も同じ
  `ochakai-okf.tar.gz` で、**どちらかを言うものが一つも無い**。つまり
  0109 §3 が恐れた事態は管理者だけが使える今日すでに作れており、断ることは
  それを一度も防いでいなかった。決定は、読む人が出会う二箇所に書くこと —
  根の index の frontmatter `bundle_scope`(機械)と本文の一文(人)、そして
  ファイル名 `ochakai-okf-<subtree>.tar.gz`(**復旧する人は展開する前に
  ファイル名を打つ**)。深い階層の index も全体のアーカイブも一字も
  変わらない。見分けが付く以上、断ることが買うものは無くなる — subtree の
  中身は呼び出し元が concept を一つずつ取れば既に得られるもので、断って
  いたのは知識ではなく便宜だった。**根は 403、読めない subtree は 404**。
  これで [0119](0119-an-operated-fleet-is-deployments-or-directories.md) §4 が
  代金に数えた「出口(C1)に運用者が要る」が返る。面はどれも動かない
  (`--prefix` は既にある綴り)。却下: import に読ませて警告(予約ファイルを
  読み戻さない決定を曲げ、手書きバンドルに誤警告が出る。ファイル名のほうが
  早く届く)、re-root(id とリンクが壊れ、卒業経路も壊れる)、別コマンド、
  スコープ持ちに「見える範囲の全体」を渡すこと、署名やマニフェスト。
- [0124 ディレクトリは、自分の管理者を持てる](0124-a-directory-can-have-its-own-administrator.md)
  — **Accepted**。0109 §3 の床を**一つの subtree について**改訂する。
  払っていた代金は「ディレクトリを一つ預けるのにバンドル全部を預ける」
  ことで、チームに境界を任せれば他の全チームの境界を消す権限も渡り、
  組織ごとに一行足す自動化([0119](0119-an-operated-fleet-is-deployments-or-directories.md))は
  恒久的に完全な管理者でいる必要があった。決定は付与の三つ目の力
  `may_admin` — **この prefix 以下の規則そのものを編集してよい**、
  `may_write` を含み、**根には置けない**(`prefix: ""` の may_admin は
  「ポリシー全体を誰が編集できるか」をポリシーに置くことで、それが
  0109 §3 の回転扉そのもの)。**床は一字も動かない**: prefix 管理者を
  作れるのも取り上げられるのも完全な管理者だけで、外へ出る道が無い。
  **丸ごと置き換える文書との組み合わせが要点**で、prefix 管理者は自分の
  subtree の規則だけを読み、**外の規則は書き込みを跨いで保たれる** —
  見えなかったものについて、その文書は何も言っていない。分割も前提条件も
  同じトランザクション・同じ advisory lock の下(0120 を開け直さない)、
  **版は読んだものを覆う**(見えない変化で 412 にならない)、外の prefix を
  含む文書は**黙って落とさず 403**。自分より浅い規則は自分のものではない —
  自分を含む規則を編集できれば subtree はバンドルへ育つ。§4 に塞げない
  ものを書いた: 浅い規則による実効的なアクセスは、自分に見える規則より
  広いことがある。移行 0045 は追加的で既定 false、**数える面はどれも
  動かない**。却下: prefix 管理者に export・move・reembed を開くこと、
  浅い規則を見せること、付与の履歴、`OCHAKAI_ADMINS` に prefix を書くこと。
- [0123 数は、何を数えたかを言う](0123-the-numbers-say-what-they-counted.md)
  — **Accepted**。0109 §3 の「バンドル全体を取る操作は管理者のものになる」
  から **`stats` だけを外す**。0109 §3 の理由(部分集合はループを測る数に
  二つ目の意味を与える)は正しく、**対象が広すぎた** — `stats` は 0041 以来
  `prefix` を取り、部分集合はその日も出せた。二つ目の意味を作っていたのは
  部分集合の存在ではなく、**答えがどちらを運んでいるかを言わないこと**で
  ある。決定は、応答が `scope`(数えた prefix、全体なら載らない)を持ち、
  範囲を持つ呼び出し元は自分の数を読めること。**管理者が `prefix` で訊いた
  ときも同じ宣言が付く** — 欄は数についてであって呼び出し元についてでは
  ない。**空の一覧は「あなたには何も見えない」**で、全部ゼロなのが「何も
  起きなかった」ではないと言うためにこの形が要る(空の prefix 一覧は
  フィルタでは全体を意味する)。[0114](0114-a-degraded-ranking-says-so.md)
  が退化したランキングに置いた形の再適用である。**絞れない二つは絞れないと
  言う**: miss は id を持たない(0069 §5.1)ので差し止め(`withheld`)、
  ゼロにはしない — `recording: false` と別の語なのは直し方が違うから
  (設定を変える / 管理者に訊く)。dropped は全体のまま(失われた時点で
  何についてか分からず、上の数の信頼度の脚注である)。**export・`move`・
  `reembed`・ポリシー自身は管理者のまま** — それぞれ理由がまだ効いており、
  export は「欠けたアーカイブもバックアップと呼ばれる」ので宣言では直らない。
  §5 で一つ直した: `Queues` の注釈が「prefix で絞られない」と書いていたが
  store は既に絞っていた。応答専用スキーマへの追加なので凍結の外
  ([0082](0082-what-the-freeze-holds-still.md) §2)で、数える面はどれも
  動かない。却下: miss を絞って見せる、403 のまま据え置く、`scope` を常に
  載せる、`withheld` を `recording` に畳む、export も同時に割る。
- [0120 ポリシーは、読んだままの姿でだけ置き換わる](0120-the-policy-is-replaced-only-as-it-was-read.md)
  — **Accepted**。0109 §2 の「文書一枚を丸ごと置き換える」に**前提条件**を
  足す。二人が同じポリシーを読んで一行ずつ足すと、後に保存したほうの文書に
  前者の行が無い — 0109 §2 はこの形を予期し、**concept では If-Match が
  既に解いていることまで書いていた**。足す理由は
  [0119](0119-an-operated-fleet-is-deployments-or-directories.md): 付与を
  置くのが人ではなく自動化になると、この競合が通常の経路になり、しかも
  落ちた組織が見るのは 404 なので原因の側から何も見えない。`If-Match` は
  concept と同じ意味(不在なら後勝ち、不一致は 412 で何も書かない、`*` は
  400)で、`If-None-Match` は 400 — 常に存在する文書に作成専用の前提条件は
  意味を持たない。**版は付与だけを数え、`granted_at` を数えない**(付与の
  変わらない書き込みが誰かの前提条件を無効にしないため)、**順序は Go で
  課す**(SQL の照合順は同じ関数ではない)、**空のポリシーにも版がある**
  (最初の一行こそ二つの自動登録が競う書き込み)。比較は置き換えと同じ
  トランザクションの中で、空の状態を守るため advisory lock の下で行う。
  版は `ETag` と本文 `version` の両方に載り(concept の
  `summary.content_hash` と同じ)、要求本文の `version` は `readOnly` と
  宣言して読まない。**面は一つも増えない** — HEADER も FLAG も名前で
  数えており、`If-Match` は既にそこにいる。§6 で一つ直した: 付与を持たない
  デプロイの `GET /api/v1/access` が `{"rules": null}` を返しており、契約は
  必須の配列と言っていた。**空のポリシーを wire で読むテストが無かった**
  ので契約チェックに見るものが無かった。却下: 行ごとの操作、本文の版を
  前提条件にすること、`If-Match` の必須化、ポリシーの履歴、競合時の併合。
- [0122 最初の一行を置けるのは、管理者だけである](0122-the-first-rule-is-an-administrators-to-write.md)
  — **Accepted**。0109 §3 の管理者の床を、設定の側からではなく**呼び出し元の
  側から**読み直す。付与が一つも無いデプロイでは全員が入口の管理者検査を
  通る(0109 §2)ので、`OCHAKAI_ADMINS` の外の呼び出し元が最初の一行を
  書くと、**行はコミットされ、ログは「置き換えた」と言い、呼び出し元には
  403 が返っていた** — 出口でもう一度ポリシーを読み、いま自分が作った
  境界に照らして拒まれるからである。403 は「何も書かなかった」と言う符号
  なので、自動化はそれを再送し、そのたびにポリシーを置き換える(0119)。
  決定は**書く前に断る**こと: 最初の一行を置いた瞬間からポリシーは
  `OCHAKAI_ADMINS` のものになるので、その一行を置けるのも
  `OCHAKAI_ADMINS` である。**互換性の話ではない** — ここで断る呼び出しは
  これまでも例外なく 403 で終わっており、変わったのは行が残らなくなった
  ことだけである。より一般的な規則をもう一つ残す: **書き終えた操作の
  戻り道に、拒否できる検査を置かない**(出口の再読み込みは管理者検査を
  通らなくなった)。付与を空にする置き換えも、管理者を名指さないデプロイ
  の 400 も動かない。Web UI のタブは付与が無いあいだ全員に出たままで、
  **保存の失敗が理由を名指す**ことで半分だけ 0109 §6 に従う — 従い切ると
  「あなたは管理者か」を応答に書くことになり、認可の答えが二箇所になる。
  却下: 出口の検査だけ外すこと、締め出しを受け入れて 200 を返すこと、
  400 で断ること、最初の一行を書いた本人を自動的に管理者にすること。
- [0108 context pack は退役する](0108-the-context-pack-retires.md)
  — **Accepted**。**BREAKING(REST 非コア・MCP・CLI)**。
  REST の `/context`・MCP `get_context`・CLI の context コマンドを
  全面から同時に退役させる。0076 と違い通行量はあった — 降ろす根拠は前提の古びで
  ある: pack は「往復は高価」という前提の機構で、何を読むべきかを
  サーバが先に決めるが、search → get を自前で反復できるエージェントから
  その判断を取り上げていた。pack が同梱していた逆向きの hop は concept
  自身の `linked_from`(0106)が、書き戻しの hint は `get_concept` の
  応答(0096 §3 の改訂)が引き継ぐ。需要シグナルは配られた pack より
  選ばれた fetch のほうが正直になる。縮小版の pack は作らない。
  見直す条件: シェルも MCP の反復も持たないホストが一回の HTTP で
  「読むべき一式」を要すると実際に詰まった一件。
- [0093 予算は応答全体を縛り、一位は名前だけでは返らない](0093-the-budget-governs-the-whole-response.md) — **Superseded by 0108**。
- [0098 ジェネレータは動く。欠けていたのは宣言だった](0098-the-generator-works-what-was-missing-was-the-declaration.md)
  — **Accepted**。0064 §11 の「どんなジェネレータもこの一つの住所に
  ついては動くクライアントを生成しない」を取り下げる。`/api/v1/bundle/{path}`
  の `path` を宣言していたのは `get` だけで、`put` と `delete` は宣言して
  いなかった — **契約が三つの操作について三つの違うことを言っていた**。
  ジェネレータはそこで止まっており、大きさでも凍結でもなかった。path item
  直下に一度だけ宣言する形に直すと、生成もコンパイルも通り、
  スラッシュを含む id への GET / PUT / DELETE が実際に通る(`%2F` を
  `ServeMux` が復号する)。**凍結は破っていない** — 消えた行も変わった行も
  無く、増えた 2 行は、呼び出し側が最初から送っていたものを契約が
  言うようになった分である。確かめたのは oapi-codegen(Go)一つで、
  言えるのは「少なくとも一つは動く」まで。`{path}` を OpenAPI で正しく
  表現し直すことと、ジェネレータを CI に足すことはしない。
- [0097 書き込みは、自分が何をしたかを本文で言う](0097-a-write-says-what-it-did-in-the-body.md)
  — **Accepted**。0074 §5 を改訂する。`created` / `updated` / `unchanged`
  の一語が**応答本文にも**乗る(REST は本文の `plan`、MCP は
  `knowledge.plan`)。ヘッダは**残る** — 0082 §2 が凍結の対象に
  「ヘッダ名」を挙げており、外すのは 1.0 の仕事である。つまり issue が
  書いた「ヘッダから body へ**移設**」の半分しか取れず、取れるほうを
  取った。理由は W5 の通りで、`curl` はヘッダを印字せず `jq` は消し、
  **MCP にはヘッダが無い** — 書き戻したエージェントは「それで何か変わった
  のか」をもう一度読む以外に知る手立てが無かった。三語は
  `internal/restapi` と `internal/apiclient` に二重に書かれていたので
  `internal/domain` に移し、**移したとたん `VOCAB` が 46 → 49 になった** —
  新しい語ではなく、ずっと教えていたのに数える場所に無かった語である
  (0083 の `error.*` と同じ形)。ファイルの書き込みには乗せない(本当の
  書き込みはバイト列を比べないので `unchanged` を言えず、片方の経路にしか
  無いフィールドはヘッダより悪い)。`move` と `review` にも乗せない。
- [0113 読み替えは、本文でも言う](0113-a-reinterpretation-says-itself-in-the-body.md)
  — **Accepted**。0097 §4 が「別の記録で決める」と残した `Ochakai-Note`
  を、同じ取引で決める。書き込みの応答本文に **`notes`** が乗り(`View`
  のフィールド、読み替え一つにつき一行)、**ヘッダは残る** — 0082 §2 の
  住所なので外すのは 1.0 である。効いたのは Web UI で、**キュレーション
  の面が読み替えを一度も受け取っていなかった**: 直せるのは編集した人
  だけなのに、値はヘッダにしか無く、ページはそれを読んでいなかった。
  保存のトーストがそれを言うようになる(`plan` を読むのと同じ経路。
  この文のときだけ表示を伸ばす)。**MCP は動かさない** — 既に本文で
  返しており、`notes` と `knowledge.notes` の深さの違いは面ごとの答えの
  形であって、揃える先が両方とも悪いなら借りではない。`text/markdown`
  の応答(置き場所が無い)と CLI(両方の表現でヘッダから読めている)は
  ヘッダのままで、`VOCAB` も `HEADER` も動かない。
- [0096 MCP でも、一覧は検索ではない](0096-a-listing-is-not-a-search-here-either.md)
  — **Accepted**、**BREAKING(MCP)**。0068 §1 の「能力が二つならコマンドも
  二つ」を MCP に適用し、§2 の「ワイヤは一本のままである」がこの面には
  及ばないことを決める。`search_concepts` から `list_concepts` を割り、
  ツールは 6 → 7。**能力は一つも増えていない** — 割ったのは、一つの
  スキーマが「`query` は `sort` が無いとき必須」「`cursor` は一覧だけ」
  「`limit` の既定は二通り」を**散文でしか言えなかった**からで、
  誤った組み合わせはエージェントのターンを一つ使って初めてエラーに
  なっていた。いまは `sort` を `search_concepts` に送った時点でスキーマ
  検証が落とす。MCP がワイヤ側ではなくコマンド側に付くのは、**モデルが
  毎回同じ散文を同じ確率で読み違える**からである(人は一度覚えれば
  済む)。代金は隠れない: フィルタ七つが二重になり常駐 11,829 → 13,433
  バイト、`MCP-BYTES` 12,000 → 13,500 で、**この面で天井が上がるのは
  初めてである**。`get_context` の毎回のヒントは残す(常駐ではなく、
  0069 がそこに置いた機構)。破壊に猶予は無い — 0088 の窓は名前の改名の
  仕組みで、変わったのは引数だからである。
- [0077 ファイルの住所は一つである](0077-one-address-for-a-file.md)
  — **Accepted**。**CLI がファイルを名指す綴りの現行ドキュメント**。
  `ochakai attach` / `ochakai detach` を `ochakai put` / `ochakai delete`
  に畳む(CLI 26 → 24、新しいフラグは無し)。ファイルはバンドルパスで
  名指し、書き込みで concept かファイルかを決めるのはサーバーと同じく
  バイト列である。本文に貼る相対リンクのヒントは `put` が引き継ぐ —
  帰属が本文からの導出である以上、誰もリンクしないファイルは誰にも
  見つからない。Web UI のボタンも同じ語に揃える。
- [0004 リモート CLI](0004-cli.md) — **Superseded by 0067**。
- [0007 DB 直結コマンドの廃止](0007-api-only-cli.md) — **Superseded by 0067**。
- [0015 サーフェス一貫性の方針](0015-surface-consistency.md) — **Superseded by 0067**。
- [0058 誰も通らなかった二つのフィルタ](0058-filters-nobody-arrived-through.md) — **Superseded by 0068**。
- [0102 履歴は一つの綴りで答える](0102-one-history-in-one-spelling.md)
  — **Accepted**。**BREAKING**。`?history` は Accept が何であれ JSON で
  答え、markdown のレンダリングは無くなる。OKF SPEC §9 が markdown の
  履歴を一つ定めており(オブジェクトの隣の `log.md`)、`?history` は同じ
  台帳の行から二つ目を描いていた。重複そのものより、**それが上限を二つに
  割っていた**ほうが重い —— 0064 §14.1 が書き残したとおり、同じ
  `?history&limit=500` が概念に対して JSON では 400、markdown では 200 に
  なる。あの節の決定は「コードは変えず、スペックを直す」で、食い違いの
  記述であって解消ではなかった。落ちる能力は「一オブジェクトの履歴を
  markdown で読む」ことで、**製品の中でそれを使っていた面は一つも無い**
  (`ochakai revisions` も Web UI の履歴パネルも元から JSON)。もう半分は
  凍結の規則で、0064 §11 に**三つ目の理由**が付く: 規格が綴りを定めて
  いるものの二つ目の綴りを畳むこと。三条件(規格が定めている・ochakai に
  二つ目がある・畳むのは ochakai 側)を全部満たす場合に限り、**前の二つ
  より弱い理由である**ことも記録が書く —— 「同じ事柄か」の判断が ochakai
  の側にあるからで、だから畳めるのは表現だけで住所は畳めない。指紋は
  一行も動かない(`text/markdown` の行は概念のエクスポート形と共有)。
- [0101 一覧は歩ける — 凍結はそれを止めていなかった](0101-a-level-can-be-walked.md)
  — **Accepted**。`index.md` を JSON で読む一覧が `cursor` を持つ。
  [0068](0068-how-a-face-is-added-and-removed.md) §2.1 は一覧について
  「保証するのは歩き切ることではなく、**上限で静かに止まらないこと**」と
  既に決めていたが、バンドルの一覧は別の操作なのでその規則が一度も届いて
  いなかった —— 1000 件で切って `truncated: true` を立てるだけで、**先へ
  行く綴りが無かった**(静かではないが、壁ではある)。**ページの大きさは
  1000 のまま**にしたので、今日収まっている呼び出し元は今日と同じものを
  受け取り、壁に当たっていた側だけが歩けるようになる。cursor は**二つの
  走査**(概念は id 順、ファイルは path 順)と**ディレクトリ**を運ぶ —
  サブディレクトリは `GROUP BY` の結果で位置を持たないので初回のページ
  だけに載り、別の階層の cursor は 400 で両方を名指す。markdown の
  `index.md` は歩かない(SPEC §8 に「続き」の綴りが無い)ので、そこへの
  `?cursor=` は 400 である。もう半分は**凍結の規則**で、`cursor` という
  新しいクエリパラメータが要ったこと自体がそれを露わにした:
  [0082](0082-what-the-freeze-holds-still.md) は応答側の追加だけを外に
  置いていたが、0064 §2 が未知キーを 400 にした理由は「**凍結後に何かを
  安全に足せるかどうかを決める**」であり、**任意のクエリパラメータの追加**は
  最初から凍結の外だった。検査は三つに絞る(`query` のみ・`required=false`
  のみ・パラメータ一本の行のみ)。`truncated` は残す — 応答からの削除は
  凍結の本体で、1.0 の仕事である(0097 §2 が `Ochakai-Plan` でした取引と
  同じ)。指紋は 2 行増え、表面の数は一つも動かない。
- [0100 `.md` は concept の綴りである](0100-md-is-how-a-concept-is-spelled.md)
  — **Accepted**。**BREAKING**。`.md` の住所に
  座れるのは concept と予約名だけであり、`type` を持たない `.md` の PUT は
  400 になる。動機は適合である — 今日の書き込み面は `type` の無い `.md`
  を**その住所のまま**ファイルとして保存し、export がそれを載せるので、
  出てきたバンドルは OKF SPEC §11.1・§11.2 を落とす。§11 が consumer に
  禁じている拒否は五つ(任意フィールドの欠落・未知の `type`・未知の追加
  キー・壊れたリンク・`index.md` の不在)で、frontmatter の不在はその
  どれでもない — 0075 §3.3 はその寛容を書き込み面に広げたが、広げた先が
  元の外にあり、しかもここでの ochakai は consumer ではなく producer で
  ある。保存済みのファイルは移行で `.markdown` に改める(バイト列は出て
  くる、綴りだけが動く。0075 §2 の不変条件は壊れない)。**凍結を破る**
  ので、0064 §11 の「破ってよい唯一の理由はセキュリティ上の欠陥」に
  二つ目 —— **出力が OKF に適合しないこと** —— を足すことを同時に求める
  (判定は SPEC の三条件に対する yes / no なので、ochakai の都合で動かな
  い)。見どころは、**破壊的変更でありながら golden の diff がゼロ**で
  あること: 住所ごとに違う `requestBody` を OpenAPI が綴れないので、
  0064 §21 の「凍結が守れるのは『何が在るか』であって『どれを選ぶと何が
  返るか』ではない」がそのまま出る。表面の数は一つも動かない。同じ PR で
  0064 §20.4 の隙間(前提条件が読まれないまま消える DELETE)と、
  §16 が本文を覗いて種類を決めていた分岐と、`missingObject`(concept の
  住所で 404 と 501 を取り違えないための翻訳)が畳まれた。
- [0082 凍結が止めているのは何か](0082-what-the-freeze-holds-still.md) —
  **Accepted**。0064 §11 の「凍結を破ってよい唯一の理由はセキュリティ上の
  欠陥である」を改訂する。凍結が守るのは**住所・リクエストの形・応答からの
  削除と変更**であり、**応答専用スキーマへのプロパティ追加はその外**である
  — 0064 §2 が未知のクエリキーを 400 にしたのは「凍結後に何かを安全に
  足せるかを決める」ためで、その土台の上では知らない応答キーを既存
  クライアントは無視するだけだからである。応答の `required` はサーバの
  約束であってクライアントへの要求ではない、が向きを分ける理由。応答
  *専用*に限るのは、`requestBody` から到達できるスキーマへの必須追加は
  0064 §2 の下で 400 を生むから。追加は今までどおり golden の再生成を
  要求するので diff に出る。きっかけは C7 の計器で、取りこぼし件数を
  `stats` に載せる先が REST 以外に無かった(CLI も Web UI も REST の
  クライアント、0067 §2)。
- [0083 エラーは機械が読む符号を運ぶ](0083-an-error-carries-a-code.md) —
  **Accepted**。エラー応答が `error`(人のための一文、いつでも書き直して
  よい)の隣に `code`(クライアントが分岐してよい 12 語の閉じた語彙)を
  運ぶ。三つの条件が 409 を共有しており、区別する手立てが散文の照合しか
  無かった — しかも compatibility.md はその散文が minor で変わってよいと
  宣言していて、REST を埋め込む開発者(C6)に壊れると分かっている方法しか
  渡していなかった。ステータスが条件を決めている場合も符号は繰り返す
  (一部にしか付かない鍵は分岐の道具にならない)。RFC 9457 を採らないのは
  メディアタイプが動くこと・`type` の URI がドメインの生存を契約に持ち込む
  こと・運ぶ情報が同じことの三つ。0082 が凍結の外と定めた応答専用スキーマ
  への追加であり、VOCAB は 34 → 46 に上がる。
- [0088 名前が変わっても、一リリースは答える](0088-a-retired-name-answers-for-one-release.md)
  — **Accepted**。**MCP と CLI の安定性契約の現行ドキュメント**。あるリリースが
  MCP のツール名か CLI のコマンド名を改名したら、古い綴りはそのリリースの
  あいだ答え続け、次のリリースで消える。それまでの規則は「猶予期間は無い」で、
  正直ではあったが、**壊れる側に告知が届かない** —— エージェントの設定は
  ツール名を名指しで持っていて、CHANGELOG を読むのは人だけである(C5・C4)。
  要点は §3: **呼べるが、載らない**。`tools/list` も usage も `docs/cli.md` も
  補完も古い名前を出さないので、まだ知らない側は永久に知らず、ツール説明文の
  コンテキスト代([0067](0067-four-faces-and-what-they-decline.md) §1)を
  二重に払わずに済む。実装がミドルウェアなのはそのため —— 登録した時点で
  `tools/list` に出てしまう。各エントリは改名したリリース番号を持ち、
  CHANGELOG の最新リリースより古くなるとテストが落ちるので、**窓を閉じるのは
  release 準備 PR の仕事**になる(§4。猶予期間の失敗は破られることではなく
  閉じられないことである)。遡らない(§5)。対象は名前だけで、フラグも
  出力の形も保存形も含まない。[0082](0082-what-the-freeze-holds-still.md) が
  REST 側で緩めたぶんとの釣り合いであり、面の数は動かない。
- [0125 本文は、鍵を一度だけ名指す](0125-a-body-names-each-field-once.md)
  — **Accepted**。0064 §2 の「宣言していないボディキーは 400 で名指す」を、
  **書かれたとおりに効かせる**。規則の追加ではなく、規則を実施するはずの
  機構に半分しか無かったという話である: `encoding/json` は鍵を大小を
  区別せずに突き合わせるので `{"RULING":"verified"}` が 200 になっていた
  (`DisallowUnknownFields` は**当たってしまう鍵については何も言わない**)。
  同じ関数のもう一つが、繰り返された鍵の最後の値を黙って採ること —
  §20.2 が `?limit=10&limit=50` について「求められたとおりの答えは無い」と
  言ったのと同じ問いで、向きが一つに決まっているぶんだけ悪い。**決定は
  二つとも 400 で鍵を名指す**で、配列として反復してよいというクエリ側の
  例外は本文には無い(JSON は集合を配列で綴れる)。機構は
  `encoding/json/v2` の既定で、**読む側だけ**を移す —— v2 は marshal の
  既定も変えており、それは凍結された面が運んでいるバイト列である。
  0107 の凍結の範囲(OKF コア)は動かず、触れる四操作はその外にある。
- [0107 凍結が握るのは OKF コアである](0107-the-freeze-holds-the-okf-core.md)
  — **Accepted**。凍結の範囲を bundle の往復(`GET` / `PUT` / `DELETE
  /api/v1/bundle/{path}`)と `GET /api/v1/search` の 4 操作に狭め、残りの
  `/api/v1` を 0.x の不安定な面(BREAKING + 記録 + minor)に戻す。
  **公開した約束の撤回であり、この corpus で初めての種類の記録** — 0082 の
  「解釈の訂正」の語り口は使えないので、そう呼ばない。根拠は二つ: 凍結は
  14 日で免責を三つ持ち、その向きが全部 OKF だったこと(境界が間違った
  場所に引かれていた)、そして 0082 §5 自身の「規則の欠陥を直す代金が
  今より安いことは二度とない」。コアの判定は列挙ではなく基準 —
  **ナレッジがそこに在ることに直接触れる操作**(住所空間の読み書き削除と、
  見つける入口)。新しい約束は前より硬い: コアは広がる向きにしか動かず、
  縮める記録はもう書けない。ワイヤは 1 バイトも動かない。
- [0064 REST は /api/v1 で止まる](0064-rest-stops-at-api-v1.md) —
  **Accepted**。REST を凍結
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
  同じ最後の機会として、`title` が wire でも任意になり(§15、OKF SPEC
  §4.1 は既定を与えていない。既定を持つ `status` は解決したまま)、
  `observed.generated.at` が行の書き込み時刻ではなく内容が最後に変わった
  時刻を報告するようになり(§17、SPEC §5.2)、型に `/` を許すようになる
  (§18、SPEC §4.1・§11)。追加として、PUT がようやく `requestBody` を
  宣言し(§16 — 宣言が無いままでは動く書き込みクライアントを生成できず、
  契約テストはバンドルの PUT を全部素通りさせていたので気付けなかった)、
  サーバが前から返していた `Ochakai-Read-Only`・400・413・
  `Cache-Control`・`X-Content-Type-Options`・`Content-Security-Policy`・
  304 の `ETag` を契約に書く(§19、HEADER は 10 → 13)。
  [0046](0046-bundle-address-space.md) §3.5 の代表表現の表を改訂する
  (概念の既定は JSON の View)。型の `/` については
  [0071](0071-the-recommended-type-vocabulary.md) §1 と
  [0074](0074-the-document-and-the-vocabulary-that-asks-it.md) §3・§7 を
  改訂する。[docs/compatibility.md](../compatibility.md)
  を同 PR で書き換え、REST だけが「不安定」の対象から外れる。
- [0033 context の hits は順位に徹する](0033-context-hits-are-a-ranking.md) — **Superseded by 0067**。
- [0103 ツールの答えは一通で運ばれる](0103-the-tool-result-travels-once.md)
  — **Accepted**。MCP のツールは `outputSchema` を宣言せず、答えは text
  コンテントブロック一つで返る。実測が動機である: MCP-BYTES が数えて
  いたのは 13,474 バイト、数えていなかった `outputSchema` が 14,986
  バイトで、**数えていない方が大きかった**。呼び出しごとにも同じことが
  起きていて、`get_context` 一回は同一の JSON を `content` と
  `structuredContent` に二通載せ、応答は 22,009 バイトだった。三つとも
  go-sdk の既定であり、戻り型を書いたことがスキーマを公開したことに
  なっていた(仕様は `structuredContent` の隣に後方互換の text
  fallback を勧めており、重複自体は仕様準拠である)。天井を上げて
  両方数える案は却下した — [docs/surface.md](../surface.md) の三つ目の
  問い「何が畳めるか」に、畳めるものがあるのに答えないことになる。
  MCP-BYTES は入力側と出力側の両方を数えるようになるが、出力側が空に
  なったので数は動かない。

## 検証ループと利用測定

- [0069 検証ループと、それを測るもの](0069-the-loop-and-what-measures-it.md)
  — **Accepted**。**検証ループと利用測定の現行ドキュメント**(0025 / 0029 /
  0037 / 0049 / 0051 / 0059 の六冊を一冊にまとめたもので、決定は一つも
  動いていない)。空にできる三つのキューと、キューではない `verified_at`。
  失敗報告は証拠にもとづく陳腐化、`stale_after` は書き手の宣言なので編集で
  しか空かない。利用測定は読み取りパスに触れず best-effort、ヒット 0 の検索は
  行として残り、`GET /api/v1/stats` がインスタンスを一回で答える
  (`prefix` はミス以外のすべてを絞る)。押すのは数であって通知ではない。
  利用測定は [0090](0090-a-queue-ranks-on-what-happened-lately.md) が改訂 —
  カウンタは残り、フィードの並びだけが窓の中に移る。
- [0090 キューは、最近あったことで並ぶ](0090-a-queue-ranks-on-what-happened-lately.md)
  — **Accepted**。0069 の利用測定を改訂する。利用カウンタは running total で
  **減らない**ので、そこにキューの並びを乗せると、ベースの最初の一か月に読まれた
  concept がデプロイの生きているあいだずっとプロモーションキューの先頭に居て、
  「次はこれを見よ」が「あなたが既に見たものを見よ」になる。`sort=usage` と
  `sort=failed` は**直近 90 日の数**で並び、生涯累積は残って同点を割る。
  数はワイヤに出て(`usage.recent`)、**窓を自分で運ぶ** — 期間の暗黙な数は
  読者が検算できない(0089 と同じ規則)。窓は**設定ではなく定数**で、生イベント
  の保持 180 日の内側でなければならない(保持より長い窓は「まだ残っている分
  だけ」を意味する)。保存された減衰列は取らない — 定期実行という可動部を
  この製品は持たない。`search_hits` に窓を掛けないのは、それが需要ではない
  から(ランカーの最近の出力が最近の需要になるだけ)。
- [0095 ループには形がある](0095-the-loop-has-a-shape.md)
  — **Accepted**。0069 の人間側のフローに時系列を加える。求められていたのは
  「キュー深度と検証率の時系列」だったが、**前者は取れない** — 三つのキューは
  いまの状態から数えており、先月そこに何件あったかを記録した表は無い。
  revision から再構成すれば**それらしく見えて正しくない数**が出る(0089 §4 と
  同じ理由で足さない)。そして 0069 は既にこう書いていた —「キューは何が
  残っているかを言い、何が通ったかは決して言わない」。**取れるものが、
  たまたま正しいものだった**: 検証は台帳に時刻付きで残り剪定されないので、
  ベースが在るかぎり遡れる。7 日ずつ 8 本、古い順。暦の週にしないのは最新の
  バケツが必ず途中になり、その落ち込みがキュレーターではなく暦のものに
  なるから。`days` に従わないのは、長さが変わると二回の測定が比較できない
  から。**誰も検証していないベースには出さない**(8 本のゼロは「止まった」と
  読め、「まだ始まっていない」は別のこと)。一件も無い週は 0 として並ぶ —
  group by だけだとその週は消え、両隣が隣同士として描かれる。Web UI は
  レビューページに一枚、**軸も目盛りも凡例も無い**(それ以上描けば 0067 §1 が
  BI ツールではないと言った当のものになる)。MCP には出さない。
- [0104 裁定は理由ごと運ばれる](0104-a-ruling-travels-with-its-reason.md)
  — **Accepted**。却下の理由(`reject --note`)は REST・MCP・Web UI・
  `ochakai context` には出ていたが、**`ochakai get` の出力と export した
  バンドルには無かった**。get は却下された concept をただの draft として
  印字し、バンドルは `rejected_by` / `rejected_at` は運んで「なぜ」を
  落としていた。裁定の三つの要素のうち次の書き手が使えるのは理由だけ
  なので、export に `rejected_note` を足し(同じ拡張キーの扱い、import は
  読み戻さないまま)、get は裁定を stderr に印字し、検索結果に却下が
  混じるときは一行でそう言う。**stdout の形も行の形も変えない** —
  検索のヒットに理由を載せる案は、凍結された応答を太らせるうえに
  ランキングの一行に散文を入れることになるので却下した。0009 §3.2 の
  「バンドルが運ぶのはナレッジであってインスタンスの判断ではない」は
  動かない。
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
  残す。**§3.2 の「GCS の blob は回収しない」は 0099 が改訂した。**
- [0099 purge の約束はバイト列まで届く](0099-a-purge-reaches-the-bytes.md)
  — **Accepted**。0031 §3.2 を改訂し、どこからも参照されていない blob を
  purge とファイル削除のあとに回収する。回収は一つの削除の副作用ではなく
  大域の掃除として走り、書き手との共有ロックと「バイト列が先、行が後」の
  順序で、生きている添付を消しうる競合を閉じる。サーフェスは増えない。

## セマンティックモデルと compile

- [0070 撤去した三つの面と、撤去してよい基準](0070-what-was-retired-and-why.md)
  — **Accepted**。**「やらないと決めたこと」の現行ドキュメント**
  (0012 / 0018 / 0028 を一冊にまとめたもの)。三つとも設計が間違っていた
  のではなく**費用対効果の前提が覆った** — 利用がなく、ドキュメントが自分で
  降格させており、保守面積と債務だけが残っていた。**撤去は保守であって失敗
  ではない。** 利用者のナレッジは一件も消さない。
- [0018 import-ossie の廃止](0018-semantic-model-as-knowledge.md) — **Superseded by 0070**。
- [0028 compile_sql とセマンティックモデル面の撤去](0028-retire-compile-sql.md) — **Superseded by 0070**。

## MCP OAuth コネクタ(撤去済み)

- [0116 コネクタの値段は変わり、その条件は変わらない](0116-the-connector-price-changed-not-its-condition.md)
  — **Accepted**。[0070](0070-what-was-retired-and-why.md) §5 の「戻すときは
  revert が出発点になる」を改訂する。0012 が撤去した三週間後に
  [0086](0086-a-second-way-to-say-who-is-calling.md) が着地しており、
  **ochakai が認可サーバである必要はなくなっていた** — 認可サーバは運用者が
  既に動かしている発行者で、デプロイが公開するのは RFC 9728 のメタデータと
  401 のチャレンジ二つだけ、しかもそこに書く二つの事実は
  `OCHAKAI_OIDC_AUDIENCE` と `OCHAKAI_OIDC_ISSUER` がそのまま入る。実測で
  成立した(vendor がホストするクライアントが別ホストの認可サーバを自分で
  見つけ、自分を登録し、audience に束縛されたトークンで戻り、人として記録
  された)。**863 行が 180 行になり、環境変数も姿勢も増えない。**
  それでも**採用しない**: 0070 §1 の四つの観測のうち三つは発生しないが、
  残る一つ「利用がない」は動いておらず、それが 0012 の撤去理由そのもので
  ある。**安くなったのは実装であって需要ではない。** 値段は消えたのでは
  なく運用者の IdP に移っており(登録の許可、`scope: openid`、access token の
  email)、採用する日の記録が読者に負っているのはその三つである。戻す条件は
  0070 §5 のまま。
- [0010 MCP OAuth コネクタサービス](0010-mcp-oauth-connector.md) —
  **Superseded by 0012**。claude.ai / ChatGPT リモートコネクタ向けの
  公開第二サービス。**再実装時の出発点はここではなくなった**(0116)。
- [0012 MCP OAuth コネクタサービスの撤去](0012-retire-mcp-oauth-connector.md) — **Superseded by 0070**。
- [0039 stdio クライアントへの橋](0039-mcp-stdio-bridge.md) — **Superseded by 0067**。
