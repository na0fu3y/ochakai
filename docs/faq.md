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

六つの隣人すべてに一度に答え、ochakai が劣る場面を言い、他を選ぶべき
なのは誰かを言うページが一枚ある:
[ポジショニング](positioning.md) — semantic layer、カタログ、メモリ層、
RAG、AI アナリスト製品に内蔵された検証済みクエリストア、そして MCP
サーバー付きの markdown ノートの vault。

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
verified・rejected・deprecated — を拒み、拒否は代わりに何をすべきかを
言う:

> cannot replace metrics/revenue from this surface: it is verified, and
> this surface has no If-Match precondition to replace curated knowledge
> safely. If it is wrong, say so with report_outcome failed — that puts it
> in the re-verification feed. If you have something better,
> put_concept a new draft. A human changes curated concepts from the
> web UI or CLI.

これは認可ではない — 同じデプロイに届く人間は、REST からも CLI からも
Web UI からも何でも編集できる。これは面の規則である: MCP には、安全な
置き換えを表現できる `If-Match` の precondition を運ぶ道が無い(設計
ドキュメント 0067 §6、0030)。裁定済み concept の tombstone を
`put_concept` で復活させることが MCP で拒まれるのも同じ理由で、却下の
理由が記録されていた場所に新しい draft を置くことになるからである。
REST と CLI ではその復活が許される — 人がキュレーションする面だから
である。

### 二人が同じ concept を編集したらどうなるか

後勝ちである。クライアントがそれ以上を求めない限りは。すべての `GET` と
`PUT` は `ETag` — concept の正準 OKF 文書のハッシュを引用符で括ったもの
で、本文にも `summary.content_hash` として載る — を返し、古い値の
`If-Match` を付けた `PUT` は `412` になり何も書かない(設計ドキュメント
0030、0075 §3.2)。内容だけのハッシュなので、concept を verify したり
reject したり、ファイルを添えたりしても precondition は有効なままで
ある — 無効にするのは編集だけである。MCP は version フィールドを外に
出さないが、裁定済み concept を守るために内部で同じ仕組みを使う。

### 誰が読み書きできるか

デプロイに到達できる者すべてである — 到達可能性がアクセスモデルの全部で
あり、その上に認可は無い。ochakai が何を identity として記録するか、
到達できた呼び出し元にできることを狭める姿勢(`read-only`・`public`)は
[動作要件と設定](configuration.md#authentication-has-no-configuration)に
ある。

**境界が要るなら、インスタンスを分ける。** 部門ごとに読める範囲を変え
たいときの答えは concept ごとの権限ではなく、デプロイをもう一つ立てる
ことである。増えるのは Cloud Run のサービスと、**同じ Cloud SQL イン
スタンスの中の別データベース**だけで — `OCHAKAI_DATABASE_URL` の dbname
を変え、[deploy ガイド](../deploy/cloudrun/README.md)の bootstrap SQL を
その新しいデータベースにもう一度流す — インスタンスは増えない。Cloud
Run はリクエスト課金なので、境界一つの限界費用はほぼゼロである。

**しかも二重に効く。** 境界ごとに専用のサービスアカウントを使えば、誰が
到達できるかを Cloud Run IAM が決め、そのサービスがどのデータベースを
開けるかを Cloud SQL IAM の GRANT が決める。どちらの層にも置く secret
は無い。

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
持って出るかで、どちらも今日できる。

### ホスティング版はあるか

無い。ochakai はテナントごとのセルフホストであり、今日サインアップ
できるサービスは存在しない。

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
は無い(設計ドキュメント 0074 §2) — そして `title` は任意である。
id の最後のセグメントが既に concept を名指しているからである(設計
ドキュメント 0074 §1)。
