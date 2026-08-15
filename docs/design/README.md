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
| 全体アーキテクチャ | [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) |
| Google Cloud 前提・secret-zero | [0003](0003-gcp-only.md)。**認証の第二の経路は [0086](0086-a-second-way-to-say-who-is-calling.md)**(OIDC 発行者を名指したデプロイは自分で検証する。secret は増えない) |
| 認証と identity | [0065](0065-identity-and-provenance.md) |
| デプロイの姿勢(read-only / public / dev / sandbox) | [0066](0066-four-postures-one-word.md)。**五つ目の `sandbox` は [0087](0087-a-sandbox-says-it-is-one.md)**(匿名で、書けて、消える — そしてそう言う) |
| OKF 互換・バンドル・保存形 | [0075](0075-the-bundle-is-the-address-space.md)(バンドル・住所・保存形)、[0074](0074-the-document-and-the-vocabulary-that-asks-it.md)(文書の形と問いの語彙)。**取り込みが文書を拒む条件と CLI が送るバイト列は [0079](0079-taking-the-document.md) が現行**。期限と引用元は [0069](0069-the-loop-and-what-measures-it.md) §2 |
| 住所とパス | [0075](0075-the-bundle-is-the-address-space.md) が現行(パスが住所、型は属性、move、prefix)。`.md` が必須でバンドルパスの一部であることは [0064](0064-rest-stops-at-api-v1.md) §5 |
| 型の語彙 | [0071](0071-the-recommended-type-vocabulary.md)。型に `/` を許すのは [0064](0064-rest-stops-at-api-v1.md) §18(0071 §1 の「`/` 不可」を撤回) |
| 知識の単位の呼び名 | [0057](0057-concept-is-the-word-a-reader-meets.md)(ツール名・読む語)、[0064](0064-rest-stops-at-api-v1.md) §7 が現行(JSON フィールド名 `entries` → `concepts`) |
| ファイル | [0075](0075-the-bundle-is-the-address-space.md)(バンドルのオブジェクトと帰属)、[0080](0080-search-and-how-a-deployment-embeds.md)(検索)。ベクトルの鍵がパスであることは [0091](0091-a-file-vector-is-keyed-by-its-path.md) |
| 検索と埋め込み | [0080](0080-search-and-how-a-deployment-embeds.md) が現行 — 何を融合するかと、`OCHAKAI_EMBEDDINGS` 一語でどう埋め込むかを一冊で持つ。**埋め込みはデプロイのリージョンで行う**(§1.2、データ所在地)。住所で絞る `prefix` は [0075](0075-the-bundle-is-the-address-space.md) §6、スコアの床を持たないことは [0068](0068-how-a-face-is-added-and-removed.md) §3、`context` の `hits` は [0067](0067-four-faces-and-what-they-decline.md) §4([0093](0093-the-budget-governs-the-whole-response.md) が予算の側を改訂)。ヒットが運ぶ一致箇所は [0084](0084-a-hit-says-why-it-matched.md)。**入力窓に収まらなかった concept を数えることとチャンク化を断ることは [0089](0089-a-half-embedded-concept-says-so.md)**(0080 §3・§7 を改訂)。**ファイルのベクトルをパスで引き、帰属を検索時に読むことは [0091](0091-a-file-vector-is-keyed-by-its-path.md)**(0080 §5 を改訂) |
| サーフェスの配分 | [0067](0067-four-faces-and-what-they-decline.md)(各面の役割と、載せないもの)、[0068](0068-how-a-face-is-added-and-removed.md)(足す規則と降ろす規則)。MCP のツール 7 本とその境界は [0076](0076-two-tools-leave-mcp.md) と、**7 本目を割った [0096](0096-a-listing-is-not-a-search-here-either.md)**。CLI がファイルを名指す綴りは [0077](0077-one-address-for-a-file.md)。**`context` の予算が応答全体を縛ることは [0093](0093-the-budget-governs-the-whole-response.md)**(0067 §4 を改訂) |
| Web UI | [0072](0072-the-web-ui-serves-and-edits-documents.md) が現行(配信と編集)。プロキシと identity は [0065](0065-identity-and-provenance.md) §5。**ページが ES モジュール一式になり、テストが実行されるものになることは [0092](0092-the-page-is-modules-so-it-can-be-tested.md)**(0072 §1 を改訂)。**CSP の下で配信され、他人のフレームに入らないことは [0094](0094-the-page-runs-under-a-policy.md)** |
| 検証ループと利用測定 | [0069](0069-the-loop-and-what-measures-it.md) が現行。裁定の面と一覧のページングは [0068](0068-how-a-face-is-added-and-removed.md)。**二つのフィードが直近 90 日で並ぶことは [0090](0090-a-queue-ranks-on-what-happened-lately.md)**。**人間側のフローに時系列が加わることは [0095](0095-the-loop-has-a-shape.md)** |
| 同時実行と削除 | [0030](0030-optimistic-locking.md)、[0031](0031-purge.md)。**purge とファイル削除が参照されなくなったバイト列を回収することは [0099](0099-a-purge-reaches-the-bytes.md)**(0031 §3.2 を改訂) |
| 実装の品質ゲート | [0035](0035-verifiability.md) |
| 決定の書き方 | [0048](0048-decision-records-for-wire-contracts.md) |
| バンドル往復と provenance の所有権 | [0075](0075-the-bundle-is-the-address-space.md) §3.1 が現行(主張と観測の分離)。往復で何が動かないか・Git をレビュー経路にする決定・二つの拒否は [0009](0009-provenance-portability.md) |
| 空のベースを埋める | [0085](0085-the-empty-base-and-what-fills-it.md) — `ochakai seed` が運用者自身の撃った `INFORMATION_SCHEMA` の答えを `BigQuery Table` の draft バンドルにし、書き込みは既存の `import` が行う。**ウェアハウスには接続しない**ので [0081](0081-what-ochakai-is-and-what-it-refuses-to-hold.md) §1 のコネクタ取り込みの拒否は不変 |
| やらないと決めたこと | [0070](0070-what-was-retired-and-why.md) |
| REST の安定性契約 | [0064](0064-rest-stops-at-api-v1.md)、[docs/compatibility.md](../compatibility.md)。**凍結が止めているものの範囲は [0082](0082-what-the-freeze-holds-still.md) が現行**(応答専用スキーマへの追加は対象外)。エラー応答が運ぶ `code` は [0083](0083-an-error-carries-a-code.md) |
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

- [0024 リンクは本文から導出する](0024-links-from-body.md) — **Superseded by 0074**。

## 添付ファイル(0046 でバンドルのオブジェクトになった)

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

- [0072 Web UI — 二つの配信経路と、文書としての編集](0072-the-web-ui-serves-and-edits-documents.md)
  — **Accepted**。**Web UI の現行ドキュメント**(0006 / 0044 を一冊に
  まとめたもの)。ページは認証を知らず、同じ一枚を `ochakai ui` と
  `ochakai serve-ui` の二経路で配る — 危険な組み合わせはコマンドを分けること
  で書けなくなる。編集は正準 OKF 文書のテキスト編集一本で、行エディタを
  失うことは値引きではなく支払いである。
- [0092 ページはモジュールになる — テストできるように](0092-the-page-is-modules-so-it-can-be-tested.md)
  — **Accepted**。0072 §1 を改訂する。「自己完結の一枚」はビルドステップを
  持たないことの言い換えだった — **そこは変わらない**(ビルドなし・
  フレームワークなし・CDN なし・ページは認証を知らない、の四つはそのまま)。
  変わったのは 2,438 行が**一つの名前空間**に載っていたことで、境界が無い
  ところにはテストが書けない。分割して最初に分かったのは、**四つの関数が
  宣言されたきり一度も呼ばれていなかった**ことである。切り方は面ではなく
  依存の向きで決めた — 五つのモジュールは document に触らないので Node が
  import でき、それが実行されるテストの前提になる。**バンドラも
  フレームワークも npm 依存も入れない**: テストランナは Node に付いてくる
  ものを使い、ブラウザ側も CDP を標準 `WebSocket` で叩く一ファイルにした。
  一枚が二十個になったので静的資産は**一式で一つの ETag** と `no-cache` を
  持つ(`max-age` は先に進んだ API に古いモジュールを出す)。テストは
  「HTML 文字列の grep が 13 本」から三層になり、Go に残るのは Go と JS に
  またがる不変とページ全体についての主張だけ。CSP・a11y・日本語切り替えは
  **可能になっただけで、決めていない**。
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
- [0068 面はどう足され、どう降ろされるか](0068-how-a-face-is-added-and-removed.md)
  — **Accepted**。**面を足す規則と降ろす規則の現行ドキュメント**(0050 /
  0056 / 0058 / 0062 の四冊を一冊にまとめたもので、決定は一つも動いて
  いない)。能力とコマンドは一対一(一つなら畳み、二つなら割る)、一覧は
  検索ではない(cursor は一覧だけ、順位は `limit` が契約)、通行量の無い
  入口は降ろす(`min_score` の廃止、`fm.` を MCP から)、裁定は
  `POST /api/v1/review/{id}` 一本から下し `withdraw` 一語で言う。
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
- [0093 予算は応答全体を縛り、一位は名前だけでは返らない](0093-the-budget-governs-the-whole-response.md)
  — **Accepted**。0067 §4 の「`hits` はバイト予算の外」を撤回する。理由
  (本文を持たず件数にも上限がある)はどちらも本当で、それでも足りない —
  `hits` は最大 40 行・約 5 KB あり、**`budget=12000` と言った呼び出し側が
  受け取るのは 12,000 + 自分では計算できない量**で、窓を見積もる数として
  機能しない。同じ議論は `outline` を内側に入れたときに一度通っている。
  予算は応答全体を縛り、**買うのは丸ごとの concept**、`hits` と `outline` は
  **床**として常に返る(在ったと知っている呼び出し側しか予算を上げられ
  ない)。順位は削らない。床が予算を超えるときは concept をゼロ個配る —
  減算をそのまま渡すと「最も小さい予算が最も大きい応答を返す」になる。
  既定の `limit` は 5 なので、実際の変化は 12,000 の約 10% である。
  もう半分は**一位が予算に収まらなかったときの `excerpt`** で、
  [0033](0033-context-hits-are-a-ranking.md) の「半分の SQL は実行できる
  ように見える」に**反論せず満たす**: 切るのは文書ではなく本文(frontmatter は
  行が既に運んでいる)、最初のコードフェンスの手前で止まり、段落境界を
  優先し、`bytes` と `id` が残りの量と在処を言う。一位だけ、予算の四分の一
  まで — 抜粋が下の全部を飢えさせるのは greedy packing が防いでいる形
  そのものだからである。近似重複の除去はしない(§4)。

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
- [0100 `.md` は concept の綴りである](0100-md-is-how-a-concept-is-spelled.md)
  — **Proposed**。受理されるまで何も動かない提案である。`.md` の住所に
  座れるのは concept と予約名だけにし、`type` を持たない `.md` の PUT を
  400 にする。動機は適合である — 今日の書き込み面は `type` の無い `.md`
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
  返るか』ではない」がそのまま出る。表面の数は一つも動かない。
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

- [0010 MCP OAuth コネクタサービス](0010-mcp-oauth-connector.md) —
  **Superseded by 0012**。claude.ai / ChatGPT リモートコネクタ向けの
  公開第二サービス。再実装時はこの設計が出発点。
- [0012 MCP OAuth コネクタサービスの撤去](0012-retire-mcp-oauth-connector.md) — **Superseded by 0070**。
- [0039 stdio クライアントへの橋](0039-mcp-stdio-bridge.md) — **Superseded by 0067**。
