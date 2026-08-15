# ドキュメント

まずは [README](../README.md) を読む — ochakai とは何か、クイックスタート、
そして何をしないか。README はわざと短くしてあり、詳しい説明はここにある。

**English speakers: [docs/en.md](en.md)** — one page that says what each
Japanese page holds, in what order to read them, and the names you would
otherwise have to hunt for. Not a translation; a way in.

**このマニュアルは日本語である**(C8、[surface.md](surface.md))。英語で
残るのは玄関と契約だけで、それは決定である — [README](../README.md)
(使うかどうかを決める場所であり、その読者は国際的である)、
[api/openapi.yaml](../api/openapi.yaml)、[cli.md](cli.md)(バイナリの
`-h` から生成されるので、ページを訳すことはバイナリを訳すことになる)、
[互換性](compatibility.md)、[ROADMAP](../ROADMAP.md)、
[SECURITY](../SECURITY.md)、[SUPPORT](../SUPPORT.md)。**対訳は置かない**
— 同じことを言う二本のテキストは、どちらが古いか誰にも分からなくなる。

## 導入を検討している人へ

- [FAQ](faq.md) — 検証済みナレッジに対してエージェントが何をしてよいか、
  二人が同じ concept を編集したらどうなるか、誰が読み書きできるか、そして
  辞めるときは何が起こるか。他のページが持っている問いには短い答えと
  そこへのリンクだけを置き、二重に書かない。
- [ポジショニング](positioning.md) — 「X を使えばいいのでは」への答えを
  一箇所に集めたページ: semantic layer、カタログ、メモリ層、RAG、
  AI アナリスト製品に内蔵された検証済みクエリストア、MCP サーバー付きの
  markdown vault。ochakai が劣る場面と、そのとき何を選ぶべきかも含む。
- [互換性とサポート](compatibility.md) — REST は `/api/v1` で凍結済み。
  MCP・CLI・保存形式はまだ 0.x のままで不安定、名前の猶予期間は一リリース
  だけ、サポートされるのは最新リリースのみ。ochakai の上に何かを組む前に
  読む。
- [ROADMAP](../ROADMAP.md) — いま取り組んでいること、そして意図して
  断っていること。
- [CHANGELOG](../CHANGELOG.md) — リリース間で何が変わったか。

## 使う人へ

- [改善ループ](loop.md) — 思い出す・書き戻す・裁定する・結果報告の四つ:
  それを端から端まで歩く四つのプロンプト、Web UI の三つのフィード、
  そして何が測られているか。
- [最初のひと月](guides/onboarding.md) — 空のベースから始める人の一本道:
  範囲の決め方と先に書き出す 20 の問い、型ごとの取り込み順序と分量の目安、
  **書いたものをエージェントが検索で見つけられるかの確かめ方**、そして
  二周目から週次で何を見るか。
- [アーキテクチャ](architecture.md) — マニュアルの参照側。concept とは
  何か、type、住所としての id、本文から導かれるリンク、trust を運ぶ OKF
  の frontmatter、検索が何をするか、部品がどう組み合わさるか。**何であり
  なぜ選ぶか**は README にあり、このページは読み終えて作業を始めた人を
  前提にする。
- [CLI リファレンス](cli.md) — 全コマンドの synopsis、フラグ、実例。
  `ochakai <command> -h` をそのまま出力したもので、両者が食い違えば
  テストが落ちる。バイナリを持つ前から読め、持った後も古びない。
- [MCP クライアントを繋ぐ](guides/mcp-clients.md) — エージェントに見える
  七つのツール、そしてクライアントごとの URL か `mcp-stdio` ブリッジ、
  各クライアントが読む設定ファイル。
- [最初のカタログを作る](../examples/bigquery-catalog) — BigQuery の
  テーブルを concept として取り込む day-one の手順、それを走らせる job、
  そして走りが正しかったかを判定する attester。コネクタではなく、自分の
  サービスアカウントで動く普通のクライアントである。

## 運用する人へ

- [動作要件と設定](configuration.md) — ochakai が起動前に必要とするもの、
  そして読み込むすべての環境変数。
- [Terraform でデプロイ](../deploy/terraform/README.md) — 推奨される
  経路で、月 $10 程度: `terraform apply` 一回と、手作業ひとつ(スキーマの
  bootstrap)で済む。gcloud を直接叩くと、同じものを一手順ずつ組み立てる
  ことになる。
- [Cloud Run にデプロイ](../deploy/cloudrun/README.md) — module の内側で
  実際に走っている gcloud の手順そのもの。コマンドを自分の手で打ちたい
  ときは module の代わりにここを読む。各リソースが何で、なぜ必要かの
  リファレンスでもある: private IP、IAP 越しの web UI、security
  hardening のチェックリスト、アップグレードの経路。
- [deploy/compose.yaml](../deploy/compose.yaml) — ローカルのスタック。
  Google アカウントは要らない。手元から CLI で触るならバイナリも要る
  ([動作要件](configuration.md#requirements))。
- [デプロイの運用](guides/operating.md) — 二種類のバックアップとそれぞれ
  が何を復旧するか、OKF バンドルからの復元が持ち帰ら*ない*もの、デプロイ
  が劣化しつつも動いているときログが何と言うか、スケールについて分かって
  いること — そして分かっていないこと。
- [golden query canary](guides/golden-query-canary.md) — 検証済みクエリ
  を CI から実行し、静かに壊れたナレッジベースがそうと分かるようにする。
- [Git をレビュー経路にする](guides/git-review.md) — export した木を Git
  に置き、ナレッジの変更を PR で通す運用。往復で何が動かないか、なぜ
  merge が verify ではないか、そして CI から取り込むと誰が記録されるか。
- [トラブルシューティング](guides/troubleshooting.md) — ローカルとクライ
  アント側の半分: 空の検索結果、スキップされた import、404 や 412、空の
  web UI、起動を拒むサーバー。Google Cloud 側の症状はデプロイガイドの
  §7 にある。

## 組み込んで使う人へ

- [api/openapi.yaml](../api/openapi.yaml) — REST の契約。説明ではなく
  検査される対象で、統合テストがすべてのリクエストとレスポンスをこれと
  突き合わせるので、乖離したエンドポイントは CI で落ちる。
- [examples/demo](../examples/demo) — import して触れる 11 concept の
  ナレッジベース(`ochakai import examples/demo`)。リンクした concept、
  混在する status、宣言された期限を過ぎた concept があり、フィードと
  `get_context` に何か表示するものがある。
- [examples/claude-code](../examples/claude-code) — そのまま使える
  エージェント向けの指示と、思い出す/書き戻すのフック。
- [REST API を組み込む](guides/rest-integration.md) — 認証の方法、
  利用者本人の identity をサービスアカウントに潰さず転送する方法、他の
  書き込みと競合しない書き方。

## 変更する人へ

- [CONTRIBUTING.md](../CONTRIBUTING.md) — ローカルのセットアップ、CI が
  走らせる正確なチェック、design doc の仕組み。
- [表面](surface.md) — ochakai が存在する理由となる八つの条件、そして
  REST operation・クエリパラメータ・ヘッダ・MCP ツール・CLI コマンド・
  CLI フラグ・環境変数・語彙・このマニュアルのページを一箇所で数えた
  もの。表面を一つ足す変更が答えるべき三つの問いもここにある。既定の
  答えは no である。九つの一覧すべてをビルドから読み戻すテストがある
  ので数はずれず、機能は diff に出ないままでは現れない — それを説明
  する散文も同じで、これが九つ目の一覧であり、最も速く増えている。
- [docs/design](design) — 番号付きの、書き換えない決定記録、そしてどれ
  が今の状態を説明しているかを言う [index](design/README.md)。ほとんど
  は日本語で、英語の記述と食い違えば記録本体が正しい。
  [README.en.md](design/README.en.md) はその全部を英語で要約する — 何が
  決まったか、それが ochakai を使う人にとって何を意味するか。
- [SECURITY.md](../SECURITY.md) — 脆弱性の報告方法、そして何が脆弱性に
  数えられるか。
