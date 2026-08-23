# デプロイの運用

バックアップとリストア、既定を超えるハードニング、チーム向け web UI、
モニタリング、キャパシティ、サプライチェーン、アップグレード — Cloud
Run デプロイガイドの
[§1–§5](../../deploy/cloudrun/README.md)がデプロイを組み立て、Google
側のトラブルシューティングを扱う。このページが扱うのは、その後動かし
続けることである。

ここで測っていない数字は、そう書く。それは意図的である — ochakai は
小さなプロジェクトであり、でっち上げたキャパシティの数字は、正直に
「分からない」と言うより値打ちが低い。

<a id="backup-and-restore"></a>

## バックアップとリストア

**バックアップは二種類あり、それぞれ違うものを復旧する。** どちらかが
もう一方を包含するわけではない、というのが持ち帰るべき唯一の事実である。

| | Cloud SQL バックアップ | `ochakai export` |
|---|---|---|
| ナレッジ | yes | yes |
| provenance — 誰が書き、誰が検証し、いつか | yes | **no** — 文書には書かれるが、取り込みが読み戻さない |
| リビジョン履歴 | yes | **no** |
| 利用回数と結果報告 | yes | **no** |
| 添付ファイルのバイト列 | no — GCS にある | yes、ファイルとして |
| ochakai なしで読めるか | no | yes: markdown と YAML |
| リストア先 | 同じデータベース | 任意のインスタンス、任意のバージョン、任意の OKF consumer |

Cloud SQL バックアップは運用上のもの: デプロイをそのときの状態に復旧
する。OKF export は可搬性のためのもの: ochakai より長生きするコピーで
あり、README が「ナレッジが人質になることはない」と約束できる理由で
ある。

### Cloud SQL バックアップ

example インスタンスは `--no-backup` を付けて出荷されており、月 ~$10
に収まっている。だからバックアップを有効にするのは既定ではなく意図的な
一歩である — コマンドは下の[ハードニング](#hardening)を見よ。

リストアは Cloud SQL 自身の手順であり、データベースだけを復旧する。
**ファイルのバイト列はそこに含まれない**: それらは content-addressed な
オブジェクトとして GCS に住んでいるので、データベースのバックアップから
リストアしたインスタンスは、バイト列が今日のバケットの中身のままである
ファイルのメタデータを見つけることになる。それが重要なら object
versioning か retention policy を有効にせよ — そして `purge` は意図的に
GCS の blob を回収しない(設計ドキュメント 0031)ので、バケットは増える
一方であることに注意せよ。

### エクスポートする

```sh
ochakai export ./knowledge          # ディレクトリへ
ochakai export - > okf.tar.gz       # あるいはストリームへ
ochakai export --no-files -   # concept のみ; バイト列はすでに GCS にある
ochakai export --prefix teams/growth -   # 一つのディレクトリだけ(バックアップではない)
```

**`--prefix` はバックアップではない。** 一つのディレクトリを持ち出すため
のもので、そのディレクトリを読める人なら誰でも取れる(設計ドキュメント
[0127](../design/0127-an-archive-says-which-part-it-is.md))。取れたものは
自分が部分であることを二箇所で名乗る — 根の `index.md` の
`bundle_scope` と、`ochakai-okf-teams-growth.tar.gz` というファイル名で
ある。**ベース全体のバックアップは `--prefix` を付けない形のほうで、
そちらは管理者だけが取れる。**

バンドルは OKF v0.2 のディレクトリである: concept ごとに YAML
frontmatter 付きの markdown ファイルが一つ、ファイルはその concept の
隣に、そしてディレクトリごとに `index.md` と `log.md` が一つずつ。
10 個の concept を持つデモベースを export すると 30 ファイルになる —
concept 10 個と、10 ディレクトリ分の index と log である。

スケジュール実行するなら、これが人間に読め、diff でき、commit できる
バックアップである。インスタンス間の移行経路でもあり、ochakai を聞いた
ことも無いプロジェクトへナレッジを移す唯一の方法でもある。

### バンドルからのリストア

```sh
ochakai import ./knowledge
```

import は普通のエンドポイントに対するクライアント側のループである —
サーバー側の一括 import は決定として存在しない(設計ドキュメント
0067 §5.2) — なのでリストアには CLI か、そのループを再現するクライアント
が要る。それを見込んでおくこと: Web コンソールを前提にした
disaster-recovery の runbook はここでは動かない。

**バンドルのリストアが持ち帰らないもの。** 10 個の concept を持つ
ベースを export し、空のインスタンスへ import して測定した:

- **タイムスタンプは復元した瞬間になる。** リストアされた concept の
  `created_at` と `updated_at` は import した瞬間であり、実際に作業した
  瞬間ではない。
- **履歴は潰れる。** 2 回のリビジョンを持っていた concept は 1 回で
  戻ってくる。リビジョンはそのインスタンスが何をしたかの監査証跡で
  あり、バンドルはその証跡ではない。
- **provenance は import した者として再記録される。** 別の誰かが書き
  検証したと*主張する*バンドルは、そのまま信じられることはない: export
  した frontmatter を編集して `generated.by: human:tanaka@…` と
  `verified.by: human:sato@…` にしてから import すると、本文の変更は
  保存されたが、両方の行為者は import した identity のまま記録された。
  provenance はインスタンスが観測したものであって、文書が主張するもの
  では決してない(設計ドキュメント 0009)。
- **検証そのものが持ち帰らない。** 文書が持つ trust family は、それを
  書いたインスタンスの観測であってこのインスタンスのものではないので、
  台帳にも trust tier にも入らず、`received:` の下に主張として保存される
  (設計ドキュメント 0075 §3.1)。空のインスタンスへ import された
  concept は status を保ったまま `unverified` として着地し、取り込みは
  それを concept ごとの note で報告する。**再検証はリストア後の仕事で
  ある。**
- **利用回数と結果報告はまったく持ち越されない。**

持ち帰る*のは*知識そのもの — 本文、type、status、envelope のフィールド、
そして他のインスタンスが何を主張していたかの記録 — である。

**読める記録としては残る。** export された .md は書いた側の `generated` /
`verified` / `created_by` を frontmatter に持ったままなので、「誰がいつ
確かめたか」はバンドルを開けば読める — リストアで復旧しないだけである。
アーカイブとして要るのは export した木そのものであって、それを Git に
置くかどうかは別の判断である([Git をレビュー経路にする](git-review.md))。

つまり: **すべての concept 上で見えても構わない identity で import を
実行せよ**。provenance が重要なら、データベースバックアップも合わせて
持て。どちらもバグではない — バンドルが運ぶのはナレッジであり、
provenance はそれが起きるのを見ていたインスタンスに属する — が、export
からの完全な復元を期待する runbook は、最悪の瞬間に間違っていることに
なる。

## モニタリング

### `/health` は liveness チェックであり、それ以上のものではない

`GET /health` は無条件に `ok` を返す
([cmd/ochakai/main.go](../../cmd/ochakai/main.go))。データベースには
触れない。uptime チェックがこれに答えるのは「プロセスは応答しているか」
であって「デプロイはちゃんと動いているか」ではない — データベースが
落ちたサーバーも、行が要るリクエストが来るまでは `ok` と答え続ける。

`/healthz` ではなく `/health` なのは、Google のフロントエンドが
`run.app` の URL 上で `/healthz` を横取りし、自前の 404 を返すからで
ある。

もっと厳格なチェックが欲しければ、store に触れる read なら何でもよい:
`GET /api/v1/search?sort=verified_at&limit=1` は、データベースが答える
ときにだけ成功する。`ochakai whoami` はシェルから同じことをしつつ、
サーバーがどの identity を見ているかも表示する。

### リクエストログ

**リクエストは一本ずつ一行になる。** `/api/v1` と `/mcp` のすべての
リクエストが、応答が終わった時点で `request` という一行を出す:

```
"request" method=GET route="GET /api/v1/search" status=200 duration_ms=34 bytes=1820 actor=process
"request" method=PUT route="GET /api/v1/bundle/{path...}" status=409 duration_ms=7 bytes=96 actor=human code=already_exists
```

- **`route` はパスではなく、mux が一致させたパターンである。**
  バンドルのパスは concept の住所、つまり利用者のナレッジそのものなので、
  ログに出せばナレッジベースの形を一行ずつ第二の場所に写すことになる。
  出るのは「何が呼ばれたか」であって「何に対して呼ばれたか」ではない。
- **`actor` は kind だけである**(`human` / `process`)。誰が書いたかは
  provenance であり concept の上に住む(設計ドキュメント
  [0065](../design/0065-identity-and-provenance.md))。ログはその第二の
  コピーではない。運用者が率として読むのは人か処理かの別である。
- **`code`** は失敗のときだけ付く。ステータスが言えない半分がこれで、
  409 は三つの条件が共有している(設計ドキュメント
  [0083](../design/0083-an-error-carries-a-code.md))。

**これはテレメトリではない。** 送信先はこのデプロイの stderr で、
Cloud Run ではこのプロジェクトの Cloud Logging である(stdout ではない
— そちらは `ochakai mcp-stdio` のワイヤであり、クライアントコマンドの
出力である)。そこから先は
ログベースの指標にできる — ochakai が `/metrics` を生やさず、二つ目の
アドレス空間もスクレイプ対象も持たずに済むのはそのためである
(すべては `/api/v1` に載るか、どこにも載らない、設計ドキュメント
[0067](../design/0067-four-faces-and-what-they-decline.md) §1)。組む
価値があるのは次の三つである。

| 指標 | クエリの骨子 |
|---|---|
| レイテンシ | `jsonPayload.message="request"` の `duration_ms` を分布で。`route` で分けると、遅いのが検索なのか export なのかが出る |
| エラー率 | 同じ行の `status>=500`。`4xx` は呼び出し元の問題なので、率で見るなら `code` で分ける |
| 劣化 | `query embedding failed` の件数(下の表)。検索が lexical に落ちている時間である |

### ログ

すべては `slog` を通した JSON なので、Cloud Logging は手を加えずに
パースできる。**レベルは `severity` として、本文は `message` として
届く** — Cloud Logging が読む綴りであり、`severity>=WARNING` で下の表を
まとめて引けるのはそのためである(slog 自身の綴りは `level` と `msg`
で、そのままでは全行が同じ重大度で並ぶ)。アラートや保存クエリを組む
価値があるメッセージは次の表の通りである。ほとんどは**劣化しているが動いている**ことを表す
warning だが、レベルで絞り込むとこの表を素通りする行が三つ混ざって
いる: `Error` が一つ(export の失敗)、エラーではないキュレーション
行為の監査ログが二つ(検証・破棄)である。

| メッセージ | 意味 |
|---|---|
| `usage flush failed` | usage イベントのバッチが失われた。設計として best-effort であり(0069 §3)、これが*連続*するならデータベースの調子が悪いということである |
| `usage recording failed` | メモリ上のバッファが満杯 — 20,000 イベント — になり、イベントが捨てられた |
| `search miss recording failed` | 同じバッファ、同じ取引(0069 §4): 答えの無かった問いが記録されなかった。ナレッジには影響しない; `ochakai stats` が過小に数える |
| `query embedding failed; falling back to lexical-only` | Vertex AI が検索に答えなかった; 結果は lexical 側だけでランクされて返る |
| `document embedding failed; concept remains findable by the lexical half of search` / `storing embedding failed` | concept は書き込まれたがベクトル索引には無い。`ochakai reembed` が直す |
| `attachment embedding failed; attachment remains findable by name` | 同じことがファイルについて起きた。メッセージは今も *attachment* と言う: 0064 が変えたのはワイヤ上の語であって、ログの語ではない |
| `backlink lookup failed` | ある concept の backlink がレスポンスから欠けている |
| `export truncated after the response began` | export が途中で失敗した。**クライアントが受け取ったバイト列は完全なバックアップではない** — このリストの中で静かに損害を与えうるのはこれだけである |
| `knowledge verified` / `knowledge purged` | エラーではない: 行為者付きの、キュレーション行為の監査ログである |

起動時にサーバーは自分の姿勢を述べる。動いているインスタンスが実際に
何を有効にしているかを確認する、いちばん速い方法である:

```
"ochakai listening" addr=:8080 version=… insecure_dev=false endpoints=[/mcp /api/v1 /health]
"semantic search enabled" model=gemini-embedding-001 dim=768 project=… location=asia-northeast1 discovered=true
"files disabled (no OCHAKAI_GCS_BUCKET); markdown concepts only"
```

`discovered=true` は、model・location・project のどれもデプロイが書いた
ものではないことを意味する — **project と location はメタデータサーバーから
来ており**、model だけが製品の既定である(`OCHAKAI_EMBEDDINGS` にモデルの
resource name を書けば `discovered=false` になる、設計ドキュメント
[0080](../design/0080-search-and-how-a-deployment-embeds.md))。semantic
search は Google Cloud 上での既定だからである(同 §1)。

**`location=` は監査で訊かれる行である。** 埋め込みは動いているリージョン
で行われるので、この値は「concept の本文と検索クエリがどこの Vertex AI に
送られたか」そのものである(設計ドキュメント
[0080](../design/0080-search-and-how-a-deployment-embeds.md) §1.2)。
asia-northeast1 のデプロイなら `location=asia-northeast1` が出る。
`gemini-embedding-2` を名指したデプロイだけは別で、そのモデルは
`global`/`us`/`eu` にしか居ないため、**名指した時点で埋め込み呼び出しが
自リージョンの外に出る** — 画像・PDF 検索と引き換えに払うものがそれである。

これに代わる行はどちらに転んだかを言う:

| 行 | 意味 |
|---|---|
| `semantic search off: Vertex AI did not answer for this deployment; …` | 起動時の probe が答えを得られなかった。理由は二つあり、`location=` が見分ける — ロールが無い(`roles/aiplatform.user` を付与して再起動)か、**そのリージョンにモデルが居ない**(`gemini-embedding-001` は大半のリージョンに居るが全部ではない。別リージョンを名指せば直るが、それはテキストがそこへ行くということである) |
| `semantic search off: this deployment's region could not be read from the metadata server, …` | project は読めたが region が読めなかった。ochakai は誰も選んでいないリージョンでは埋め込まないので、字句検索で動いている。resource name でリージョンを名指せば有効になる |
| `semantic search is off: this database cannot hold vectors` | pgvector が無く、このロールには作成できないかもしれない。admin ユーザーとして extension を作成せよ(デプロイガイド §3) |
| `semantic search off by configuration (OCHAKAI_EMBEDDINGS=off); using lexical search only` | 頼んだ通り — ここにある行の中で、調べる価値の無い唯一のものである |
| `semantic search disabled; using lexical search only` | モデルが名指されておらず、project も発見されなかった — ochakai は Google Cloud 上で動いていない |

`insecure_dev=true` がラップトップ以外のどこかに出ていたら、そこで
止めて直せ: すべての呼び出し元が `human:anonymous` になり、何も認証
されていない。

### アラートを張る価値があるもの

ここに規範的なものは無い — これは決済システムではなくナレッジベース
である — が、実際に損害になる順に並べると:

1. **Cloud Run の 5xx 率**。壊れたデプロイがまず最初に現れる場所で
   ある。
2. **`export truncated`**。切り詰められたバックアップはバックアップの
   ように見えるからである。
3. **Cloud SQL のディスク使用量**。example インスタンスは
   `--no-storage-auto-increase` 付きの 10 GB だからである: 育つのでは
   なく止まる。
4. **`usage flush failed` の持続的な連続**。統計そのものというより、
   データベースの健康シグナルとして。

### レビューすべきものがあると知る

上のリストはデプロイについてである。もう一つ見る価値があるのはナレッジ
の方であり、それは静かに失敗する: 誰も開かないレビューキューは、何も
入っていないレビューキューとまったく同じに見える。

`ochakai stats` がその答えのすべてである(設計ドキュメント
[0069](../design/0069-the-loop-and-what-measures-it.md) §5)。その中の行のうち、キュレー
ターが空にする三つのキューは、それぞれ一覧表示するコマンドを添えて
出てくる:

```console
$ ochakai stats
…
drafts	12	ochakai list usage --status draft
failed	1	ochakai list failed
stale_after	0	ochakai list stale_after
…
```

それぞれのキューは、それを一覧表示する `sort` にちなんで名付けられて
いるので、数とそれを見る方法は同じ一語である(設計ドキュメント
[0069](../design/0069-the-loop-and-what-measures-it.md) §5.2)。`drafts`
は例外で、単一の sort がそれを一覧表示することは無いからである。

`--exit-code` はそれをスケジューラーが見張れるものに変える:
**三つのうちどれかが空でない間は 2、すべて空なら 0** — そして 1 は
これまで通りエラーのままなので、到達できないサーバーが「やることが
無い」と読み違えられることは無い。`--prefix teams/growth` はそれを
絞る、共有デプロイ上の一つのチームが自分たちのキューについて尋ねる方法
である。

これで、チームがすでに赤いビルドを見張っているどんなチャンネルでも、
週次の通知には十分である — 宛先リストも要らず、秘密も要らず、新しく
動かすものも無い:

```yaml
name: ochakai-review-queue
on:
  schedule:
    - cron: "0 0 * * 1" # 月曜、09:00 JST
jobs:
  queues:
    runs-on: ubuntu-latest
    permissions:
      id-token: write # Workload Identity Federation(鍵レス)
    steps:
      - uses: google-github-actions/auth@v3
        with:
          workload_identity_provider: ${{ vars.WIF_PROVIDER }}
          service_account: ${{ vars.REVIEWER_SA }}
      - run: |
          TOKEN=$(gcloud auth print-identity-token --audiences="$OCHAKAI_URL")
          waiting=$(curl -sf -H "Authorization: Bearer $TOKEN" \
            "$OCHAKAI_URL/api/v1/stats" | jq '.queues | add')
          echo "review queue: $waiting waiting"
          [ "$waiting" -eq 0 ] || { echo "::error::$waiting waiting for review"; exit 1; }
```

ochakai 自身は何も配送しない — メールも chat も webhook も無い(設計
ドキュメント 0069 §6)。上のジョブが配送であり、それはあなたのもので
ある。

同じコマンドは、もう一歩先の問いにも答える — 「何か待っているか」では
なく「これはうまくいっているか」: ベースのどれだけが確認済みか、今月
どれだけがレビューを通ったか、そして人々が何を検索して見つけられなかった
か(設計ドキュメント
[0069](../design/0069-the-loop-and-what-measures-it.md) §5)。1 行に
1 数字なので、キューを見張るのと同じ cron が `>>` と日付でその履歴も
残せる:

```console
$ ochakai stats --days 7
concepts	128
draft	31
stable	92
…
misses	11
gap	5	解約率の定義
gap	3	arr by segment
```

`gap` の行は空振りに終わった問いであり、最も多く聞かれた順である —
次に何を書くべきかを言う唯一のリストである。何も記録しないデプロイは
`misses 0` ではなく `misses -` と出す。両者は同じ答えではないからで
ある(`OCHAKAI_RECORD_MISSES`、そして公開デプロイは一件も記録しない)。

<a id="capacity"></a>

## キャパシティ

### 分かっていること

- **検索**: 5000 件の concept に対する日本語検索で約 16 ms、ラテン文字
  の語なら 0.2 ms。日本語の語がスキャンで答えられているのは、trigram
  索引が二文字のパターンを引けないからである。
- **ファイル**: 1 ファイル 5 MiB まで(サーバが強制)。件数に上限は
  無い。
- **usage バッファ**: メモリ上に 20,000 イベント、5 秒ごとに flush
  される。上限を超えるとイベントはキューイングされず捨てられる(設計
  ドキュメント 0069 §3)。
- **生の usage イベント**は 180 日後に pruning される; concept ごとの
  合計は pruning されない。
- **シャットダウン**: 処理中のリクエストは SIGTERM の後 5 秒をもらい、
  それから最後の usage flush が走る。Cloud Run は SIGKILL まで 10 秒
  許容する。

### `--max-instances` を上げる

デプロイガイドは `--max-instances=1` を設定しているが、それは正しさで
はなくコストの決定である: サーバーの中に single-instance でなければ
ならないものは何も無い。usage バッファはインスタンスごとであり、
どちらにせよ best-effort である。データベースだけが共有された状態で
ある。

上げるときに見るべき天井は**データベース接続数**である。各インスタンス
は自分のプールを開き、`pgxpool` の既定は 4 接続(あるいは CPU 一つに
つき 1、どちらか大きい方)である。つまり *N* インスタンスはおよそ *4N*
接続になる。`db-f1-micro` は shared-core インスタンスで接続数の上限が
小さいので、複数インスタンスを走らせるつもりなら、収まるかどうか
`SHOW max_connections` で確認し、絞りたいなら `OCHAKAI_DATABASE_URL`
に `pool_max_conns` を設定せよ。

### 測っていないこと

正直に言えば、大半である。measured な concept 数の天井も、スループット
の数字も、並行時のレイテンシ分布も無く、いつ `db-f1-micro` が足りなく
なるかの指針も無い。このプロジェクトは一人のものであり、これらの数字は
観測するのではなくでっち上げることになってしまう。

これが重要になる規模で ochakai を動かしているなら、役に立つのは
[Discussions](https://github.com/na0fu3y/ochakai/discussions) で実際に
見えているものを話すことである。この節に数字が増えるのはそうやって
である。

<a id="supply-chain"></a>

## サプライチェーン

イメージは GitHub Actions によって SBOM と SLSA provenance 付きで
`ghcr.io/na0fu3y/ochakai` に公開される; workflow のアクションはコミット
SHA で固定されている。バイナリは `distroless/static` 上の静的で
trimmed-path な Go ビルドである。デプロイしようとしているものを検証
せよ:

```sh
gh attestation verify oci://ghcr.io/na0fu3y/ochakai:<tag> -R na0fu3y/ochakai
```

CLI のリリースアーカイブも同じ attestation に加えて `checksums.txt` と、
アーカイブごとの SPDX SBOM(`*.sbom.json`、リリースの隣に添付)を運ぶ。
Binary Authorization はデプロイ時にこのチェックを強制できる —
下の[ハードニング](#hardening)を見よ。

<a id="hardening"></a>

## ハードニング

既定のデプロイガイドの手順
([§1–§5](../../deploy/cloudrun/README.md))は、すでに secret-zero かつ
最小権限である — Cloud Run IAM、Google の identity、明示的な付与のみ、
空の authorized-networks 一覧、provenance が証明されたイメージ。下の
手順はさらにバーを上げる; 自分たちのリスクプロファイルに合うものを
選べ。

### Private IP のみにする

本番相当のデプロイでは、Cloud SQL のパブリック IP を完全に外せ。
追加コストは無い(VPC、private services access、Cloud Run の Direct
VPC egress はすべて無料である); トレードオフは、ローカルの admin
アクセス(デプロイガイド §3 の SQL、この節の下にあるパスワード退役の
作業 — §5 の import は影響を受けない、API 経由だからである)に一時的な
public IP か VPC に接続したワークステーションが要ることである。

VPC ごとに一度だけ — peering range を確保して接続する(Compute API を
今有効にしたばかりなら、`addresses create` が `SERVICE_DISABLED` を
返さなくなるまで 1 分ほど要ることがある):

```sh
gcloud services enable servicenetworking.googleapis.com compute.googleapis.com
gcloud compute addresses create google-managed-services-default \
  --global --purpose=VPC_PEERING --prefix-length=16 --network=default
gcloud services vpc-peerings connect --service=servicenetworking.googleapis.com \
  --ranges=google-managed-services-default --network=default
```

それからインスタンスを `--network=default --no-assign-ip` で作成する
(デプロイガイド §2 の既定の代わりに) — あるいは既存のものを変換する。
これは同じパッチで、15–20 分かかる:

```sh
gcloud sql instances patch ochakai --network=default --no-assign-ip
```

Cloud Run サービスに Direct VPC egress を追加し、private IP に届く
ようにする(`/cloudsql` コネクタソケットは自動で追随する;
`OCHAKAI_DATABASE_URL` は変わらない):

```sh
gcloud run services update ochakai --region=$REGION \
  --network=default --subnet=default
```

ローカルの admin アクセスはもう VPC の外からは届かない。一時的に
public IP を付け、作業後に外す:

```sh
gcloud sql instances patch ochakai --assign-ip       # + cloud-sql-proxy、作業する
gcloud sql instances patch ochakai --no-assign-ip    # 終わったら
```

(下の `sql.restrictPublicIp` org policy が強制されている場合、一時的な
`--assign-ip` もブロックされる — その間だけポリシーを外すか、VPC に
接続したワークステーションを使え。)

<a id="guardrails-against-misconfiguration"></a>

### 設定ミスに対するガードレール

org policy が要る; Org Policy Administrator ロールが要る。これらは
危険な状態を「避けられる」のではなく「表現できない」ものにする:

```sh
# Cloud SQL への authorized network の追加を一切禁止する(広い範囲だけ
# でなく — 空の一覧という姿勢そのものを、離脱できないものにする)
gcloud resource-manager org-policies enable-enforce \
  sql.restrictAuthorizedNetworks --project=$PROJECT_ID
# Cloud SQL のパブリック IP を禁止する — 上の private IP only と組み合わ
# せる。既存のパブリック IP は既得権として残る: インスタンスは動き続け、
# 無関係なパッチも動き続ける; IP 設定への変更だけがブロックされる
gcloud resource-manager org-policies enable-enforce \
  sql.restrictPublicIp --project=$PROJECT_ID
```

強制が反映されるまでは 1、2 分かかる — その直後に出した違反パッチは
それでも成功しうる。ガードレールが効いているかどうかは、一つ試して
確かめよ: `gcloud sql instances patch ochakai
--authorized-networks=203.0.113.1/32` は `Organization Policy check
failure` で失敗するはずである(成功したなら、早すぎただけである —
`--clear-authorized-networks` してもう一度)。

Domain Restricted Sharing(`iam.allowedPolicyMemberDomains`)は org
レベルでオンにしておけ; 動いているデプロイの中で `allUsers` を必要と
するものは無い — [チーム向け web UI](#the-team-web-ui) を含むすべての
サービスが `--no-allow-unauthenticated` のままである。デプロイガイドの
§5d の公開デモだけがこのポリシーにブロックされるが、それは正しい
トレードオフである: デモが欲しいなら専用のプロジェクトを一つ免除せよ、
org policy 自体はそのままにして。

**直接接続に TLS を強制する**(Cloud SQL コネクタの通信は常に mTLS で
ある; これは authorized-network の経路をカバーする):

```sh
gcloud sql instances patch ochakai --ssl-mode=ENCRYPTED_ONLY
```

**最後のパスワードを退役させる。** admin ユーザーのパスワードはシステム
全体で唯一保存されている secret であり、それも無くせる: メンテナンス用
の個人 IAM ログインを作り、admin ユーザーの持ち物をそちらへ移管し、
パスワードユーザーを削除する。以後のメンテナンスは自分自身の identity
で `cloud-sql-proxy --auto-iam-authn` を通す。

この順序を形作る Postgres の事実が二つある。起動時のマイグレーションが
作るテーブルは**ランタイムのサービスアカウント**が所有するので、admin
はそれらに `GRANT` できない — ロールメンバーシップ経由で継承すること
になる。そして、まだオブジェクトを所有しているか grant に現れている
ロールは drop できないので、admin の足跡は先に再割り当てして drop しな
ければならない — それは同時に**admin がこれまでに与えたすべての grant
を取り消し、下の再付与までサービスを壊す**。これは一つの作業時間の中で
終わらせること:

```sh
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member=user:you@your-org.example --role=roles/cloudsql.instanceUser
gcloud sql users create you@your-org.example --instance=ochakai --type=cloud_iam_user
```

admin ユーザーとして(`cloud-sql-proxy` + `psql`、デプロイガイド §3)、
一つのセッションの中で:

```sql
GRANT "ochakai-run@<PROJECT_ID>.iam" TO "you@your-org.example";  -- ランタイムが所有する、現在と将来のテーブルを継承する
REASSIGN OWNED BY "ochakai" TO "you@your-org.example";
DROP OWNED BY "ochakai";  -- その grant をクリアする; 再付与するまでサービスは劣化する
GRANT USAGE, CREATE ON SCHEMA public
  TO "ochakai-run@<PROJECT_ID>.iam", "you@your-org.example";
```

(スキーマの grant は `cloudsqlsuperuser` のメンバーから出さなければ
ならない — admin は今も要件を満たすが、あなたの IAM ログインは決して
満たさない。)それから自分自身として(`cloud-sql-proxy
--auto-iam-authn`、`you@your-org.example` として接続)、今あなたが所有
しているテーブルに対してランタイムを再付与する:

```sql
GRANT ALL ON ALL TABLES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "ochakai-run@<PROJECT_ID>.iam";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "ochakai-run@<PROJECT_ID>.iam";
```

最後にパスワードユーザーを削除し、コールドスタートを確認する(起動時
マイグレーションにはスキーマの grant が要る — ウォームなインスタンスは
ミスを覆い隠しうる):

```sh
gcloud sql users delete ochakai --instance=ochakai
gcloud run services update ochakai --region=$REGION --update-labels=grants=rotated  # 新しいインスタンスを強制する
# それからデプロイガイド §3 の proxy + curl /health
```

(接続文字列では `@` を `%40` に URL エンコードすること:
`postgres://you%40your-org.example@localhost:55432/ochakai`。)

その後の「secret-zero」が意味すること: 組み込みの `postgres` ユーザー
は今も存在し、Cloud SQL admin の IAM を持つ誰でも、break-glass の
スキーマレベル作業のために使い捨てのパスワードを発行できる
(`gcloud sql users set-password postgres ...`) — *保存された* secret
は残らず、admin アクセスは引き続き IAM で守られる。

**バックアップと point-in-time recovery** — example ではコスト削減の
ために省いているが、本物のナレッジにはそれらがふさわしい:

```sh
gcloud sql instances patch ochakai --backup-start-time=03:00 --enable-point-in-time-recovery
```

(バックアップを有効にするとは開始時刻を選ぶことである — `patch` に
素の `--backup` フラグは無い。両方をまた off にするには、先に PITR を
無効にせよ: `--no-backup` は `--no-enable-point-in-time-recovery` と
組み合わせないと拒否される。)

**デプロイ時のイメージゲーティング**: リリースは SLSA provenance を
運ぶ(上の[サプライチェーン](#supply-chain)); Binary Authorization は
運用担当者の注意力に頼る代わりに、すべてのデプロイでそのチェックを
自動的に強制できる。

**監査証跡**: ナレッジの変更は ochakai 自身によって完全に記録されている
(`knowledge_revision`、変更ごとの actor)。インフラレベルの証跡には、
Console で Cloud SQL の Data Access 監査ログを有効にせよ — admin の
活動は既定で記録されている。

上のすべて — private IP、[チーム向け web UI](#the-team-web-ui) の IAP
コマンド、org-policy のガードレール、TLS の強制、バックアップ、そして
パスワード退役の手順 — は、デプロイガイドの example デプロイに対して
最初から最後まで実際に試されている(2026-07、Postgres 17、gcloud
575)。唯一の例外は Binary Authorization であり、これは今もポインタの
ままである; 書かれた通りに動かないものがあれば報告してほしい。

<a id="access-boundaries"></a>

## ディレクトリごとの閲覧者と編集者

既定では、デプロイに届いた者が全部を読み書きする。境界が要るときの
**最初の**答えはいまもデプロイを分けることで、Cloud Run のサービスと
同じ Cloud SQL インスタンスの中の別データベースで済み、到達と DB の
二層で効く([faq](../faq.md#誰が読み書きできるか))。

分割が効かないのは、**チームどうしが語彙を共有しているとき**である —
全社の用語集をコピーすると取り込みは provenance を読み戻さないので、
コピーは `unverified` で着地する。そのときだけ、同じバンドルの中に
ディレクトリ単位の付与を置く(設計ドキュメント
[0109](../design/0109-a-directory-has-readers-and-writers.md))。

**順序を間違えないこと。** 管理者を先に置く。

```sh
gcloud run services update ochakai --region "$REGION" \
  --update-env-vars OCHAKAI_ADMINS=human:ops@example.co.jp
```

**順序を間違えると、断られる。** 最初の一行を置こうとする呼び出し元が
`OCHAKAI_ADMINS` に居なければ 403 で、**何も書かれない**(設計ドキュメント
[0122](../design/0122-the-first-rule-is-an-administrators-to-write.md))。
最初の一行が入った瞬間からポリシーは管理者のものになるので、その一行を
置けるのも管理者だけである。

ポリシーは JSON 文書一枚で、表示と置き換えだけがある。**git に入れて
レビューする**のが想定した使い方である。

```sh
ochakai access --json > policy.json   # いまの姿(最初は空)
$EDITOR policy.json
ochakai access -f policy.json         # 置き換え、置いた結果が印字される
```

```json
{"rules": [
  {"prefix": "glossary",     "principal": "*",                          "may_write": false},
  {"prefix": "teams/growth", "principal": "human:tanaka@example.co.jp", "may_write": true},
  {"prefix": "personnel",    "principal": "human:hr@example.co.jp",     "may_write": true}
]}
```

読み方は一行ずつで足りる — 「この principal はこのディレクトリ以下を
読める、`may_write` なら書ける、`may_admin` ならそこ以下の規則そのものを
編集できる」。拒否規則は無く、**どの規則も付与しな
かったものは読めない**。照合はセグメント境界なので `sales` は
`sales/orders` に当たり `sales-legacy/orders` には当たらない。
principal は台帳と同じ綴り(`human:` / `process:`、`*` は全認証済み
呼び出し元)で、生のメールアドレスは受け付けない — 綴りが割れると片方
だけが一致するからである。

**最初の一行が、全員に対して同時に境界を入れる。** 段階的に効かせる
仕組みは無いので、置いたら別の identity で `ochakai get` と
`ochakai search` を実行して読み返すこと。全部戻すのは空のポリシーである:

```sh
echo '{"rules": []}' | ochakai access -f -
```

**二人で編集するなら、読んだ版を添えること。** 文書は丸ごと置き換わる
ので、二人が同じポリシーを読んで一行ずつ足すと、後に保存したほうにしか
その行が残らない。エラーは出ない — 気づく契機は、消された側の人が
「読めない」と言ってくることだけである。`--if-match` を付けると、
読んだときの版をポリシーがまだ持っている場合にだけ置き換わり、
そうでなければ**何も書かずに失敗する**(設計ドキュメント
[0120](../design/0120-the-policy-is-replaced-only-as-it-was-read.md))。

```sh
ochakai access --json > policy.json
$EDITOR policy.json
ochakai access -f policy.json --if-match "$(jq -r .version policy.json)"
```

付与を自動で置くもの(組織を受け入れるたびに一行足す類のもの)を書くなら、
これは任意ではない。REST では同じものが `If-Match` ヘッダで、版は `ETag`
と本文の `version` の両方に載る。**空のポリシーにも版がある** — 最初の
一行こそ二つの登録処理が競う書き込みである。

**ディレクトリごと預けるなら、`may_admin` を使う**(設計ドキュメント
[0124](../design/0124-a-directory-can-have-its-own-administrator.md))。
チームや、組織ごとに一行足す自動化に渡すのはこれで、渡した相手は
**その prefix 以下の規則しか読めず、書けない**。他のディレクトリの規則は
その相手の保存を跨いで残るので、`ochakai access --json > f` → 編集 →
`-f f` の手順をそのまま渡してよい。

```json
{"rules": [
  {"prefix": "teams/growth", "principal": "human:lead@example.co.jp", "may_admin": true}
]}
```

**バンドル全体への `may_admin` は置けない。** ポリシー全体を誰が編集
できるかという答えは、そのポリシーの中には置けない — それは
`OCHAKAI_ADMINS` の仕事である。そして**渡した相手より浅い規則は、その
相手には見えないし消せない**: `prefix: ""` に `*` の読み取りを与えて
あれば `teams/growth` にも効き、`teams/growth` の管理者はそれを知らない。
預ける前に、上から効いている行がないか読み返すこと。

**知っておくべきことが三つある。**

1. **バンドル全体を取る操作は管理者のものになる** — `ochakai export`
   (`--prefix` の付かない形)・`ochakai move`・`ochakai reembed`。
   **`--prefix` を付けた export は別で、そのディレクトリを読める人なら
   取れる**(設計ドキュメント
   [0127](../design/0127-an-archive-says-which-part-it-is.md)) — アーカイブ
   が自分の範囲を名乗るので、部分をベース全体のバックアップと取り違え
   ようがない。
   **`ochakai stats` は別で、範囲を持つ利用者も自分の数を読める**
   (設計ドキュメント
   [0123](../design/0123-the-numbers-say-what-they-counted.md))。答えの
   `scope` 行が何を数えたかを言い、全体を数えたときだけ出ない。ただし
   **該当なしの検索(miss)は差し止められる** — 何も返さなかった検索には
   id が無いので絞れず、絞らずに見せれば他のチームが何を探したかを教えて
   しまう。`misses withheld` と出るのはそれで、ゼロではない。
   cron の `stats --exit-code` を回しているなら、その呼び出し元の
   キューだけを見ることになる — インスタンス全体を見張りたいなら、
   その呼び出し元を管理者にする。
2. **スコープ外は 404 で、403 ではない。** 「そこに何かあるか」への
   答えが境界そのものである。書けるが読めない、は起こらない。
3. **本文の散文は塞げない。** 読める concept の本文が隠した id を名指し
   ていれば、その id は本文の中に現れる — サーバーは受け取った文書を
   書き換えないと決めており、そこにバンドルの往復が乗っている。約束して
   いるのは一覧・検索・取得・エクスポート・index から出てこないこと
   である。秘匿する対象を、公開側の concept が引用していないかは
   キュレーションの問題として残る。

管理者を空にしたまま行が残っているデプロイは**起動しない** — 誰も
ポリシーを編集できない状態だからである。その状態から抜けるには
`OCHAKAI_ADMINS` を設定して再デプロイする。

**web UI にも同じものがある。** 管理者として届いた呼び出し元にだけ
「アクセス」タブが出て、上の表を表示し、同じ JSON 文書を置き換える。
ただし `serve-ui`(下の「チーム向け web UI」)は呼び出し元をサービスの
SA 一つに畳むので、**IAP を前置していなければ、その URL に届く全員が
その SA としてポリシーを編集できる**。git に入れてレビューする経路は
CLI のままで、UI は読み返しと、急ぎの一行のためにある。

<a id="the-team-web-ui"></a>

## チーム向け web UI

web UI は `serve` の中ではなく、それ自身のサービスとして動く — core が
自分の serving surface を最小限に保つためである。個人利用ではデプロイ
そのものが要らない: `ochakai ui` が loopback 上で自分自身の identity
のまま同じページを配る。Go の CLI を動かせない人にブラウザアクセスが
必要になったときに、このサービスをデプロイせよ。

これは ochakai 自身と**同じコンテナイメージ**である — デプロイガイド
§1 の `$IMAGE` — 既定の `serve` の代わりに `--args=serve-ui` で起動する:
静的ページと、自分のサービス identity(`X-Serverless-Authorization`)を
API 呼び出しに添えるリバースプロキシであり、ochakai は組織限定のまま
でいられる — 二つ目のイメージをビルドする必要は無く、UI は常に自分が
前面に立つサーバーとまったく同じバージョンである。webui 自体も
非公開である: [Identity-Aware
Proxy](https://cloud.google.com/iap/docs/enabling-cloud-run) が前段に
立ち、ブラウザは Google アカウントでサインインし、組織のメンバーだけが
通れる — `allUsers` の付与はどこにも無い。既定では UI 経由の書き込みは
webui のサービスアカウント(`process:ochakai-webui@…`)として記録される;
ブラウザにいる本人を記録したいなら下の `OCHAKAI_IAP_AUDIENCE` を設定
せよ。MCP と CLI のクライアントは、デプロイガイド §5 の proxy 経路経由で
ユーザーごとの identity を得る。

```sh
# 専用の identity。ochakai を呼び出すことだけを許可する
gcloud iam service-accounts create ochakai-webui
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=serviceAccount:ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --role=roles/run.invoker

# IAP を前段に立てて非公開でデプロイする — デプロイガイド §1 と同じ $IMAGE。
# (下の iap web コマンドには Resource Manager API が要る)
gcloud services enable iap.googleapis.com cloudresourcemanager.googleapis.com
gcloud run deploy ochakai-webui \
  --image=$IMAGE --args=serve-ui \
  --region=$REGION --no-allow-unauthenticated --iap \
  --service-account=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com \
  --min-instances=0 --max-instances=1 --cpu=1 --memory=256Mi \
  --set-env-vars=OCHAKAI_URL=$OCHAKAI_URL

# 組織を IAP 経由で通す(デプロイはすでにサービスに対して IAP の
# サービスエージェントに run.invoker を付与している — 出力の
# "Setting IAP service agent")
gcloud iap web add-iam-policy-binding \
  --resource-type=cloud-run --service=ochakai-webui --region=$REGION \
  --member=domain:your-org.example --role=roles/iap.httpsResourceAccessor
```

webui サービスの URL を開く: IAP が Google のサインインを提示し、それ
からページが読み込まれる。UI と API は proxy を通じて同一オリジンなので、
他に設定することは無い。IAP が本当にサービスの前段にいるか確認するには、
未認証でリクエストしてみよ — 応答はページではなく、
`x-goog-iap-generated-response: true` ヘッダー付きの
`accounts.google.com` への 302 である。

### サービスアカウントではなくブラウザの利用者を記録する

そのままでは UI からの編集はすべて `process:ochakai-webui@…` と読める。
チーム全員に対して著者が一人だけということである。実際に誰が編集したか
を記録するには、どの IAP audience を信頼するかを serve-ui に伝え、
webui を delegator として ochakai に受け入れさせる(設計ドキュメント
0065 §3・§5):

**この順序で設定すること。** serve-ui が identity を転送し始めた瞬間、
ochakai はこの呼び出し元をすでに受け入れていない限り 403 を返す
(静かに格下げすることは無い) — だから委譲を先に付与せよ、そうすれば
UI がその間の窓を見ることは無い。

```sh
# 1. ochakai はこの呼び出し元が転送する identity を受け入れなければならない
gcloud run services update ochakai --region=$REGION \
  --update-env-vars="OCHAKAI_DELEGATING_CALLERS=ochakai-webui@$PROJECT_ID.iam.gserviceaccount.com"

# 2. serve-ui にどの IAP audience を信頼するか伝える。推測はしない:
# 間違った値は何も検証しないのと同じことになるからである。IAP を
# Cloud Run に直接有効にした場合 — この節がデプロイしているのはこれ
# である — audience は
# /projects/PROJECT_NUMBER/locations/REGION/services/SERVICE_NAME である。
# (ロードバランサの背後では代わりに backend service になる。IAP の
# コンソールがそれをくれる: リソース横の ⋮ → "Get JWT audience code"。)
IAP_AUDIENCE="/projects/$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')/locations/$REGION/services/ochakai-webui"

gcloud run services update ochakai-webui --region=$REGION \
  --update-env-vars="OCHAKAI_IAP_AUDIENCE=$IAP_AUDIENCE"
```

起動時のログはどちらに転んだかを言う: 要求する audience と共に
`recording browser users by their IAP identity`、あるいは `no
OCHAKAI_IAP_AUDIENCE; writes are recorded as this service account` で
ある。

その後、書き込みは `human:tanaka@example.co.jp via
process:ochakai-webui@…` と読める — 片方だけになることは無く、常に
両方の identity である。serve-ui は IAP の署名、audience、有効期限、
issuer をすべてのリクエストで検証し、audience を設定した後は**IAP が
署名していないリクエストを(403 で)拒否する**: IAP が本当にサービスの
前段にいないなら、何か月分もの書き込みが間違った著者に帰属して
いたと気づくのではなく、すぐに気づくことになる。受け取った audience は
設定したものと並べてログに出るので、食い違いは一行で直せる。

これを設定していてもいなくても、両方の proxy はブラウザが送ってくる
どんな `Ochakai-On-Behalf-Of` も破棄する — ページは自分自身の著者を
名乗れない(設計ドキュメント 0065 §5)。

最初から最後まで試してみて分かったこと:

- **プログラムからのアクセス**(curl、スクリプト)は、`--iap` が使う
  Google 管理の OAuth クライアントによって制限される: 普通の ID
  トークンは拒否される。`roles/iap.httpsResourceAccessor` を付与された
  サービスアカウントは、`aud` がサービス URL に `/*` を足したものである
  自己署名 JWT で通れる(`gcloud iam service-accounts sign-jwt`); それ
  以上のことをしたいなら、カスタム OAuth クライアントで IAP を設定
  せよ。ここで想定しているクライアントはブラウザである — MCP と CLI は
  デプロイガイド §5 の経路を使う。

<a id="sandbox"></a>

## 使い捨てのサンドボックス

`OCHAKAI_MODE=sandbox`(設計ドキュメント
[0087](../design/0087-a-sandbox-says-it-is-one.md))は、**匿名で・書けて・
消える**デプロイである。公開デモ(下)は read-only なので、この製品の
中心であるループ — draft を書き、人が裁定し、結果が戻る — だけが試せない
という状態だった。サンドボックスはそこを埋める。

**復元はあなたの仕事で、ochakai は行わない。** 定期的に import する
コンテナは cron であって製品ではないからである。Cloud Scheduler が
Cloud Run job を叩き、job が既知のバンドルを書き戻す形にする:

```sh
# ジョブが実行するもの。ベースを空にしてから、種のバンドルを入れ直す。
ochakai export - > /tmp/before.tar.gz   # 任意: 直前の状態を残したいとき
ochakai list --json | jq -r '.hits[].id' | while read -r id; do
  ochakai delete "$id" && ochakai purge "$id"
done
ochakai import /seed
```

**サンドボックスは自分でそう言う。** `GET /api/v1/stats` が
`sandbox: true` を返し、同梱の Web UI は全ページにバナーを出す。
これは親切ではなく設計上の要請である — **言わないサンドボックスは、
そこに書いた人の作業を盗む**(0087 §3)。バナーを消したいと思ったときは、
消したいのはバナーではなく姿勢のほうである。

Terraform では `sandbox = true` が `allUsers` の付与まで面倒を見る。
`public_read_only` とは併用しない — サーバーが取るのは一語である。

<a id="public-demo"></a>

## 公開デモ

デプロイガイドのすべては、誰かが書き込めるあらゆるデプロイに対して
「`allUsers` は決してだめ」と言っており、それは今も正しい。デモは
例外であり、それは何を諦めるかがあるからこそ例外である。設定は二つ、
そして二つ目が一つ目を含意する:

- **`OCHAKAI_MODE=read-only`** — このデプロイはナレッジを何も変えない
  (設計ドキュメント 0066 §2)。書き込みは 403 になり、MCP は書き込み用の
  ツールを一切提供せず、web UI はどうせ失敗するだけのボタンを描くのを
  やめる。これ単体でも有用で、参照専用のコピーや、移行中にベースを
  凍結する用途に向く。
- **`OCHAKAI_MODE=public`** — このデプロイは identity を何も読まない
  (設計ドキュメント 0066 §3)。`Authorization` ヘッダーは無視される: 前段に
  Cloud Run IAM が無ければその署名は検証しようがなく、偽造された未署名
  トークンがそのまま信じられてしまうからである。`Ochakai-On-Behalf-Of`
  も無視される。すべての呼び出し元は `human:anonymous` になり、401 が
  返ることは無い。これは**read-only を含意し**、それと切り離すことは
  できない — 公開で読めて*かつ*書き込める ochakai は、このプログラムが
  受け付ける設定ではないので、「public」の危険な半分には設定そのものが
  存在しない。

それが取引のすべてである: **誰の名乗りも信じないサービスだからこそ、
公開しても安全である** — 誰も記録せず、何も書き込まないからである。

### 順序の問題

**read-only なデプロイはシードできない。** `ochakai import` は普通の
書き込みエンドポイント(デプロイガイド §5)に対するクライアント側の
ループである — その拒否を迂回する一括 import の経路は無い。だから順序
は常に: **書き込み可能かつ非公開**でデプロイし、import し、それから
切り替える。これを逆にすると、空のデモが残り、埋める方法が無くなる。

**Terraform** — [deploy/terraform](../../deploy/terraform) は両方の
変数を持つ:

```sh
cd deploy/terraform
# 1. 通常の非公開デプロイ: public_read_only は false のまま。
#    (invoker_members = ["user:you@your-org.example"] でデモには十分。)
terraform apply
export OCHAKAI_URL=$(terraform output -raw service_url)
# ... 一度だけのスキーマ bootstrap は、いつも通り(module の README)...

# 2. まだ非公開で書き込み可能なうちにシードする — 下を見よ。

# 3. 切り替える。これは同じ apply の中で OCHAKAI_MODE=public を設定し
#    allUsers を付与する; module には片方だけをやる方法が無い。
terraform apply -var public_read_only=true
terraform output -raw demo_url
```

**gcloud** — デプロイガイド §3 の通りにデプロイし(非公開、
`--no-allow-unauthenticated`)、シードしてから、この順序で切り替える:

```sh
# 1. まだ非公開のうちに、identity を信じるのと書き込みをやめる。
gcloud run services update ochakai --region=$REGION \
  --update-env-vars=OCHAKAI_MODE=public

# 2. それから初めて開く。
gcloud run services add-iam-policy-binding ochakai --region=$REGION \
  --member=allUsers --role=roles/run.invoker
```

順序こそが要点である: 二つのコマンドの間、サービスは無害であり、逆の
順序だと、公開された ochakai が見知らぬ人の送る `Authorization`
ヘッダーを何であれ信じてしまう窓が残る。デプロイガイド全体と同じく、
`--update-env-vars` を使うこと — `--set-env-vars` は
`OCHAKAI_DATABASE_URL` を消してしまう。

### シードする

[examples/demo](../../examples/demo) は読ませるために作られた 18 個の
concept を持つナレッジベースである: リンクされた concept、混在した
type、そして `sort=usage` が何かを意味するだけの利用実績。サービスが
まだ非公開のうちに import せよ。認証の方法はデプロイガイド §5 とまったく
同じである — CLI は自分の gcloud ログインから Google の ID トークンを
解決する。特別なことは何も無い:

```sh
git clone --depth 1 https://github.com/na0fu3y/ochakai && cd ochakai
go run ./cmd/ochakai import examples/demo    # $OCHAKAI_URL は上で
```

その 18 個の concept は `human:you@your-org.example` として記録される —
このベースが受け取る最後の provenance であり、切り替えの後に訪問者が
見る唯一の `created_by` である。(この手順はローカルサーバーに対して
リハーサル済みである: 書き込み可能な状態で import — 18 created、0
skipped — それから `OCHAKAI_MODE=public` で再起動すると、読み取りは
匿名で応答し、すべての書き込みが拒否される。Cloud Run が足すのは IAM の
binding だけである。)

### 切り替えが反映されたことを確認する

デモは(サンドボックスと並んで)素の `curl` が動くはずのデプロイなので、
proxy 無しで検証できる:

```sh
curl -s -o /dev/null -w '%{http_code}\n' "$OCHAKAI_URL/api/v1/search?q=revenue"  # 200、トークン無し
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE "$OCHAKAI_URL/api/v1/bundle/queries/monthly-revenue.md"  # 403
```

トークン無しでの 200 は、public の付与と `OCHAKAI_MODE=public` の両方が
反映されたことを証明する(切り替え前ならこれは Google の 401 である);
書き込みでの 403 は含意された read-only を証明する。でたらめな
`Authorization` ヘッダーを送っても、どちらの答えも変わらない — それは
副作用ではなく、その性質そのものである。

デモに**hybrid search**(デプロイガイド §4)を見せたいなら、import の
*前に* Vertex AI のロールを付与せよ: ベクトルは concept が書き込まれた
ときに書き込まれ、`ochakai reembed` それ自体が書き込みなので、
read-only になった後では拒否される。デモのバンドルにはファイルが無い
ので、デプロイガイド §4b のバケットはこれには要らない。

### 訪問者が何を打つか

Google アカウントを持たない人が、curl だけでなく CLI でも到達できる。
クライアントは https だからという理由で資格情報を要求したりはせず、
持っていなければ持たずに送り、**要ると言うかどうかはサーバーに任せる**
— public は 401 を返さないので、そのまま通る:

```sh
go install github.com/na0fu3y/ochakai/cmd/ochakai@latest
ochakai use https://demo.example      # デモの URL
ochakai search "なぜ売上が落ちているのか"
```

`ochakai whoami` はこのとき `human:anonymous (no Google credentials
found)` と表示する。これは推測ではなく、public が全員をそう解決する
ことそのものである(設計ドキュメント 0066 §3)。`ochakai ui` と
`ochakai mcp-stdio` も同じ資格情報無しの経路で動くので、web UI を
公開ホストしなくても、訪問者は自分の loopback で UI を開ける。
資格情報を要求するデプロイに同じことをすれば、サーバーが 401 で
そう言い、CLI はそこで初めて `gcloud auth login` を促す。

### コスト

デプロイガイド冒頭の表と同じ形である — デモは一つの Cloud Run サービス
と一つの `db-f1-micro` で、余分に払うものは無い。種類として違うのは
一つだけ: Cloud Run はリクエスト課金であり、リクエストはもはや自分の
組織からだけ来るのではない。それを抑えているのは `--max-instances=1`
(デプロイガイド §3)であり、そのままにしておくこと。

### 何を諦めるか、そしてなぜそれで構わないか

- **identity が一切無い。** 訪問者について知られ、記録されるものは
  何も無い。公開サービスを通して書き込まれた何かの `created_by` /
  `updated_by` は、サーバーがでっち上げた値になってしまう — だからこそ
  何も書き込めないようになっている。provenance は ochakai が売るものの
  大半であり、公開デモはそれを弱めるのではなく、それを装うことを拒否
  しているだけである。
- **ベースの中身はすべて公開される。** どこにも 401 は無いので、
  concept、そのリビジョン、そして著者のメールアドレスがオープンな
  インターネット上にある。デモ用のバンドルを入れよ。「read-only
  だから」という理由で、この姿勢を本物のナレッジベースに向けては
  決していけない。
- **委譲は無くなる**(デプロイガイド §5c) — 埋め込んだアプリケーション
  は自分の利用者の identity をここへ転送できない。何であれ信じられない
  からである。
- **Domain Restricted Sharing は `allUsers` の binding を拒否する** —
  上の[設定ミスに対するガードレール](#guardrails-against-misconfiguration)
  を見よ; デプロイガイドの §7 に正確なエラーメッセージがある。拒否
  されて正しい — 欲しいなら組織全体ではなく専用のデモプロジェクトの
  ためにそれを外せ。

**デモのデータベースはそれでも育つ。** usage テレメトリ — 検索のヒット
と fetch の回数(設計ドキュメント 0069 §3)— は、read-only の下でも意図的
に記録され続ける: それは呼び出し元の書き込みではなくサーバー自身の
観測であり、それを止めれば、デモがいちばん見せたい `sort=usage` の
フィードが凍りついてしまうからである(設計ドキュメント 0066 §2)。
帰結は二つ: auto-growth が off の 10 GB ディスクは硬い天井なので、
注目を集めるデモではそれを見張ること; そしてそれらの数は匿名の
見知らぬ人々によって動いているので、ナレッジについての証拠としてでは
なく、公開の人気シグナルとして扱うこと。

### 元に戻す

完全な取り壊しはデプロイガイドの §9 である。姿勢だけを取り下げてデプロイ
は残すには、設定したときと逆の順序を辿る — public の付与を先に外す
ので、read-only 以外の何かのままサービスが開いている瞬間は無い:

```sh
gcloud run services remove-iam-policy-binding ochakai --region=$REGION \
  --member=allUsers --role=roles/run.invoker
gcloud run services update ochakai --region=$REGION \
  --remove-env-vars=OCHAKAI_MODE
```

Terraform では、`terraform apply -var public_read_only=false` がその
順序で両方をやる。

<a id="upgrades"></a>

## アップグレード

マイグレーションは起動時に自動で走るので、アップグレードとは新しい
イメージタグのことである。最初に読むべき二つは、
[changelog](../../CHANGELOG.md) — 破壊的変更に印を付け、それぞれに
ついて運用担当者が何をすべきかを言う — と、下のバージョンごとの注記
である。

```sh
gcloud run services update ochakai --region=$REGION \
  --image=$REGION-docker.pkg.dev/$PROJECT_ID/ghcr/na0fu3y/ochakai:<new-tag>
```

デプロイガイド §1 でセットアップした Artifact Registry のリモート
リポジトリは、新しいタグを GHCR からオンデマンドで取得する; 何を得たか
は上の[サプライチェーン](#supply-chain)のコマンドで検証せよ。Cloud Run
のトラフィックを以前のリビジョンにロールバックしても、データベースの
マイグレーションはロールバック**されない**; マイグレーションは追加的な
ので、古いバイナリは新しいスキーマに対しても動き続ける。

`:latest` ではなくバージョンを固定せよ。再デプロイが決定であるように。

ここで知っておく価値のある、アップグレードに付随する罠が三つある:

- **web UI は API より前か、API と一緒にアップグレードせよ。決して
  後にしてはいけない。** 設計ドキュメント
  [0064](../design/0064-rest-stops-at-api-v1.md)は、dual-accept の
  窓を設けずに委譲用のヘッダーの名前を変えた。web UI の proxy は自分の
  ビルドが知っているスペルしか剥がさない。0064 をすでに取り込んだ API
  の前に、古い web UI がいると、今のスペルのヘッダーをそのまま素通り
  させてしまう — 400 ではなく、静かな書き込みの誤帰属になる(issue
  [#418](https://github.com/na0fu3y/ochakai/issues/418))。Terraform
  module は常に API を先に適用する; この一件のためだけに、
  `webui_image_tag` を `image_tag` より先に進めよ(issue
  [#426](https://github.com/na0fu3y/ochakai/issues/426))。
- **`OCHAKAI_VERTEX_MODEL` / `OCHAKAI_VERTEX_LOCATION` /
  `OCHAKAI_VERTEX_PROJECT` / `OCHAKAI_EMBEDDING_DIM` を設定している
  なら、アップグレードの*前*に `OCHAKAI_EMBEDDINGS` を書き換える。**
  四つは 0.18.0 で黙って無視されるようになった(設計ドキュメント
  [0080](../design/0080-search-and-how-a-deployment-embeds.md))。無視された
  デプロイは既定のモデル・リージョン・幅に落ちる。**そこで起きることは
  ログにしか出ない**: 幅が違えばベクトルテーブルは新しい幅で再構築されて
  空になり、幅が同じでモデルだけ違えば古い行は残るが新しいモデルの
  クエリからは見えない(検索はモデルで絞る)。どちらでも semantic search
  は黙って字句のみに落ち、**壊れたようには見えない** — 順位が悪くなる
  だけである。直すのは `ochakai reembed` で、それは Vertex AI の呼び出し
  をベースの規模ぶん支払う。手で `DROP TABLE` する必要は無い: ベクトルは
  それが記述する concept から導出されるものだからである(設計ドキュメント
  [0080](../design/0080-search-and-how-a-deployment-embeds.md) §3)。
  **この「黙って無視される」は終わった**: 四つを設定したまま上げると、
  そのリビジョンは名前を挙げて起動を拒む(設計ドキュメント
  [0112](../design/0112-a-start-refuses-a-variable-it-does-not-read.md))。
  起動しないリビジョンにトラフィックは移らないので、直前のリビジョンが
  答えたまま、消すべき名前がログに出る。
- **semantic search が到達可能になったり、モデルが変わったりしても、
  backfill はされない。** 既存の concept は `ochakai reembed` まで
  embedding が無いままである。これは Vertex AI のトークンをベースの
  規模に比例して消費する。

### バージョンごとの注記

- **0.9.0 より前から**: それより古いリリースはすべて
  [retract](https://go.dev/ref/mod#go-mod-file-retract) されており、
  バージョンごとの注記(0.9.0 自身の破壊的変更を含む)はここから
  取り除かれている — 必要なら git の履歴から回収すること(`git log --
  docs/guides/operating.md deploy/cloudrun/README.md`)。一つだけ残す:
  **廃止された MCP OAuth コネクタサービスのデプロイを、このイメージに
  向けては決していけない** — あれは公開で呼び出し可能であり、この
  イメージはそこに、ヘッダーを信じる非公開向けの surface を提供して
  しまう。アップグレードするのではなく削除せよ(`gcloud run services
  delete ochakai-connector --region=$REGION`)。
