# FAQ

README が前提にしているのに、一箇所では書いていないことへの答え。

ここにある問いのいくつかは、実際には他のページが持っている領域の話で
ある。そういうものには短い答えとリンクだけを置き、二つ目の写しは作ら
ない — **「X を使えばいいのでは」は[ポジショニング](positioning.md)、
症状はすべて[トラブルシューティング](guides/troubleshooting.md)、設定は
すべて[動作要件と設定](configuration.md)である。** 二つのテキストが同じ
ことを言えば、どちらかが古いのに誰にも分からなくなる。

### Google Cloud 無しで動かせるか

本番では動かせない。Cloud Run の IAM チェックがアクセスモデルそのもので
あり、Cloud SQL IAM がデータベースの資格情報である — この二つが揃って
いることが、ochakai が自分の secret を一つも持たない理由である。
`deploy/compose.yaml` は同じバイナリを、認証を切った素の PostgreSQL に
対してローカルで動かす — 開発用のハーネスであって、本番の小さい端では
ない。可搬なのはナレッジであってランタイムではない。詳しくは
[動作要件](configuration.md#requirements)。

### 自分のデータはプロジェクトの外に出るか

出ない。ochakai はどこへも何も報告しない — テレメトリが無いので、
オプトアウトするものも無い。接続する先はあなた自身のプロジェクトの
Google Cloud API だけである: 常に Cloud SQL、`OCHAKAI_GCS_BUCKET` を
設定していれば GCS、そして意味的検索が有効なら Vertex AI — Google Cloud
の上で動く以上、これが既定である(設計ドキュメント
[0080](design/0080-search-and-how-a-deployment-embeds.md))。
`OCHAKAI_EMBEDDINGS=off` — このデプロイがどう埋め込むかを言う唯一の変数
(設計ドキュメント
[0080](design/0080-search-and-how-a-deployment-embeds.md))— がそれを断り、
`roles/aiplatform.user` を与えないことでも同じことになる。どちらでも、
インスタンスは自分のデータベースとしか話さなくなる。

利用回数はあなた自身のデータベースの行である。どのナレッジが自分の
場所に見合っているかをレビュアーが見られるように存在するもので、
そこから出ることはない。

### RAG やメモリ層、markdown vault と何が違うか

比較対象を一つずつ取り上げ、ochakai が劣る場面を書き、他を選ぶべきなのは
誰かを書くページが一枚ある: [ポジショニング](positioning.md)。Google Cloud
の Knowledge Catalog(旧 Dataplex)、ウェアハウス native の semantic
layer、カタログ、メモリ層、RAG、AI アナリスト製品に内蔵された検証済み
クエリストア、オントロジー基盤、そして MCP サーバー付きの markdown
vault が並ぶ。

一行にすると: *メモリ層は起きたことを覚え、ochakai は正しいことを
キュレーションする*。そして vault に対しては、ファイル形式**以外の
すべて**が違う — `ochakai export` のバンドルは
**vault として開ける**からである
([ポジショニング](positioning.md#a-markdown-vault-with-an-mcp-server))。

### 埋め込みは必要か

たいていは必要で、だからこそ頼まなくても付いてくる: Google Cloud の上で
動く ochakai は自分のプロジェクトを見つけ、サービス identity が Vertex
AI を呼べるなら hybrid search を有効にする。埋め込みが効くのは、
ナレッジベースが日本語のとき、問いがキーワードではなく文で届くとき、
そして中身で検索したい画像や PDF を添えるときである。無くても検索は
動く — 字句のみで、外部 API を呼ばない。それが何を犠牲にするか、なぜ
日本語が埋め込みを要する場合なのかは
[アーキテクチャの検索の節](architecture.md#search)にある。断る三つの
方法は[設定](configuration.md#environment-variables)にある。

<a id="can-an-agent-overwrite-or-delete-knowledge-a-human-verified"></a>

### エージェントは人が検証したナレッジを上書き・削除できるか

MCP からはできない。削除はそもそもツールですらない — 設計ドキュメント
[0076](design/0076-two-tools-leave-mcp.md) が `delete_concept` をこの面
から降ろした。ナレッジを消すことは裁定であり、MCP は取り消せる裁定さえ
運んでいないからである。`put_concept` は人が裁定した concept —
verified・deprecated — を拒み、拒否は代わりに何をすべきかを
言う:

> cannot replace metrics/revenue from this surface: a human ruled on it
> (verified), and this surface has no If-Match precondition to replace
> curated knowledge safely. If it is wrong, say so with report_outcome
> failed — that puts it in the re-verification feed. If you have
> something better, put_concept a new draft at a different id, linking
> this one from its body so the reviewer sees both. A human changes
> curated concepts from the web UI or CLI.

「別の id に、リンクして」が答えの本体である。検証済みのテーブル concept
に業務説明を後から足したいなら、追記は別の concept として書き、本文から
そのテーブルにリンクする — テーブルを取ったエージェントは `linked_from`
でその追記に出会う(設計ドキュメント
[0106](design/0106-a-read-carries-what-points-at-it.md))。検証は信頼の
印であると同時に、この面に対する書き込みの門でもある。

これは認可ではない — 同じデプロイに届く人間は、REST からも CLI からも
Web UI からも何でも編集できる。これは面の規則である: MCP には、安全な
置き換えを表現できる `If-Match` の precondition を運ぶ道が無い(設計
ドキュメント 0067 §6、0030)。検証済み・deprecated だった concept の
tombstone を `put_concept` で復活させることが MCP で拒まれるのも同じ
理由で、人が保証した内容を、判断されたことの無い draft で置き換える
ことになるからである。REST と CLI ではその復活が許される — 人が
キュレーションする面だからである。**却下された id はこれに当たらない**:
却下は削除であり、その墓標は普通の墓標なので、MCP からも書き直せる
(設計ドキュメント 0135 §3)。

### 二人が同じ concept を編集したらどうなるか

後勝ちである。クライアントがそれ以上を求めない限りは。すべての `GET` と
`PUT` は `ETag` — concept の正準 OKF 文書のハッシュを引用符で括ったもの
で、本文にも `summary.content_hash` として載る — を返し、古い値の
`If-Match` を付けた `PUT` は `412` になり何も書かない(設計ドキュメント
0030、0075 §3.2)。内容だけのハッシュなので、concept を verify したり
利用を報告したり、ファイルを添えたりしても precondition は有効なままで
ある — 無効にするのは編集だけである。MCP は version フィールドを外に
出さないが、裁定済み concept を守るために内部で同じ仕組みを使う。

**Web UI のエディタはそれ以上を求める。** 画面を開いた版に対して保存する
ので、開いている間に誰かが保存していれば**上書きせずに止まり**、書いた
ものは欄に残る。人が散文を書く面で後勝ちにしていたのはこちらの欠陥で、
API は最初からこの precondition を持っていた。

<a id="who-can-read-and-write"></a>

### 誰が読み書きできるか

既定では、デプロイに到達できる者すべてである — 到達可能性がアクセス
モデルの全部であり、その上に認可は無い。ochakai が何を identity として記録するか、
到達できた呼び出し元にできることを狭める姿勢(`read-only`・`public`)は
[動作要件と設定](configuration.md#authentication-has-no-configuration)に
ある。

**境界が要るなら、まずインスタンスを分ける。** 部門ごとに読める範囲を
変えたいときの最初の答えは、デプロイをもう一つ立てることである。増えるのは Cloud Run のサービスと、**同じ Cloud SQL イン
スタンスの中の別データベース**だけで — `OCHAKAI_DATABASE_URL` の dbname
を変え、[deploy ガイド](../deploy/cloudrun/README.md)の bootstrap SQL を
その新しいデータベースにもう一度流す — インスタンスは増えない。Cloud
Run はリクエスト課金なので、境界一つの限界費用はほぼゼロである。

**しかも二重に効く。** 境界ごとに専用のサービスアカウントを使えば、誰が
到達できるかを Cloud Run IAM が決め、そのサービスがどのデータベースを
開けるかを Cloud SQL IAM の GRANT が決める。どちらの層にも置く secret
は無い。

### 分けられないときは、ディレクトリごとに閲覧者と編集者を置ける

分割が効かない形が一つある — **チームどうしが語彙を共有しているとき**で
ある。全社の用語集を各バンドルにコピーすると、取り込みは provenance を
読み戻さないので(設計ドキュメント
[0075](design/0075-the-bundle-is-the-address-space.md) §3.1)、コピーは
`unverified` で着地する。**分割の代金が「検証済みという事実」になる。**

そのための任意の機構が
[0109](design/0109-a-directory-has-readers-and-writers.md) である:
`ochakai access` がポリシーを表示し、`-f` で置き換える。一行は
「この principal はこのディレクトリ以下を読める(`may_write` なら書ける)」
で、拒否規則は無く、照合はセグメント境界(`sales` は `sales/orders` に
当たり `sales-legacy/orders` には当たらない)。管理者は
`OCHAKAI_ADMINS` が名指す。

**行が一つも無ければ、この節の上半分がそのまま現行である。** 最初の
一行が全員に対して同時に境界を入れるので、書いたら読み返すこと。

**そして塞げないものが一つある**: 読める concept の本文が、隠した
concept の id を名指していれば、その id は本文の中に現れる。サーバーは
受け取った文書を書き換えないと決めており(0075 §3)、そこにバンドルの
往復が乗っているからである。約束しているのは**一覧・検索・取得・
エクスポート・index から出てこない**ことであって、id が本文に一度も
現れないことではない。

### ochakai を使うのをやめたら、ナレッジはどうなるか

`ochakai export ./knowledge` がベース全体を、git に置ける OKF v0.2 の
バンドルとして書き出す — ochakai の存在を知らない他の OKF consumer が
読める形である。`ochakai import` はその逆で、どの producer のバンドル
でも受け取る。往復で何が残り何が残らないか — provenance は残らない —
は[デプロイの運用](guides/operating.md#backup-and-restore)にある。
Git をレビュー経路にする運用そのものは
[Git をレビュー経路にする](guides/git-review.md)にある。

### 維持者が止まったら、どうなるか

維持者は一人で([SUPPORT.md](../SUPPORT.md))、後任も、それを約束する
組織も無い。答えはサポート体制ではなく、**人質を取っていない**ことで
ある。

動いているデプロイは止まらない。ochakai が話す相手はあなた自身の
プロジェクトの Google Cloud API だけで、ライセンスサーバーにも SaaS
にもテレメトリにも問い合わせない — 上流が消えた日に期限切れになる
ものが一つも無い。ナレッジは一つ上の問いのとおり丸ごと出て、MIT なので
フォークできる。

止まるのは**次のリリース**である。最新リリースだけがサポートされ
バックポートは無いので([互換性とサポート](compatibility.md#support))、
その時点で脆弱性修正も止まる。選択はフォークして直すか、バンドルを
持って出るかで、どちらも今日できる。フォークが自分の名前で動くために
変えるのは二箇所 — `go.mod` の module 行と、`release.yaml` が push する
イメージのパス — で、CI の検査はそのまま付いてくる。

### ホスティング版はあるか

無い。ochakai はテナントごとのセルフホストであり、今日サインアップ
できるサービスは存在しない。誰かが複数の組織の運用を引き受ける日が
来ても、形は変わらない — 組織ごとにデプロイか、上の問いのディレクトリ
で、運用者は IAM の principal であって鍵の持ち主ではない(設計ドキュメント
[0119](design/0119-an-operated-fleet-is-deployments-or-directories.md))。

### 自分のウェアハウスに接続したり、SQL を実行したりするか

どちらもしない。ochakai はウェアハウスの資格情報を持たず、何も実行
しない。持つのは sanctioned な計算 — `# Computation` フェンスの中の
SQL と、`runtime` / `parameters` / `executor` / `attester` が書く契約 —
であり、それをそのまま返す。走らせるのはあなたのエージェントである。
実行の正しさを確かめる方法を書く Attested Computation でさえ、ochakai
が記録するのは契約であって、その一部を取りに行くことも実行することも
ない。

### どれくらいの大きさのナレッジベースを扱えるか

天井を測った人はおらず、正直な答えは「ochakai は**キュレーションされた**
ベースが届く規模のために作られている」である — 数百万ではなく数千の
concept であり、一つ残らず人を通っているからである。何が測られていて
何が測られていないかは
[デプロイの運用](guides/operating.md#capacity)にある。それより明らかに
大きなデプロイを計画しているなら
[Discussions](https://github.com/na0fu3y/ochakai/discussions) で言って
ほしい — 約束する前に測るべき類のことである。

### Go のツールチェーンを入れずに試せるか

試せる。二通りある。
[リリースアーカイブ](https://github.com/na0fu3y/ochakai/releases)を
取るか — linux・macOS・Windows の amd64 と arm64、チェックサムと
build provenance 付き — クライアントを丸ごと飛ばして REST API を直接
叩くかである。後者は CLI がやっていることそのものである。
[データモデル](architecture.md#the-data-model)は concept を作る `curl`
から始まる。

### 「concept」とは正確には何か

YAML frontmatter を持つ一つの markdown 文書で、パスのような id
(`queries/sales/monthly-revenue`)で住所指定される。id が住所であり、
型はディレクトリではなく属性である(設計ドキュメント 0075 §2)。関係は
本文の普通の markdown リンクから導かれる — 埋めるべき links フィールド
は無い(設計ドキュメント 0136 §2) — そして `title` は任意である。
id の最後のセグメントが既に concept を名指しているからである(設計
ドキュメント 0136 §1)。
